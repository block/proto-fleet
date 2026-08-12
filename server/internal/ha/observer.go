package ha

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/transportguard"
)

var (
	ErrDCSClusterIdentityMismatch = errors.New("DCS cluster identity mismatch")
	ErrLeaderLeaseExpired         = errors.New("DCS leader lease is not live")
	ErrTimelineMismatch           = errors.New("Patroni and PostgreSQL timelines do not match")
	ErrWritableServerMismatch     = errors.New("DCS leader does not match writable PostgreSQL server")
	ErrWriterChanged              = errors.New("DCS writer changed during validation")
)

const defaultHAHTTPTimeout = 5 * time.Second

// DCSMember is the connection identity Patroni publishes for one member.
type DCSMember struct {
	Name    string
	APIURL  string
	ConnURL string
}

// DCSSnapshot is one linearizable view of the Patroni leader and matching member.
type DCSSnapshot struct {
	ClusterID        string
	Revision         int64
	LeaderName       string
	WriterGeneration int64
	LeaderLeaseID    int64
	Member           DCSMember
}

// PatroniIdentity is returned only when Patroni's /primary check succeeds.
type PatroniIdentity struct {
	Role     string
	Timeline int64
}

type dcsReader interface {
	Snapshot(ctx context.Context, clusterPath string) (DCSSnapshot, error)
	LeaseTTL(ctx context.Context, leaseID int64) (time.Duration, error)
}

type postgresIdentityReader interface {
	GetConnectedPostgresIdentity(
		ctx context.Context,
	) (sqlc.ConnectedPostgresIdentity, error)
}

type patroniIdentityReader interface {
	PrimaryIdentity(
		ctx context.Context,
		memberAPIURL string,
		expectedServerAddress string,
	) (PatroniIdentity, error)
}

type hostResolver func(context.Context, string) ([]string, error)

// Observer validates every authority needed to use a DCS leader revision as a
// writer generation. It deliberately owns no retry or caching policy.
type Observer struct {
	clusterPath string
	dcs         dcsReader
	postgres    postgresIdentityReader
	patroni     patroniIdentityReader
	resolve     hostResolver
}

// NewObserver builds a production observer from narrow DCS, SQL, and Patroni
// readers. The caller supplies clients configured for the deployment's trust
// boundary.
func NewObserver(
	clusterPath string,
	dcs dcsReader,
	postgres postgresIdentityReader,
	patroni patroniIdentityReader,
) (*Observer, error) {
	clusterPath = strings.TrimRight(clusterPath, "/")
	if clusterPath == "" || dcs == nil || postgres == nil || patroni == nil {
		return nil, errors.New("HA writer observer requires cluster path and readers")
	}
	return &Observer{
		clusterPath: clusterPath,
		dcs:         dcs,
		postgres:    postgres,
		patroni:     patroni,
		resolve:     resolveHost,
	}, nil
}

// Observe brackets writer validation with linearizable DCS reads and fails
// closed unless the DCS member, writable SQL server, Patroni role, timeline,
// live leader lease, and cluster identity agree.
func (o *Observer) Observe(ctx context.Context) (WriterObservation, error) {
	return o.observeAndRun(ctx, nil)
}

// ObserveAndRun executes action after validating the candidate writer and
// before the closing DCS read. The action must independently bind its database
// write to the server identity in the observation.
func (o *Observer) ObserveAndRun(
	ctx context.Context,
	action func(context.Context, WriterObservation) error,
) (WriterObservation, error) {
	if action == nil {
		return WriterObservation{}, errors.New("HA writer observation action is required")
	}
	return o.observeAndRun(ctx, action)
}

func (o *Observer) observeAndRun(
	ctx context.Context,
	action func(context.Context, WriterObservation) error,
) (WriterObservation, error) {
	first, err := o.dcs.Snapshot(ctx, o.clusterPath)
	if err != nil {
		return WriterObservation{}, fmt.Errorf("read initial DCS snapshot: %w", err)
	}
	if err := validateDCSSnapshot(first); err != nil {
		return WriterObservation{}, err
	}

	connected, err := o.postgres.GetConnectedPostgresIdentity(ctx)
	if err != nil {
		return WriterObservation{}, fmt.Errorf("read connected PostgreSQL identity: %w", err)
	}
	if connected.InRecovery ||
		connected.ServerAddress == "" ||
		connected.ServerPort <= 0 ||
		connected.Timeline <= 0 {
		return WriterObservation{}, fmt.Errorf(
			"%w: invalid connected PostgreSQL identity",
			ErrWritableServerMismatch,
		)
	}
	if err := o.validateConnectedPostgresEndpoints(ctx, first.Member, connected); err != nil {
		return WriterObservation{}, err
	}

	patroni, err := o.patroni.PrimaryIdentity(
		ctx,
		first.Member.APIURL,
		connected.ServerAddress,
	)
	if err != nil {
		return WriterObservation{}, fmt.Errorf("validate Patroni primary: %w", err)
	}
	if !isPrimaryRole(patroni.Role) {
		return WriterObservation{}, fmt.Errorf("%w: Patroni role is %q", ErrWritableServerMismatch, patroni.Role)
	}
	observed := writerObservation(first, connected)
	if patroni.Timeline != connected.Timeline {
		second, closingErr := o.confirmDCSUnchanged(ctx, first)
		if closingErr != nil {
			return WriterObservation{}, closingErr
		}
		return writerObservation(second, connected), fmt.Errorf(
			"%w: PostgreSQL=%d Patroni=%d",
			ErrTimelineMismatch,
			connected.Timeline,
			patroni.Timeline,
		)
	}
	if action != nil {
		if err := action(ctx, observed); err != nil {
			return WriterObservation{}, err
		}
	}

	second, err := o.confirmDCSUnchanged(ctx, first)
	if err != nil {
		return WriterObservation{}, err
	}

	ttlRequestStarted := time.Now()
	ttl, err := o.dcs.LeaseTTL(ctx, second.LeaderLeaseID)
	if err != nil {
		return WriterObservation{}, fmt.Errorf("validate DCS leader lease: %w", err)
	}
	if ttl <= 0 {
		return WriterObservation{}, ErrLeaderLeaseExpired
	}
	proofDeadline := ttlRequestStarted.Add(ttl)
	if !proofDeadline.After(time.Now()) {
		return WriterObservation{}, ErrLeaderLeaseExpired
	}

	observed = writerObservation(second, connected)
	observed.DCSProofDeadline = proofDeadline
	return observed, nil
}

func (o *Observer) confirmDCSUnchanged(
	ctx context.Context,
	first DCSSnapshot,
) (DCSSnapshot, error) {
	second, err := o.dcs.Snapshot(ctx, o.clusterPath)
	if err != nil {
		return DCSSnapshot{}, fmt.Errorf("read final DCS snapshot: %w", err)
	}
	if err := validateDCSSnapshot(second); err != nil {
		return DCSSnapshot{}, err
	}
	if second.ClusterID != first.ClusterID {
		return DCSSnapshot{}, fmt.Errorf(
			"%w: started with %s, finished with %s",
			ErrDCSClusterIdentityMismatch,
			first.ClusterID,
			second.ClusterID,
		)
	}
	if second.LeaderName != first.LeaderName ||
		second.WriterGeneration != first.WriterGeneration ||
		second.LeaderLeaseID != first.LeaderLeaseID ||
		second.Member.APIURL != first.Member.APIURL ||
		second.Member.ConnURL != first.Member.ConnURL {
		return DCSSnapshot{}, fmt.Errorf(
			"%w: started with %s@%d, finished with %s@%d",
			ErrWriterChanged,
			first.LeaderName,
			first.WriterGeneration,
			second.LeaderName,
			second.WriterGeneration,
		)
	}
	return second, nil
}

func writerObservation(
	snapshot DCSSnapshot,
	connected sqlc.ConnectedPostgresIdentity,
) WriterObservation {
	return WriterObservation{
		DCSClusterID:     snapshot.ClusterID,
		WriterGeneration: snapshot.WriterGeneration,
		LeaderName:       snapshot.LeaderName,
		ServerAddress:    connected.ServerAddress,
		ServerPort:       connected.ServerPort,
		Timeline:         connected.Timeline,
	}
}

func (o *Observer) validateConnectedPostgresEndpoints(
	ctx context.Context,
	member DCSMember,
	connected sqlc.ConnectedPostgresIdentity,
) error {
	conn, err := url.Parse(member.ConnURL)
	if err != nil || conn.Hostname() == "" {
		return fmt.Errorf("%w: invalid Patroni conn_url", ErrWritableServerMismatch)
	}
	port := int32(5432)
	if conn.Port() != "" {
		parsedPort, err := strconv.ParseInt(conn.Port(), 10, 32)
		if err != nil {
			return fmt.Errorf("%w: invalid Patroni conn_url port", ErrWritableServerMismatch)
		}
		port = int32(parsedPort)
	}
	if port != connected.ServerPort {
		return fmt.Errorf(
			"%w: SQL selected port %d, DCS advertised %d",
			ErrWritableServerMismatch,
			connected.ServerPort,
			port,
		)
	}
	api, err := url.Parse(member.APIURL)
	if err != nil || api.Hostname() == "" {
		return fmt.Errorf("%w: invalid Patroni member URL", ErrWritableServerMismatch)
	}

	connectedAddress := normalizedIP(connected.ServerAddress)
	resolvedHosts := make(map[string][]string, 2)
	for _, endpoint := range []struct {
		host    string
		display string
	}{
		{host: conn.Hostname(), display: endpointAuthority(conn)},
		{host: api.Hostname(), display: endpointAuthority(api)},
	} {
		addresses, ok := resolvedHosts[endpoint.host]
		if !ok {
			addresses, err = o.resolve(ctx, endpoint.host)
			if err != nil {
				return fmt.Errorf("%w: resolve DCS leader: %v", ErrWritableServerMismatch, err)
			}
			resolvedHosts[endpoint.host] = addresses
		}
		matched := slices.ContainsFunc(addresses, func(address string) bool {
			return normalizedIP(address) == connectedAddress
		})
		if !matched {
			return fmt.Errorf(
				"%w: SQL selected %s:%d, DCS advertised %s",
				ErrWritableServerMismatch,
				connected.ServerAddress,
				connected.ServerPort,
				endpoint.display,
			)
		}
	}
	return nil
}

func endpointAuthority(endpoint *url.URL) string {
	return (&url.URL{Scheme: endpoint.Scheme, Host: endpoint.Host}).String()
}

func validateDCSSnapshot(snapshot DCSSnapshot) error {
	switch {
	case snapshot.ClusterID == "" || snapshot.ClusterID == "0":
		return errors.New("DCS snapshot has no cluster identity")
	case snapshot.Revision <= 0:
		return errors.New("DCS snapshot has no revision")
	case snapshot.LeaderName == "":
		return errors.New("DCS snapshot has no leader")
	case snapshot.WriterGeneration <= 0:
		return errors.New("DCS leader create_revision is invalid")
	case snapshot.LeaderLeaseID <= 0:
		return errors.New("DCS leader lease is invalid")
	case snapshot.Member.Name != snapshot.LeaderName ||
		snapshot.Member.APIURL == "" ||
		snapshot.Member.ConnURL == "":
		return errors.New("DCS snapshot has no matching leader member")
	default:
		return nil
	}
}

func resolveHost(ctx context.Context, host string) ([]string, error) {
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve host %s: %w", host, err)
	}
	result := make([]string, 0, len(addresses))
	for _, address := range addresses {
		result = append(result, address.IP.String())
	}
	return result, nil
}

func normalizedIP(address string) string {
	if parsed := net.ParseIP(address); parsed != nil {
		return parsed.String()
	}
	return address
}

func isPrimaryRole(role string) bool {
	return role == "primary" || role == "master"
}

type etcdAPI interface {
	Close() error
	Get(
		ctx context.Context,
		key string,
		opts ...clientv3.OpOption,
	) (*clientv3.GetResponse, error)
	TimeToLive(
		ctx context.Context,
		id clientv3.LeaseID,
		opts ...clientv3.LeaseOption,
	) (*clientv3.LeaseTimeToLiveResponse, error)
}

// EtcdClient reads Patroni authority through etcd's authenticated native API.
// The official client owns TLS, authentication tokens, endpoint failover, and
// token refresh; this wrapper only translates the two reads the observer needs.
type EtcdClient struct {
	client         etcdAPI
	requestTimeout time.Duration
}

func NewEtcdClient(config clientv3.Config) (*EtcdClient, error) {
	if config.DialTimeout <= 0 {
		config.DialTimeout = defaultHAHTTPTimeout
	}
	client, err := clientv3.New(config)
	if err != nil {
		return nil, fmt.Errorf("create etcd client: %w", err)
	}
	return &EtcdClient{
		client:         client,
		requestTimeout: defaultHAHTTPTimeout,
	}, nil
}

func (c *EtcdClient) Close() error {
	return c.client.Close()
}

func (c *EtcdClient) Snapshot(ctx context.Context, clusterPath string) (DCSSnapshot, error) {
	prefix := strings.TrimRight(clusterPath, "/") + "/"
	requestCtx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()
	response, err := c.client.Get(requestCtx, prefix, clientv3.WithPrefix())
	if err != nil {
		return DCSSnapshot{}, fmt.Errorf("read Patroni DCS prefix: %w", err)
	}
	return extractDCSSnapshot(response, clusterPath)
}

func (c *EtcdClient) LeaseTTL(ctx context.Context, leaseID int64) (time.Duration, error) {
	requestCtx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()
	response, err := c.client.TimeToLive(requestCtx, clientv3.LeaseID(leaseID))
	if err != nil {
		return 0, fmt.Errorf("read Patroni leader lease TTL: %w", err)
	}
	if response == nil || int64(response.ID) != leaseID {
		return 0, errors.New("etcd returned a different leader lease")
	}
	if response.TTL <= 0 {
		return 0, ErrLeaderLeaseExpired
	}
	return time.Duration(response.TTL) * time.Second, nil
}

func extractDCSSnapshot(response *clientv3.GetResponse, clusterPath string) (DCSSnapshot, error) {
	if response == nil || response.Header == nil {
		return DCSSnapshot{}, errors.New("DCS snapshot has no response header")
	}
	clusterPath = strings.TrimRight(clusterPath, "/")
	leaderPath := clusterPath + "/leader"
	var leaderName string
	var generation, leaseID int64
	members := make(map[string]DCSMember)

	for _, kv := range response.Kvs {
		key := string(kv.Key)
		switch {
		case key == leaderPath:
			if leaderName != "" {
				return DCSSnapshot{}, errors.New("DCS snapshot has duplicate leader key")
			}
			leaderName = string(kv.Value)
			generation = kv.CreateRevision
			leaseID = kv.Lease
		case strings.HasPrefix(key, clusterPath+"/members/"):
			name := strings.TrimPrefix(key, clusterPath+"/members/")
			var value struct {
				APIURL  string `json:"api_url"`
				ConnURL string `json:"conn_url"`
			}
			if err := json.Unmarshal(kv.Value, &value); err != nil {
				return DCSSnapshot{}, fmt.Errorf("decode DCS member %s: %w", name, err)
			}
			members[name] = DCSMember{Name: name, APIURL: value.APIURL, ConnURL: value.ConnURL}
		}
	}

	snapshot := DCSSnapshot{
		ClusterID:        strconv.FormatUint(response.Header.ClusterId, 10),
		Revision:         response.Header.Revision,
		LeaderName:       leaderName,
		WriterGeneration: generation,
		LeaderLeaseID:    leaseID,
		Member:           members[leaderName],
	}
	return snapshot, validateDCSSnapshot(snapshot)
}

// PatroniHTTPClient validates the member's /primary response.
type PatroniHTTPClient struct {
	client *http.Client
}

func NewPatroniHTTPClient(client *http.Client) *PatroniHTTPClient {
	if client == nil {
		client = &http.Client{Timeout: defaultHAHTTPTimeout}
	}
	return &PatroniHTTPClient{client: client}
}

func (c *PatroniHTTPClient) PrimaryIdentity(
	ctx context.Context,
	memberAPIURL string,
	expectedServerAddress string,
) (PatroniIdentity, error) {
	parsed, err := url.Parse(memberAPIURL)
	if err != nil {
		return PatroniIdentity{}, fmt.Errorf("parse Patroni member API URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return PatroniIdentity{}, fmt.Errorf(
			"Patroni member API URL scheme must be http or https; got %q",
			parsed.Scheme,
		)
	}
	if parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return PatroniIdentity{}, errors.New("Patroni member API URL is malformed")
	}
	pinnedIP := net.ParseIP(expectedServerAddress)
	if pinnedIP == nil {
		return PatroniIdentity{}, errors.New("validated PostgreSQL server address is not an IP")
	}

	baseTransport := c.client.Transport
	if baseTransport == nil {
		baseTransport = http.DefaultTransport
	}
	transport, ok := baseTransport.(*http.Transport)
	if !ok {
		return PatroniIdentity{}, errors.New("Patroni HTTP client transport cannot be safely cloned")
	}
	transport = transport.Clone()
	if transport.DialTLSContext != nil || transport.DialTLS != nil {
		return PatroniIdentity{}, errors.New("Patroni HTTP client transport has a custom TLS dialer")
	}
	transport.Proxy = nil
	if parsed.Scheme == "https" && transport.TLSClientConfig != nil {
		transport.TLSClientConfig.ServerName = parsed.Hostname()
	}
	// Keep the URL hostname for Host and TLS SNI, but dial only the IP that
	// PostgreSQL and both DCS endpoints were already validated against.
	var dialer net.Dialer
	transport.Dial = nil
	transport.DialContext = func(
		ctx context.Context,
		network string,
		address string,
	) (net.Conn, error) {
		_, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("parse Patroni dial address: %w", err)
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(pinnedIP.String(), port))
	}
	client := *c.client
	client.Transport = transport
	client.CheckRedirect = transportguard.RejectRedirect
	defer transport.CloseIdleConnections()

	parsed.Path = strings.TrimSuffix(parsed.Path, "/patroni") + "/primary"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return PatroniIdentity{}, fmt.Errorf("create Patroni primary request: %w", err)
	}
	response, err := client.Do(req)
	if err != nil {
		return PatroniIdentity{}, fmt.Errorf("call Patroni primary endpoint: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return PatroniIdentity{}, fmt.Errorf("Patroni /primary returned %s", response.Status)
	}
	var state struct {
		Role     string `json:"role"`
		Timeline int64  `json:"timeline"`
	}
	if err := json.NewDecoder(response.Body).Decode(&state); err != nil {
		return PatroniIdentity{}, fmt.Errorf("decode Patroni primary response: %w", err)
	}
	if !isPrimaryRole(state.Role) {
		return PatroniIdentity{}, fmt.Errorf("Patroni reports non-primary role %q", state.Role)
	}
	if state.Timeline <= 0 {
		return PatroniIdentity{}, errors.New("Patroni reports invalid timeline")
	}
	return PatroniIdentity{
		Role:     state.Role,
		Timeline: state.Timeline,
	}, nil
}
