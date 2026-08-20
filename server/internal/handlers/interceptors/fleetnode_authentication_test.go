package interceptors

import (
	"context"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/fleetnode/auth"
)

type fleetNodeSessionStore struct {
	resolved *auth.ResolvedFleetNode
}

func (s fleetNodeSessionStore) UpsertChallenge(context.Context, []byte, int64, time.Time) error {
	panic("not used")
}

func (s fleetNodeSessionStore) ConsumeChallenge(context.Context, []byte, time.Time) (int64, error) {
	panic("not used")
}

func (s fleetNodeSessionStore) SweepExpiredChallenges(context.Context, time.Time) (int64, error) {
	panic("not used")
}

func (s fleetNodeSessionStore) UpsertSession(context.Context, string, int64, time.Time) error {
	panic("not used")
}

func (s fleetNodeSessionStore) GetSessionFleetNode(context.Context, string, time.Time) (*auth.ResolvedFleetNode, error) {
	return s.resolved, nil
}

func (s fleetNodeSessionStore) SweepExpiredSessions(context.Context, time.Time) (int64, error) {
	panic("not used")
}

type fleetNodeStreamingConn struct {
	procedure string
	header    http.Header
}

func (c fleetNodeStreamingConn) Spec() connect.Spec         { return connect.Spec{Procedure: c.procedure} }
func (fleetNodeStreamingConn) Peer() connect.Peer           { return connect.Peer{} }
func (fleetNodeStreamingConn) Receive(any) error            { panic("not used") }
func (c fleetNodeStreamingConn) RequestHeader() http.Header { return c.header }
func (fleetNodeStreamingConn) Send(any) error               { panic("not used") }
func (fleetNodeStreamingConn) ResponseHeader() http.Header  { return http.Header{} }
func (fleetNodeStreamingConn) ResponseTrailer() http.Header { return http.Header{} }

func TestFleetNodeAuthInterceptorExpiresAuthenticatedStream(t *testing.T) {
	const procedure = "/test.Service/ControlStream"
	expiresAt := time.Now().Add(50 * time.Millisecond)
	authService := auth.NewService(fleetNodeSessionStore{resolved: &auth.ResolvedFleetNode{
		FleetNodeID:      42,
		OrgID:            7,
		Name:             "test-node",
		SessionExpiresAt: expiresAt,
	}}, nil, nil)
	interceptor := NewFleetNodeAuthInterceptor(authService, []string{procedure})
	header := http.Header{}
	header.Set("Authorization", "Bearer session-token")
	conn := fleetNodeStreamingConn{procedure: procedure, header: header}

	wrapper := interceptor.WrapStreamingHandler(func(ctx context.Context, _ connect.StreamingHandlerConn) error {
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.WithinDuration(t, expiresAt, deadline, time.Millisecond)
		<-ctx.Done()
		return ctx.Err()
	})

	err := wrapper(t.Context(), conn)

	require.ErrorIs(t, err, context.DeadlineExceeded)
}
