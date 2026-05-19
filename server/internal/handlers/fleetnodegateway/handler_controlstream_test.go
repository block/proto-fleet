package fleetnodegateway_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
	"github.com/block/proto-fleet/server/internal/domain/apikey"
	"github.com/block/proto-fleet/server/internal/domain/fleetnodeauth"
	"github.com/block/proto-fleet/server/internal/domain/fleetnodecontrol"
	"github.com/block/proto-fleet/server/internal/domain/fleetnodeenrollment"
	"github.com/block/proto-fleet/server/internal/domain/fleetnodepairing"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/handlers/fleetnodegateway"
	"github.com/block/proto-fleet/server/internal/handlers/interceptors"
	"github.com/block/proto-fleet/server/internal/testutil"
)

type controlHarness struct {
	handler     *fleetnodegateway.Handler
	registry    *fleetnodecontrol.Registry
	fleetNodeID int64
}

func newControlHarness(t *testing.T) *controlHarness {
	t.Helper()
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	db := testutil.GetTestDB(t)
	_, err := db.Exec(`INSERT INTO organization (id, org_id, name, miner_auth_private_key) VALUES (1, 'test-org', 'Test Org', 'dummy-key') ON CONFLICT DO NOTHING`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO "user" (id, user_id, username, password_hash) VALUES (1, 'test-user', 'op', 'dummy') ON CONFLICT DO NOTHING`)
	require.NoError(t, err)

	apiKeyStore := sqlstores.NewSQLApiKeyStore(db)
	apiKeySvc := apikey.NewService(apiKeyStore, nil)
	transactor := sqlstores.NewSQLTransactor(db)
	enrollmentStore := sqlstores.NewSQLFleetNodeEnrollmentStore(db)
	enrollmentSvc := fleetnodeenrollment.NewService(enrollmentStore, apiKeySvc, transactor, nil)
	authStore := sqlstores.NewSQLFleetNodeAuthStore(db)
	authSvc := fleetnodeauth.NewService(authStore, enrollmentStore, apiKeySvc)
	pairingStore := sqlstores.NewSQLFleetNodePairingStore(db)
	pairingSvc := fleetnodepairing.NewService(pairingStore, enrollmentStore, transactor)
	registry := fleetnodecontrol.NewRegistry()

	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	signing, _, _ := ed25519.GenerateKey(rand.Reader)
	code, _, err := enrollmentSvc.CreateCode(t.Context(), 1, 1, time.Hour)
	require.NoError(t, err)
	agent, _, err := enrollmentSvc.RegisterFleetNode(t.Context(), code, "agent-control", pubKey, signing)
	require.NoError(t, err)

	return &controlHarness{
		handler:     fleetnodegateway.NewHandler(enrollmentSvc, authSvc, pairingSvc, registry),
		registry:    registry,
		fleetNodeID: agent.ID,
	}
}

func TestControlStream_DispatchesCommandAndRoutesAck(t *testing.T) {
	// Arrange
	h := newControlHarness(t)
	client := startControlServer(t, h)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream := client.ControlStream(ctx)
	t.Cleanup(func() { _ = stream.CloseRequest(); _ = stream.CloseResponse() })

	require.NoError(t, stream.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_Hello{Hello: &pb.ControlHello{}}}))
	first, err := stream.Receive()
	require.NoError(t, err)
	require.NotNil(t, first.GetAccepted(), "expected Accepted")

	// Server has now registered; dispatch a command and assert it lands on the wire.
	events, cleanup, err := waitForSend(t, h.registry, h.fleetNodeID, "cmd-1", []byte("payload"))
	require.NoError(t, err)
	defer cleanup()

	got, err := stream.Receive()
	require.NoError(t, err)
	cmd := got.GetCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "cmd-1", cmd.GetCommandId())

	// Act: agent acks
	require.NoError(t, stream.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_Ack{Ack: &pb.ControlAck{CommandId: "cmd-1", Succeeded: true}}}))

	// Assert
	select {
	case ev, ok := <-events:
		require.True(t, ok)
		require.NotNil(t, ev.Ack)
		assert.True(t, ev.Ack.GetSucceeded())
	case <-time.After(time.Second):
		t.Fatal("expected ack on events channel")
	}
}

func TestControlStream_RejectsSecondStreamForSameNode(t *testing.T) {
	// Arrange
	h := newControlHarness(t)
	client := startControlServer(t, h)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s1 := client.ControlStream(ctx)
	t.Cleanup(func() { _ = s1.CloseRequest(); _ = s1.CloseResponse() })
	require.NoError(t, s1.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_Hello{Hello: &pb.ControlHello{}}}))
	first, err := s1.Receive()
	require.NoError(t, err)
	require.NotNil(t, first.GetAccepted())

	// Act
	s2 := client.ControlStream(ctx)
	t.Cleanup(func() { _ = s2.CloseRequest(); _ = s2.CloseResponse() })
	require.NoError(t, s2.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_Hello{Hello: &pb.ControlHello{}}}))
	_, err = s2.Receive()

	// Assert
	require.Error(t, err)
	var connErr *connect.Error
	require.ErrorAs(t, err, &connErr)
	assert.Equal(t, connect.CodeFailedPrecondition, connErr.Code())
}

func TestControlStream_RequiresHelloFirst(t *testing.T) {
	// Arrange
	h := newControlHarness(t)
	client := startControlServer(t, h)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream := client.ControlStream(ctx)
	t.Cleanup(func() { _ = stream.CloseRequest(); _ = stream.CloseResponse() })

	// Act: skip Hello, send Ack directly
	require.NoError(t, stream.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_Ack{Ack: &pb.ControlAck{CommandId: "x"}}}))
	_, err := stream.Receive()

	// Assert
	require.Error(t, err)
	var connErr *connect.Error
	require.ErrorAs(t, err, &connErr)
	assert.Equal(t, connect.CodeInvalidArgument, connErr.Code())
}

func waitForSend(t *testing.T, r *fleetnodecontrol.Registry, fleetNodeID int64, commandID string, payload []byte) (<-chan fleetnodecontrol.CommandEvent, func(), error) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		events, cleanup, err := r.Send(context.Background(), fleetNodeID, &pb.ControlCommand{CommandId: commandID, Payload: payload})
		if err == nil {
			return events, cleanup, nil
		}
		if time.Now().After(deadline) {
			return nil, nil, err
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func startControlServer(t *testing.T, h *controlHarness) fleetnodegatewayv1connect.FleetNodeGatewayServiceClient {
	t.Helper()
	subject := &fleetnodeauth.Subject{FleetNodeID: h.fleetNodeID, OrgID: 1, Name: "agent-control"}
	mux := http.NewServeMux()
	mux.Handle(fleetnodegatewayv1connect.NewFleetNodeGatewayServiceHandler(
		h.handler,
		connect.WithInterceptors(interceptors.NewErrorMappingInterceptor(), agentSubjectInjector{subject: subject}),
	))
	srv := httptest.NewUnstartedServer(h2c.NewHandler(mux, &http2.Server{}))
	srv.Start()
	t.Cleanup(srv.Close)
	httpClient := &http.Client{
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, network, addr)
			},
		},
	}
	return fleetnodegatewayv1connect.NewFleetNodeGatewayServiceClient(httpClient, srv.URL, connect.WithGRPC())
}

type agentSubjectInjector struct {
	subject *fleetnodeauth.Subject
}

func (a agentSubjectInjector) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		return next(authn.SetInfo(ctx, a.subject), req)
	}
}

func (a agentSubjectInjector) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (a agentSubjectInjector) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		return next(authn.SetInfo(ctx, a.subject), conn)
	}
}
