package gateway_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"database/sql"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
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
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/auth"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/enrollment"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/pairing"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/handlers/fleetnode/gateway"
	"github.com/block/proto-fleet/server/internal/handlers/interceptors"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
	"github.com/block/proto-fleet/server/internal/testutil"
)

type controlHarness struct {
	handler     *gateway.Handler
	registry    *control.Registry
	files       *files.Service
	fleetNodeID int64
	db          *sql.DB
}

func newControlHarness(t *testing.T) *controlHarness {
	t.Helper()
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	t.Chdir(t.TempDir())

	db := testutil.GetTestDB(t)
	_, err := db.Exec(`INSERT INTO organization (id, org_id, name) VALUES (1, 'test-org', 'Test Org') ON CONFLICT DO NOTHING`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO "user" (id, user_id, username, password_hash) VALUES (1, 'test-user', 'op', 'dummy') ON CONFLICT DO NOTHING`)
	require.NoError(t, err)

	apiKeyStore := sqlstores.NewSQLApiKeyStore(db)
	apiKeySvc := apikey.NewService(apiKeyStore, nil)
	transactor := sqlstores.NewSQLTransactor(db)
	enrollmentStore := sqlstores.NewSQLFleetNodeEnrollmentStore(db)
	enrollmentSvc := enrollment.NewService(enrollmentStore, apiKeySvc, transactor, nil)
	authStore := sqlstores.NewSQLFleetNodeAuthStore(db)
	authSvc := auth.NewService(authStore, enrollmentStore, apiKeySvc)
	pairingStore := sqlstores.NewSQLFleetNodePairingStore(db)
	registry := control.NewRegistry()
	pairingSvc := pairing.NewService(pairingStore, enrollmentStore, transactor).
		WithProvisioning(sqlstores.NewSQLDeviceStore(db), sqlstores.NewSQLDiscoveredDeviceStore(db), registry)

	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	code, pendingEnrollmentID, _, err := enrollmentSvc.CreateCodeWithEnrollmentID(t.Context(), 1, 1, time.Hour)
	require.NoError(t, err)
	agent, _, err := enrollmentSvc.RegisterFleetNode(t.Context(), code, "agent-control", pubKey, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	// Confirm the node so pairDeviceLocked (which requires CONFIRMED) can bind
	// devices during ReportPairedDevices persistence.
	_, _, err = enrollmentSvc.ConfirmExpected(t.Context(), agent.ID, 1, pendingEnrollmentID)
	require.NoError(t, err)
	filesService, err := files.NewService(files.Config{})
	require.NoError(t, err)

	return &controlHarness{
		handler:     gateway.NewHandler(enrollmentSvc, authSvc, pairingSvc, registry, filesService),
		registry:    registry,
		files:       filesService,
		fleetNodeID: agent.ID,
		db:          db,
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
	session := waitForSend(t, h.registry, h.fleetNodeID, "cmd-1", []byte("payload"))
	defer session.Close()

	got, err := stream.Receive()
	require.NoError(t, err)
	cmd := got.GetCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "cmd-1", cmd.GetCommandId())

	// Act: agent acks
	require.NoError(t, stream.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_Ack{Ack: &pb.ControlAck{CommandId: "cmd-1", Succeeded: true}}}))

	// Assert
	select {
	case ev := <-session.Events():
		require.NotNil(t, ev.Ack)
		assert.True(t, ev.Ack.GetSucceeded())
	case <-time.After(time.Second):
		t.Fatal("expected ack on events channel")
	}
}

func TestControlStream_DropsInvalidAck(t *testing.T) {
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
	require.NotNil(t, first.GetAccepted())

	session := waitForSend(t, h.registry, h.fleetNodeID, "cmd-1", []byte("payload"))
	defer session.Close()
	got, err := stream.Receive()
	require.NoError(t, err)
	require.Equal(t, "cmd-1", got.GetCommand().GetCommandId())

	// Act: the node sends an ack whose error_message exceeds the 4096-byte cap, then a valid ack.
	require.NoError(t, stream.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_Ack{Ack: &pb.ControlAck{
		CommandId: "cmd-1", ErrorMessage: strings.Repeat("x", 5000),
	}}}))
	require.NoError(t, stream.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_Ack{Ack: &pb.ControlAck{
		CommandId: "cmd-1", Succeeded: true,
	}}}))

	// Assert: the gateway dropped the invalid ack; only the valid ack is routed to the operator.
	select {
	case ev := <-session.Events():
		require.NotNil(t, ev.Ack)
		assert.True(t, ev.Ack.GetSucceeded(), "oversized ack must be dropped, only the valid ack delivered")
		assert.Empty(t, ev.Ack.GetErrorMessage())
	case <-time.After(2 * time.Second):
		t.Fatal("expected the valid ack on the events channel")
	}
}

func TestControlStream_SecondStreamEvictsFirst(t *testing.T) {
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

	// Act: second stream for the same node
	s2 := client.ControlStream(ctx)
	t.Cleanup(func() { _ = s2.CloseRequest(); _ = s2.CloseResponse() })
	require.NoError(t, s2.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_Hello{Hello: &pb.ControlHello{}}}))
	secondAccepted, err := s2.Receive()

	// Assert: second is accepted (newest-wins)
	require.NoError(t, err)
	require.NotNil(t, secondAccepted.GetAccepted())

	// Assert: first stream sees its session end (server closed it)
	_, recvErr := s1.Receive()
	require.Error(t, recvErr)
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

func waitForSend(t *testing.T, r *control.Registry, fleetNodeID int64, commandID string, payload []byte) *control.Session {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		session, err := r.Send(context.Background(), fleetNodeID, &pb.ControlCommand{CommandId: commandID, Payload: payload}, nil, control.ReportKindDiscovery, nil)
		if err == nil {
			return session
		}
		if time.Now().After(deadline) {
			t.Fatalf("waitForSend: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func startControlServer(t *testing.T, h *controlHarness, opts ...connect.HandlerOption) fleetnodegatewayv1connect.FleetNodeGatewayServiceClient {
	t.Helper()
	subject := &auth.Subject{FleetNodeID: h.fleetNodeID, OrgID: 1, Name: "agent-control"}
	mux := http.NewServeMux()
	handlerOptions := []connect.HandlerOption{
		connect.WithInterceptors(interceptors.NewErrorMappingInterceptor(), agentSubjectInjector{subject: subject}),
	}
	handlerOptions = append(handlerOptions, opts...)
	mux.Handle(fleetnodegatewayv1connect.NewFleetNodeGatewayServiceHandler(
		h.handler,
		handlerOptions...,
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
	subject *auth.Subject
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
