package ha

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	ErrDCSClusterIdentityMismatch = errors.New("DCS cluster identity mismatch")
	ErrLeaderLeaseExpired         = errors.New("DCS leader lease is not live")
	ErrTimelineMismatch           = errors.New("Patroni and PostgreSQL timelines do not match")
	ErrWritableServerMismatch     = errors.New("DCS leader does not match writable PostgreSQL server")
	ErrWriterChanged              = errors.New("DCS writer changed during validation")
)

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

// PostgresIdentity identifies the server selected by Fleet's writable DSN.
type PostgresIdentity struct {
	ServerAddress string
	ServerPort    int32
	InRecovery    bool
	Timeline      int64
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

type writerIdentityReader interface {
	WritableIdentity(ctx context.Context) (PostgresIdentity, error)
}

type patroniIdentityReader interface {
	PrimaryIdentity(ctx context.Context, memberAPIURL string) (PatroniIdentity, error)
}

type hostResolver func(context.Context, string) ([]string, error)

// Observer validates every authority needed to use a DCS leader revision as a
// writer generation. It deliberately owns no retry or caching policy.
type Observer struct {
	clusterPath string
	dcs         dcsReader
	writer      writerIdentityReader
	patroni     patroniIdentityReader
	resolve     hostResolver
}

// NewObserver builds a production observer from narrow DCS, SQL, and Patroni
// readers. The caller supplies clients configured for the deployment's trust
// boundary.
func NewObserver(
	clusterPath string,
	dcs dcsReader,
	writer writerIdentityReader,
	patroni patroniIdentityReader,
) (*Observer, error) {
	clusterPath = strings.TrimRight(clusterPath, "/")
	if clusterPath == "" || dcs == nil || writer == nil || patroni == nil {
		return nil, errors.New("HA writer observer requires cluster path and readers")
	}
	return &Observer{
		clusterPath: clusterPath,
		dcs:         dcs,
		writer:      writer,
		patroni:     patroni,
		resolve:     resolveHost,
	}, nil
}

// Observe brackets writer validation with linearizable DCS reads and fails
// closed unless the DCS member, writable SQL server, Patroni role, timeline,
// live leader lease, and cluster identity agree.
func (o *Observer) Observe(ctx context.Context) (WriterObservation, error) {
	first, err := o.dcs.Snapshot(ctx, o.clusterPath)
	if err != nil {
		return WriterObservation{}, fmt.Errorf("read initial DCS snapshot: %w", err)
	}
	if err := validateDCSSnapshot(first); err != nil {
		return WriterObservation{}, err
	}

	writer, err := o.writer.WritableIdentity(ctx)
	if err != nil {
		return WriterObservation{}, fmt.Errorf("read writable PostgreSQL identity: %w", err)
	}
	if writer.InRecovery || writer.ServerAddress == "" || writer.ServerPort <= 0 || writer.Timeline <= 0 {
		return WriterObservation{}, fmt.Errorf("%w: invalid writable PostgreSQL identity", ErrWritableServerMismatch)
	}
	if err := o.validateWriterAddress(ctx, first.Member.ConnURL, writer); err != nil {
		return WriterObservation{}, err
	}

	patroni, err := o.patroni.PrimaryIdentity(ctx, first.Member.APIURL)
	if err != nil {
		return WriterObservation{}, fmt.Errorf("validate Patroni primary: %w", err)
	}
	if patroni.Role != "primary" && patroni.Role != "master" {
		return WriterObservation{}, fmt.Errorf("%w: Patroni role is %q", ErrWritableServerMismatch, patroni.Role)
	}
	if patroni.Timeline != writer.Timeline {
		return WriterObservation{}, fmt.Errorf(
			"%w: PostgreSQL=%d Patroni=%d",
			ErrTimelineMismatch,
			writer.Timeline,
			patroni.Timeline,
		)
	}

	second, err := o.dcs.Snapshot(ctx, o.clusterPath)
	if err != nil {
		return WriterObservation{}, fmt.Errorf("read final DCS snapshot: %w", err)
	}
	if err := validateDCSSnapshot(second); err != nil {
		return WriterObservation{}, err
	}
	if second.ClusterID != first.ClusterID {
		return WriterObservation{}, fmt.Errorf(
			"%w: started with %s, finished with %s",
			ErrDCSClusterIdentityMismatch,
			first.ClusterID,
			second.ClusterID,
		)
	}
	if second.LeaderName != first.LeaderName ||
		second.WriterGeneration != first.WriterGeneration ||
		second.Member.APIURL != first.Member.APIURL ||
		second.Member.ConnURL != first.Member.ConnURL {
		return WriterObservation{}, fmt.Errorf(
			"%w: started with %s@%d, finished with %s@%d",
			ErrWriterChanged,
			first.LeaderName,
			first.WriterGeneration,
			second.LeaderName,
			second.WriterGeneration,
		)
	}

	ttl, err := o.dcs.LeaseTTL(ctx, second.LeaderLeaseID)
	if err != nil {
		return WriterObservation{}, fmt.Errorf("validate DCS leader lease: %w", err)
	}
	if ttl <= 0 {
		return WriterObservation{}, ErrLeaderLeaseExpired
	}

	return WriterObservation{
		DCSClusterID:     second.ClusterID,
		DCSRevision:      second.Revision,
		WriterGeneration: second.WriterGeneration,
		LeaderName:       second.LeaderName,
		LeaderLeaseID:    second.LeaderLeaseID,
		ServerAddress:    writer.ServerAddress,
		ServerPort:       writer.ServerPort,
		Timeline:         writer.Timeline,
	}, nil
}

func (o *Observer) validateWriterAddress(
	ctx context.Context,
	connURL string,
	writer PostgresIdentity,
) error {
	parsed, err := url.Parse(connURL)
	if err != nil || parsed.Hostname() == "" {
		return fmt.Errorf("%w: invalid Patroni conn_url", ErrWritableServerMismatch)
	}
	port := int32(5432)
	if parsed.Port() != "" {
		parsedPort, err := strconv.ParseInt(parsed.Port(), 10, 32)
		if err != nil {
			return fmt.Errorf("%w: invalid Patroni conn_url port", ErrWritableServerMismatch)
		}
		port = int32(parsedPort)
	}
	if port != writer.ServerPort {
		return fmt.Errorf(
			"%w: SQL selected port %d, DCS advertised %d",
			ErrWritableServerMismatch,
			writer.ServerPort,
			port,
		)
	}
	addresses, err := o.resolve(ctx, parsed.Hostname())
	if err != nil {
		return fmt.Errorf("%w: resolve DCS leader: %v", ErrWritableServerMismatch, err)
	}
	for _, address := range addresses {
		if normalizedIP(address) == normalizedIP(writer.ServerAddress) {
			return nil
		}
	}
	return fmt.Errorf(
		"%w: SQL selected %s:%d, DCS advertised %s",
		ErrWritableServerMismatch,
		writer.ServerAddress,
		writer.ServerPort,
		connURL,
	)
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

// EtcdHTTPClient implements the minimal etcd v3 JSON-gateway surface required
// for linearizable Patroni leader observations.
type EtcdHTTPClient struct {
	endpoint string
	client   *http.Client
}

func NewEtcdHTTPClient(endpoint string, client *http.Client) *EtcdHTTPClient {
	if client == nil {
		client = http.DefaultClient
	}
	return &EtcdHTTPClient{endpoint: strings.TrimRight(endpoint, "/"), client: client}
}

func (c *EtcdHTTPClient) Snapshot(ctx context.Context, clusterPath string) (DCSSnapshot, error) {
	prefix := []byte(strings.TrimRight(clusterPath, "/") + "/")
	payload := map[string]any{
		"key":          base64.StdEncoding.EncodeToString(prefix),
		"range_end":    base64.StdEncoding.EncodeToString(prefixRangeEnd(prefix)),
		"serializable": false,
	}
	var response etcdRangeResponse
	if err := c.post(ctx, "/v3/kv/range", payload, &response); err != nil {
		return DCSSnapshot{}, err
	}
	return extractDCSSnapshot(response, clusterPath)
}

func (c *EtcdHTTPClient) LeaseTTL(ctx context.Context, leaseID int64) (time.Duration, error) {
	var response struct {
		ID  string `json:"ID"`
		TTL string `json:"TTL"`
	}
	if err := c.post(
		ctx,
		"/v3/lease/timetolive",
		map[string]any{"ID": strconv.FormatInt(leaseID, 10), "keys": false},
		&response,
	); err != nil {
		return 0, err
	}
	returnedID, err := parsePositiveInt(response.ID, "leader lease ID")
	if err != nil || returnedID != leaseID {
		return 0, errors.New("etcd returned a different leader lease")
	}
	ttl, err := strconv.ParseInt(response.TTL, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse leader lease TTL: %w", err)
	}
	return time.Duration(ttl) * time.Second, nil
}

func (c *EtcdHTTPClient) post(
	ctx context.Context,
	path string,
	payload any,
	result any,
) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode etcd %s request: %w", path, err)
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.endpoint+path,
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("create etcd %s request: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("call etcd %s: %w", path, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		limited, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return fmt.Errorf("etcd %s returned %s: %s", path, response.Status, strings.TrimSpace(string(limited)))
	}
	if err := json.NewDecoder(response.Body).Decode(result); err != nil {
		return fmt.Errorf("decode etcd %s response: %w", path, err)
	}
	return nil
}

type etcdRangeResponse struct {
	Header struct {
		ClusterID string `json:"cluster_id"`
		Revision  string `json:"revision"`
	} `json:"header"`
	KVs []struct {
		Key            string `json:"key"`
		Value          string `json:"value"`
		CreateRevision string `json:"create_revision"`
		Lease          string `json:"lease"`
	} `json:"kvs"`
}

func extractDCSSnapshot(response etcdRangeResponse, clusterPath string) (DCSSnapshot, error) {
	clusterPath = strings.TrimRight(clusterPath, "/")
	leaderPath := clusterPath + "/leader"
	var leaderName string
	var generation, leaseID int64
	members := make(map[string]DCSMember)

	for _, kv := range response.KVs {
		keyBytes, err := base64.StdEncoding.DecodeString(kv.Key)
		if err != nil {
			return DCSSnapshot{}, fmt.Errorf("decode etcd key: %w", err)
		}
		valueBytes, err := base64.StdEncoding.DecodeString(kv.Value)
		if err != nil {
			return DCSSnapshot{}, fmt.Errorf("decode etcd value for %s: %w", keyBytes, err)
		}
		key := string(keyBytes)
		switch {
		case key == leaderPath:
			if leaderName != "" {
				return DCSSnapshot{}, errors.New("DCS snapshot has duplicate leader key")
			}
			leaderName = string(valueBytes)
			generation, err = parsePositiveInt(kv.CreateRevision, "leader create_revision")
			if err != nil {
				return DCSSnapshot{}, err
			}
			leaseID, err = parsePositiveInt(kv.Lease, "leader lease")
			if err != nil {
				return DCSSnapshot{}, err
			}
		case strings.HasPrefix(key, clusterPath+"/members/"):
			name := strings.TrimPrefix(key, clusterPath+"/members/")
			var value struct {
				APIURL  string `json:"api_url"`
				ConnURL string `json:"conn_url"`
			}
			if err := json.Unmarshal(valueBytes, &value); err != nil {
				return DCSSnapshot{}, fmt.Errorf("decode DCS member %s: %w", name, err)
			}
			members[name] = DCSMember{Name: name, APIURL: value.APIURL, ConnURL: value.ConnURL}
		}
	}

	revision, err := parsePositiveInt(response.Header.Revision, "DCS revision")
	if err != nil {
		return DCSSnapshot{}, err
	}
	snapshot := DCSSnapshot{
		ClusterID:        response.Header.ClusterID,
		Revision:         revision,
		LeaderName:       leaderName,
		WriterGeneration: generation,
		LeaderLeaseID:    leaseID,
		Member:           members[leaderName],
	}
	return snapshot, validateDCSSnapshot(snapshot)
}

func parsePositiveInt(value string, field string) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s is missing or invalid", field)
	}
	return parsed, nil
}

func prefixRangeEnd(prefix []byte) []byte {
	result := append([]byte(nil), prefix...)
	for i := len(result) - 1; i >= 0; i-- {
		if result[i] < 0xff {
			result[i]++
			return result[:i+1]
		}
	}
	return []byte{0}
}

// PatroniHTTPClient validates the member's /primary response.
type PatroniHTTPClient struct {
	client *http.Client
}

func NewPatroniHTTPClient(client *http.Client) *PatroniHTTPClient {
	if client == nil {
		client = http.DefaultClient
	}
	return &PatroniHTTPClient{client: client}
}

func (c *PatroniHTTPClient) PrimaryIdentity(
	ctx context.Context,
	memberAPIURL string,
) (PatroniIdentity, error) {
	parsed, err := url.Parse(memberAPIURL)
	if err != nil {
		return PatroniIdentity{}, fmt.Errorf("parse Patroni member API URL: %w", err)
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/patroni") + "/primary"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return PatroniIdentity{}, fmt.Errorf("create Patroni primary request: %w", err)
	}
	response, err := c.client.Do(req)
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
	if state.Role != "primary" && state.Role != "master" {
		return PatroniIdentity{}, fmt.Errorf("Patroni reports non-primary role %q", state.Role)
	}
	if state.Timeline <= 0 {
		return PatroniIdentity{}, errors.New("Patroni reports invalid timeline")
	}
	return PatroniIdentity{Role: state.Role, Timeline: state.Timeline}, nil
}
