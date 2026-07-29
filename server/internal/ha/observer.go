package ha

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/transportguard"
)

var (
	ErrTimelineMismatch       = errors.New("Patroni and PostgreSQL timelines do not match")
	ErrWritableServerMismatch = errors.New("Patroni does not confirm the writable PostgreSQL server")
	ErrWriterChanged          = errors.New("PostgreSQL writer changed during validation")
)

const (
	defaultHAHTTPTimeout = 5 * time.Second
	defaultPatroniPort   = 8008
)

// PatroniIdentity is returned only when Patroni's /primary check succeeds.
type PatroniIdentity struct {
	Role     string
	Timeline int64
}

type postgresIdentityReader interface {
	GetConnectedPostgresIdentity(
		ctx context.Context,
	) (sqlc.ConnectedPostgresIdentity, error)
}

type patroniIdentityReader interface {
	PrimaryIdentity(
		ctx context.Context,
		serverAddress string,
	) (PatroniIdentity, error)
}

// Observer binds Fleet ownership to the writable PostgreSQL server selected by
// the multi-host DSN and to Patroni's primary lock for that same server.
type Observer struct {
	postgres postgresIdentityReader
	patroni  patroniIdentityReader
}

func NewObserver(
	postgres postgresIdentityReader,
	patroni patroniIdentityReader,
) (*Observer, error) {
	if postgres == nil || patroni == nil {
		return nil, errors.New("HA writer observer requires PostgreSQL and Patroni readers")
	}
	return &Observer{postgres: postgres, patroni: patroni}, nil
}

func (o *Observer) Observe(ctx context.Context) (WriterObservation, error) {
	return o.observeWriter(ctx)
}

// ObserveAndRun validates the writer before and after action. The action must
// also bind its database write to the supplied server identity.
func (o *Observer) ObserveAndRun(
	ctx context.Context,
	action func(context.Context, WriterObservation) error,
) (WriterObservation, error) {
	if action == nil {
		return WriterObservation{}, errors.New("HA writer observation action is required")
	}
	observed, err := o.observeWriter(ctx)
	if err != nil {
		return WriterObservation{}, err
	}
	if err := action(ctx, observed); err != nil {
		return WriterObservation{}, err
	}
	verified, err := o.observeWriter(ctx)
	if err != nil {
		return WriterObservation{}, err
	}
	if verified != observed {
		return WriterObservation{}, fmt.Errorf(
			"%w: started with %s@%d on %s:%d, finished with %s@%d on %s:%d",
			ErrWriterChanged,
			observed.PostgresSystemIdentifier,
			observed.WriterGeneration,
			observed.ServerAddress,
			observed.ServerPort,
			verified.PostgresSystemIdentifier,
			verified.WriterGeneration,
			verified.ServerAddress,
			verified.ServerPort,
		)
	}
	return verified, nil
}

func (o *Observer) observeWriter(ctx context.Context) (WriterObservation, error) {
	connected, err := o.postgres.GetConnectedPostgresIdentity(ctx)
	if err != nil {
		return WriterObservation{}, fmt.Errorf("read connected PostgreSQL identity: %w", err)
	}
	if connected.SystemIdentifier == "" ||
		connected.InRecovery ||
		net.ParseIP(connected.ServerAddress) == nil ||
		connected.ServerPort <= 0 ||
		connected.Timeline <= 0 {
		return WriterObservation{}, fmt.Errorf(
			"%w: invalid connected PostgreSQL identity",
			ErrWritableServerMismatch,
		)
	}

	patroni, err := o.patroni.PrimaryIdentity(ctx, connected.ServerAddress)
	if err != nil {
		return WriterObservation{}, fmt.Errorf("validate Patroni primary: %w", err)
	}
	if !isPrimaryRole(patroni.Role) {
		return WriterObservation{}, fmt.Errorf(
			"%w: Patroni role is %q",
			ErrWritableServerMismatch,
			patroni.Role,
		)
	}
	if patroni.Timeline != connected.Timeline {
		return WriterObservation{}, fmt.Errorf(
			"%w: PostgreSQL=%d Patroni=%d",
			ErrTimelineMismatch,
			connected.Timeline,
			patroni.Timeline,
		)
	}

	return WriterObservation{
		PostgresSystemIdentifier: connected.SystemIdentifier,
		WriterGeneration:         connected.Timeline,
		ServerAddress:            connected.ServerAddress,
		ServerPort:               connected.ServerPort,
	}, nil
}

func isPrimaryRole(role string) bool {
	return role == "primary" || role == "master"
}

// PatroniHTTPClient confirms that the selected PostgreSQL server owns
// Patroni's primary lock.
type PatroniHTTPClient struct {
	client *http.Client
	port   int
}

func NewPatroniHTTPClient(client *http.Client) (*PatroniHTTPClient, error) {
	if client == nil {
		client = &http.Client{Timeout: defaultHAHTTPTimeout}
	}
	baseTransport := client.Transport
	if baseTransport == nil {
		baseTransport = http.DefaultTransport
	}
	transport, ok := baseTransport.(*http.Transport)
	if !ok {
		return nil, errors.New("Patroni HTTP client transport cannot be safely cloned")
	}
	transport = transport.Clone()
	transport.Proxy = nil
	configuredClient := *client
	configuredClient.Transport = transport
	configuredClient.CheckRedirect = transportguard.RejectRedirect
	return &PatroniHTTPClient{
		client: &configuredClient,
		port:   defaultPatroniPort,
	}, nil
}

func (c *PatroniHTTPClient) PrimaryIdentity(
	ctx context.Context,
	serverAddress string,
) (PatroniIdentity, error) {
	ip := net.ParseIP(serverAddress)
	if ip == nil {
		return PatroniIdentity{}, errors.New("validated PostgreSQL server address is not an IP")
	}
	if c.port <= 0 || c.port > 65535 {
		return PatroniIdentity{}, errors.New("Patroni API port is invalid")
	}

	endpoint := url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(ip.String(), strconv.Itoa(c.port)),
		Path:   "/primary",
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
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

	var state PatroniIdentity
	if err := json.NewDecoder(response.Body).Decode(&state); err != nil {
		return PatroniIdentity{}, fmt.Errorf("decode Patroni primary response: %w", err)
	}
	if !isPrimaryRole(state.Role) {
		return PatroniIdentity{}, fmt.Errorf("Patroni reports non-primary role %q", state.Role)
	}
	if state.Timeline <= 0 {
		return PatroniIdentity{}, errors.New("Patroni reports invalid timeline")
	}
	return state, nil
}
