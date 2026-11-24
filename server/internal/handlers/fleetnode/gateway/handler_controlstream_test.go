package gateway_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"database/sql"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"connectrpc.com/validate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
	"github.com/block/proto-fleet/server/internal/admissionctx"
	"github.com/block/proto-fleet/server/internal/domain/apikey"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
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
	_, _, err = enrollmentSvc.ConfirmExpected(t.Context(), agent.ID, 1, pendingEnrollmentID, 1)
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
	deliveries := make(chan string, 1)
	h.registry.WithDispatchObserver(func(_ context.Context, _, _, result string, duration time.Duration) {
		assert.GreaterOrEqual(t, duration, time.Duration(0))
		deliveries <- result
	})
	client := startControlServer(t, h)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream := client.ControlStream(ctx)
	t.Cleanup(func() { _ = stream.CloseRequest(); _ = stream.CloseResponse() })

	require.NoError(t, stream.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_Hello{Hello: &pb.ControlHello{
		MaxCommandProtocolVersion: pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1,
	}}}))
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
	select {
	case result := <-deliveries:
		require.Equal(t, "sent", result)
	case <-time.After(time.Second):
		t.Fatal("expected actual stream-send observation")
	}

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

func TestControlStream_RoutesMinerWebResponseAndAck(t *testing.T) {
	h := newControlHarness(t)
	client := startControlServer(t, h)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	stream := client.ControlStream(ctx)
	t.Cleanup(func() { _ = stream.CloseRequest(); _ = stream.CloseResponse() })
	require.NoError(t, stream.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_Hello{Hello: &pb.ControlHello{
		MaxCommandProtocolVersion: pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V8,
	}}}))
	accepted, err := stream.Receive()
	require.NoError(t, err)
	require.NotNil(t, accepted.GetAccepted())
	payload, err := proto.Marshal(&pb.AgentCommand{Command: &pb.AgentCommand_MinerWeb{MinerWeb: &pb.MinerWebRequest{
		Timeout: durationpb.New(5 * time.Second), Method: http.MethodGet, Path: "/api/v1/info",
	}}})
	require.NoError(t, err)
	type result struct {
		status  int
		headers http.Header
		body    []byte
		err     error
	}
	results := make(chan result, 1)
	go func() {
		response, err := h.registry.SendMinerWeb(ctx, h.fleetNodeID, &pb.ControlCommand{CommandId: "web-response", Payload: payload})
		if err != nil {
			results <- result{err: err}
			return
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		results <- result{status: response.StatusCode, headers: response.Header, body: body, err: err}
	}()
	dispatched, err := stream.Receive()
	require.NoError(t, err)
	require.NotNil(t, dispatched.GetCommand())
	require.Equal(t, "web-response", dispatched.GetCommand().GetCommandId())
	require.Equal(t, payload, dispatched.GetCommand().GetPayload())
	body := strings.Repeat("miner response\n", 10000)
	frames := []*pb.MinerWebResponseChunk{
		{CommandId: "web-response", Part: &pb.MinerWebResponseChunk_Head{Head: &pb.MinerWebResponseHead{StatusCode: http.StatusConflict, Headers: []*pb.MinerWebHeader{{Name: "Content-Type", Value: "text/plain"}, {Name: "Cache-Control", Value: "no-store"}}}}},
	}
	for offset := 0; offset < len(body); offset += control.MinerWebChunkBytes {
		end := min(offset+control.MinerWebChunkBytes, len(body))
		frames = append(frames, &pb.MinerWebResponseChunk{CommandId: "web-response", Part: &pb.MinerWebResponseChunk_Data{Data: []byte(body[offset:end])}})
	}
	frames = append(frames, &pb.MinerWebResponseChunk{CommandId: "web-response", Part: &pb.MinerWebResponseChunk_End{End: true}})
	for _, frame := range frames {
		require.NoError(t, stream.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_MinerWebResponse{MinerWebResponse: frame}}))
	}
	require.NoError(t, stream.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_Ack{Ack: &pb.ControlAck{CommandId: "web-response", Succeeded: true, Code: pb.AckCode_ACK_CODE_OK}}}))
	select {
	case got := <-results:
		require.NoError(t, got.err)
		require.Equal(t, http.StatusConflict, got.status)
		require.Equal(t, "text/plain", got.headers.Get("Content-Type"))
		require.Equal(t, "no-store", got.headers.Get("Cache-Control"))
		require.Equal(t, body, string(got.body))
	case <-ctx.Done():
		t.Fatal("miner response did not complete over ControlStream")
	}
}

func TestControlStream_LegacyNodeRejectsV1Command(t *testing.T) {
	h := newControlHarness(t)
	client := startControlServer(t, h)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream := client.ControlStream(ctx)
	t.Cleanup(func() { _ = stream.CloseRequest(); _ = stream.CloseResponse() })
	require.NoError(t, stream.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_Hello{Hello: &pb.ControlHello{}}}))
	accepted, err := stream.Receive()
	require.NoError(t, err)
	require.NotNil(t, accepted.GetAccepted())

	session, err := h.registry.Send(
		context.Background(),
		h.fleetNodeID,
		pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1,
		&pb.ControlCommand{CommandId: "unsupported"},
		nil,
		control.ReportKindDiscovery,
		nil,
	)

	assert.Nil(t, session)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	assert.Equal(t, []int64{h.fleetNodeID}, h.registry.ConnectedFleetNodeIDs())
}

func TestControlStream_DropsInvalidAck(t *testing.T) {
	// Arrange
	h := newControlHarness(t)
	client := startControlServer(t, h)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream := client.ControlStream(ctx)
	t.Cleanup(func() { _ = stream.CloseRequest(); _ = stream.CloseResponse() })

	require.NoError(t, stream.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_Hello{Hello: &pb.ControlHello{
		MaxCommandProtocolVersion: pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1,
	}}}))
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

func TestControlStream_AckPayloadLimit(t *testing.T) {
	registry := control.NewRegistry()
	h := &controlHarness{
		handler:     gateway.NewHandler(nil, nil, nil, registry, nil),
		registry:    registry,
		fleetNodeID: 42,
	}
	client := startControlServer(t, h)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	stream := client.ControlStream(ctx)
	t.Cleanup(func() { _ = stream.CloseRequest(); _ = stream.CloseResponse() })
	require.NoError(t, stream.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_Hello{Hello: &pb.ControlHello{
		MaxCommandProtocolVersion: pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1,
	}}}))
	first, err := stream.Receive()
	require.NoError(t, err)
	require.NotNil(t, first.GetAccepted())
	session := waitForSend(t, h.registry, h.fleetNodeID, "large-telemetry", []byte("command"))
	defer session.Close()
	command, err := stream.Receive()
	require.NoError(t, err)
	require.Equal(t, "large-telemetry", command.GetCommand().GetCommandId())
	const limit = 8 * 1024 * 1024
	payload := []byte(strings.Repeat("x", limit+1))
	// The gateway must discard an oversized ACK and continue accepting a full
	// 8 MiB ACK over native gRPC. This checks the receiver, beyond local encoding.
	for _, size := range []int{limit + 1, limit} {
		require.NoError(t, stream.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_Ack{Ack: &pb.ControlAck{
			CommandId: "large-telemetry", Succeeded: true, Code: pb.AckCode_ACK_CODE_OK, Payload: payload[:size],
		}}}))
	}
	select {
	case event := <-session.Events():
		require.NotNil(t, event.Ack)
		require.Equal(t, payload[:limit], event.Ack.GetPayload())
	case <-ctx.Done():
		t.Fatal("gateway did not accept the 8 MiB ACK")
	}
}

func TestControlStream_WriteEvidenceValidation(t *testing.T) {
	for _, tc := range []struct {
		name, outcome, stage, reason string
		oversized                    bool
	}{
		{"known", "DEVICE_WRITE_NOT_DISPATCHED", "validation", "invalid_request", false},
		{"unknown outcome", "DEVICE_WRITE_FUTURE", "dispatch", "finished", false},
		{"unknown stage", "DEVICE_WRITE_NOT_DISPATCHED", "future_stage", "rejected", false},
		{"unknown reason", "DEVICE_WRITE_NOT_DISPATCHED", "validation", "future_reason", false},
		{"empty evidence", "", "", "", false},
		{"oversized outcome", strings.Repeat("x", 65), "dispatch", "finished", true},
		{"oversized stage", "DEVICE_WRITE_NOT_DISPATCHED", strings.Repeat("x", 33), "rejected", true},
		{"oversized reason", "DEVICE_WRITE_NOT_DISPATCHED", "validation", strings.Repeat("x", 33), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry := control.NewRegistry()
			h := &controlHarness{handler: gateway.NewHandler(nil, nil, nil, registry, nil), registry: registry, fleetNodeID: 42}
			client := startControlServer(t, h)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			stream := client.ControlStream(ctx)
			t.Cleanup(func() { _ = stream.CloseRequest(); _ = stream.CloseResponse() })
			require.NoError(t, stream.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_Hello{Hello: &pb.ControlHello{
				MaxCommandProtocolVersion: pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1,
			}}}))
			accepted, err := stream.Receive()
			require.NoError(t, err)
			require.NotNil(t, accepted.GetAccepted())
			session := waitForSend(t, registry, h.fleetNodeID, "evidence", []byte("command"))
			defer session.Close()
			command, err := stream.Receive()
			require.NoError(t, err)
			require.Equal(t, "evidence", command.GetCommand().GetCommandId())
			for _, ack := range []*pb.ControlAck{
				{CommandId: "evidence", Code: pb.AckCode_ACK_CODE_INTERNAL, Payload: []byte("original result"),
					WriteOutcome: &pb.DeviceWriteOutcome{Outcome: tc.outcome, Stage: tc.stage, Reason: tc.reason}},
				{CommandId: "evidence", Code: pb.AckCode_ACK_CODE_OK, Succeeded: true, Payload: []byte("fallback")},
			} {
				require.NoError(t, stream.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_Ack{Ack: ack}}))
			}
			select {
			case event := <-session.Events():
				require.NotNil(t, event.Ack)
				if tc.oversized {
					require.Equal(t, pb.AckCode_ACK_CODE_OK, event.Ack.Code)
					require.True(t, event.Ack.Succeeded)
					require.Equal(t, []byte("fallback"), event.Ack.Payload)
				} else {
					require.Equal(t, pb.AckCode_ACK_CODE_INTERNAL, event.Ack.Code)
					require.False(t, event.Ack.Succeeded)
					require.Equal(t, []byte("original result"), event.Ack.Payload)
				}
			case <-ctx.Done():
				t.Fatal("gateway did not route the ACK")
			}
		})
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

func TestControlStream_RecordsHelloDiscoveryConfig(t *testing.T) {
	tests := []struct {
		name       string
		hello      *pb.ControlHello
		wantNil    bool
		wantRanges []string
	}{
		{
			name:       "ranges",
			hello:      &pb.ControlHello{DiscoveryConfig: &pb.DiscoveryConfig{Ranges: []string{"10.0.0.0-10.0.0.255", "192.168.1.1"}}},
			wantRanges: []string{"10.0.0.0-10.0.0.255", "192.168.1.1"},
		},
		{
			name:  "empty config means discovery disabled",
			hello: &pb.ControlHello{DiscoveryConfig: &pb.DiscoveryConfig{}},
		},
		{
			name:    "missing config means node did not report",
			hello:   &pb.ControlHello{},
			wantNil: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			h := newControlHarness(t)
			client := startControlServer(t, h)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			stream := client.ControlStream(ctx)
			t.Cleanup(func() { _ = stream.CloseRequest(); _ = stream.CloseResponse() })

			// Act
			require.NoError(t, stream.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_Hello{Hello: tt.hello}}))
			accepted, err := stream.Receive()

			// Assert
			require.NoError(t, err)
			require.NotNil(t, accepted.GetAccepted())
			config := h.registry.DiscoveryConfig(h.fleetNodeID)
			if tt.wantNil {
				require.Nil(t, config)
			} else {
				require.NotNil(t, config)
				require.Equal(t, tt.wantRanges, config.GetRanges())
			}

			// Act
			require.NoError(t, stream.CloseRequest())
			require.NoError(t, stream.CloseResponse())

			// Assert
			require.Eventually(t, func() bool { return !h.registry.Connected(h.fleetNodeID) }, 2*time.Second, 10*time.Millisecond)
			require.Nil(t, h.registry.DiscoveryConfig(h.fleetNodeID))
		})
	}
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

type controlledAdmissionGate struct {
	active context.Context //nolint:containedctx // Test-owned activation lifetime.
	reject bool
}

func (g controlledAdmissionGate) Admit(ctx context.Context) (context.Context, func(), error) {
	if g.reject {
		return nil, nil, errors.New("passive")
	}
	requestCtx, cancelRequest := context.WithCancel(admissionctx.WithActiveLifetime(ctx, g.active))
	stop := context.AfterFunc(g.active, cancelRequest)
	return requestCtx, func() {
		stop()
		cancelRequest()
	}, nil
}

func TestControlStream_ReturnsStructuredNotActive(t *testing.T) {
	t.Run("passive admission", func(t *testing.T) {
		client := startControlServerWithAdmission(t, controlledAdmissionGate{reject: true})
		stream := client.ControlStream(t.Context())
		// Admission can reject before the first client write completes. In that
		// race Send returns EOF, while Receive still carries the structured RPC
		// status this test is asserting.
		_ = stream.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_Hello{Hello: &pb.ControlHello{}}})

		_, err := stream.Receive()

		requireStructuredNotActive(t, err)
	})

	t.Run("demotion before hello", func(t *testing.T) {
		activeCtx, cancelActive := context.WithCancel(t.Context())
		client := startControlServerWithAdmission(t, controlledAdmissionGate{active: activeCtx})
		stream := client.ControlStream(t.Context())
		require.NoError(t, stream.Send(nil), "start the RPC without sending Hello")

		cancelActive()
		_, err := stream.Receive()

		requireStructuredNotActive(t, err)
	})

	t.Run("accepted stream demotion", func(t *testing.T) {
		activeCtx, cancelActive := context.WithCancel(t.Context())
		client := startControlServerWithAdmission(t, controlledAdmissionGate{active: activeCtx})
		stream := client.ControlStream(t.Context())
		require.NoError(t, stream.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_Hello{Hello: &pb.ControlHello{}}}))
		accepted, err := stream.Receive()
		require.NoError(t, err)
		require.NotNil(t, accepted.GetAccepted())

		cancelActive()
		_, err = stream.Receive()

		requireStructuredNotActive(t, err)
	})
}

func TestControlStream_SendsPeriodicLiveness(t *testing.T) {
	previous := gateway.ControlStreamLivenessInterval
	gateway.ControlStreamLivenessInterval = 20 * time.Millisecond
	t.Cleanup(func() { gateway.ControlStreamLivenessInterval = previous })

	activeCtx, cancelActive := context.WithCancel(t.Context())
	defer cancelActive()
	client := startControlServerWithAdmission(t, controlledAdmissionGate{active: activeCtx})
	stream := client.ControlStream(t.Context())
	t.Cleanup(func() { _ = stream.CloseRequest(); _ = stream.CloseResponse() })
	require.NoError(t, stream.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_Hello{Hello: &pb.ControlHello{}}}))

	first, err := stream.Receive()
	require.NoError(t, err)
	require.NotNil(t, first.GetAccepted())
	second, err := stream.Receive()
	require.NoError(t, err)
	require.NotNil(t, second.GetAccepted())
}

func TestControlStream_NegotiatesKeepalive(t *testing.T) {
	tests := []struct {
		name          string
		version       pb.CommandProtocolVersion
		wantKeepalive time.Duration
	}{
		{name: "pre-keepalive node", version: pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V6, wantKeepalive: 0},
		{name: "keepalive node", version: pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V7, wantKeepalive: gateway.ControlStreamKeepaliveInterval},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			activeCtx, cancelActive := context.WithCancel(t.Context())
			defer cancelActive()
			client := startControlServerWithAdmission(t, controlledAdmissionGate{active: activeCtx})
			stream := client.ControlStream(t.Context())
			t.Cleanup(func() { _ = stream.CloseRequest(); _ = stream.CloseResponse() })

			require.NoError(t, stream.Send(controlHello(tc.version)))
			first, err := stream.Receive()

			require.NoError(t, err)
			assert.Equal(t, tc.wantKeepalive, first.GetAccepted().GetKeepaliveInterval().AsDuration())
		})
	}
}

func TestControlStream_ClosesSilentKeepaliveStream(t *testing.T) {
	setKeepaliveInterval(t, 20*time.Millisecond)
	activeCtx, cancelActive := context.WithCancel(t.Context())
	defer cancelActive()
	client := startControlServerWithAdmission(t, controlledAdmissionGate{active: activeCtx})
	stream := client.ControlStream(t.Context())
	t.Cleanup(func() { _ = stream.CloseRequest(); _ = stream.CloseResponse() })
	require.NoError(t, stream.Send(controlHello(pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V7)))

	var err error
	for err == nil {
		_, err = stream.Receive()
	}

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeUnavailable, connectErr.Code())
}

func TestControlStream_PingsKeepStreamOpen(t *testing.T) {
	setKeepaliveInterval(t, 20*time.Millisecond)
	activeCtx, cancelActive := context.WithCancel(t.Context())
	defer cancelActive()
	client := startControlServerWithAdmission(t, controlledAdmissionGate{active: activeCtx})
	stream := client.ControlStream(t.Context())
	t.Cleanup(func() { _ = stream.CloseRequest(); _ = stream.CloseResponse() })
	require.NoError(t, stream.Send(controlHello(pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V7)))
	pingCtx, stopPings := context.WithCancel(t.Context())
	defer stopPings()
	go func() {
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-pingCtx.Done():
				return
			case <-ticker.C:
				if stream.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_Ping{Ping: &pb.ControlPing{}}}) != nil {
					return
				}
			}
		}
	}()

	var received int
	var err error
	for deadline := time.Now().Add(200 * time.Millisecond); time.Now().Before(deadline) && err == nil; received++ {
		_, err = stream.Receive()
	}

	require.NoError(t, err)
	assert.Greater(t, received, 3)
}

func TestControlStream_DropsSilentStreamWhileSendIsBlocked(t *testing.T) {
	for _, tc := range []struct {
		name  string
		block func(*pb.ControlStreamResponse) bool
	}{
		{name: "initial acceptance", block: func(response *pb.ControlStreamResponse) bool { return response.GetAccepted() != nil }},
		{name: "command", block: func(response *pb.ControlStreamResponse) bool { return response.GetCommand() != nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setKeepaliveInterval(t, 20*time.Millisecond)
			h := newControlHarness(t)
			// A half-open connection blocks the handler in Send until TCP gives up.
			release := make(chan struct{})
			blocked := make(chan struct{}, 1)
			t.Cleanup(func() { close(release) })
			client := startControlServer(t, h, connect.WithInterceptors(blockControlSends{release: release, blocked: blocked, block: tc.block}))
			stream := client.ControlStream(t.Context())
			t.Cleanup(func() { _ = stream.CloseRequest(); _ = stream.CloseResponse() })
			require.NoError(t, stream.Send(controlHello(pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V7)))

			session := waitForSend(t, h.registry, h.fleetNodeID, "blocked", []byte("payload"))
			defer session.Close()
			select {
			case <-blocked:
			case <-time.After(5 * time.Second):
				t.Fatal("expected handler to block in Send")
			}

			assert.Eventually(t, func() bool { return !h.registry.Connected(h.fleetNodeID) }, 5*time.Second, 10*time.Millisecond,
				"a silent stream leaves the registry even while Send is blocked")
		})
	}
}

func TestControlStream_KeepsSilentLegacyStreamOpen(t *testing.T) {
	const interval = 20 * time.Millisecond
	setKeepaliveInterval(t, interval)
	h := newControlHarness(t)
	client := startControlServer(t, h)
	stream := client.ControlStream(t.Context())
	t.Cleanup(func() { _ = stream.CloseRequest(); _ = stream.CloseResponse() })
	require.NoError(t, stream.Send(controlHello(pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V6)))
	accepted, err := stream.Receive()
	require.NoError(t, err)
	require.NotNil(t, accepted.GetAccepted())

	time.Sleep(4 * control.KeepaliveMisses * interval)

	assert.True(t, h.registry.Connected(h.fleetNodeID), "V6 nodes never ping, so silence must not close them")
}

func controlHello(version pb.CommandProtocolVersion) *pb.ControlStreamRequest {
	return &pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_Hello{Hello: &pb.ControlHello{MaxCommandProtocolVersion: version}}}
}

func setKeepaliveInterval(t *testing.T, interval time.Duration) {
	t.Helper()
	previous := gateway.ControlStreamKeepaliveInterval
	gateway.ControlStreamKeepaliveInterval = interval
	t.Cleanup(func() { gateway.ControlStreamKeepaliveInterval = previous })
}

func requireStructuredNotActive(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	require.Equal(t, connect.CodeUnavailable, connectErr.Code())
	for _, detail := range connectErr.Details() {
		value, valueErr := detail.Value()
		require.NoError(t, valueErr)
		if fleetDetails, ok := value.(*commonpb.FleetErrorDetails); ok {
			require.Equal(t, commonpb.FleetErrorCode_FLEET_ERROR_CODE_NOT_ACTIVE, fleetDetails.GetCommon())
			return
		}
	}
	t.Fatal("missing FleetErrorDetails")
}

func startControlServerWithAdmission(t *testing.T, gate controlledAdmissionGate) fleetnodegatewayv1connect.FleetNodeGatewayServiceClient {
	t.Helper()
	const fleetNodeID = 42
	subject := &auth.Subject{FleetNodeID: fleetNodeID, OrgID: 1, Name: "agent-control"}
	mux := http.NewServeMux()
	mux.Handle(fleetnodegatewayv1connect.NewFleetNodeGatewayServiceHandler(
		gateway.NewHandler(nil, nil, nil, control.NewRegistry(), nil),
		connect.WithInterceptors(
			interceptors.NewErrorMappingInterceptor(),
			interceptors.NewActiveInterceptor(gate),
			agentSubjectInjector{subject: subject},
		),
	))
	srv := testutil.NewH2CServer(t, mux)
	return fleetnodegatewayv1connect.NewFleetNodeGatewayServiceClient(testutil.NewH2CClient(), srv.URL, connect.WithGRPC())
}

func waitForSend(t *testing.T, r *control.Registry, fleetNodeID int64, commandID string, payload []byte) *control.Session {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		session, err := r.Send(context.Background(), fleetNodeID, pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, &pb.ControlCommand{CommandId: commandID, Payload: payload}, nil, control.ReportKindDiscovery, nil)
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

type blockControlSends struct {
	release <-chan struct{}
	blocked chan<- struct{}
	block   func(*pb.ControlStreamResponse) bool
}

func (b blockControlSends) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc { return next }

func (b blockControlSends) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (b blockControlSends) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		return next(ctx, blockedSendConn{StreamingHandlerConn: conn, blockControlSends: b})
	}
}

type blockedSendConn struct {
	connect.StreamingHandlerConn
	blockControlSends
}

func (c blockedSendConn) Send(msg any) error {
	if response, ok := msg.(*pb.ControlStreamResponse); ok && c.block(response) {
		select {
		case c.blocked <- struct{}{}:
		default:
		}
		<-c.release
	}
	return c.StreamingHandlerConn.Send(msg) //nolint:wrapcheck // passthrough to the real stream
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

func TestControlStream_RelaysCommandFramesAndCancels(t *testing.T) {
	h := newControlHarness(t)
	client := startControlServer(t, h)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	stream := client.ControlStream(ctx)
	t.Cleanup(func() { _ = stream.CloseRequest(); _ = stream.CloseResponse() })
	require.NoError(t, stream.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_Hello{Hello: &pb.ControlHello{
		MaxCommandProtocolVersion: control.CommandStreamProtocolVersion,
	}}}))
	accepted, err := stream.Receive()
	require.NoError(t, err)
	require.NotNil(t, accepted.GetAccepted())

	viewerGone := errors.New("viewer gone")
	frames := make(chan []byte, 1)
	results := make(chan error, 1)
	go func() {
		_, err := h.registry.StreamCommand(ctx, h.fleetNodeID, &pb.ControlCommand{CommandId: "asic-stream", Payload: []byte{1}}, control.CommandStream{
			Kind: "test", MinimumVersion: control.CommandStreamProtocolVersion,
			OnFrame: func(payload []byte) error {
				frames <- payload
				return viewerGone
			},
		})
		results <- err
	}()
	dispatched, err := stream.Receive()
	require.NoError(t, err)
	require.Equal(t, "asic-stream", dispatched.GetCommand().GetCommandId())

	require.NoError(t, stream.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_CommandFrame{CommandFrame: &pb.CommandFrame{
		CommandId: "asic-stream", Payload: []byte("frame"),
	}}}))
	select {
	case payload := <-frames:
		require.Equal(t, []byte("frame"), payload)
	case <-ctx.Done():
		t.Fatal("command frame did not reach the caller")
	}
	require.ErrorIs(t, <-results, viewerGone)
	cancelled, err := stream.Receive()
	require.NoError(t, err)
	require.Equal(t, "asic-stream", cancelled.GetCancel().GetCommandId(), "the node is told to stop the stream")
}

// An oversized command frame ends its own stream. It must not fail message
// validation, which would end the node's control session and every command on it.
func TestControlStream_OversizedCommandFrameEndsOnlyItsStream(t *testing.T) {
	const fleetNodeID = 42
	registry := control.NewRegistry()
	mux := http.NewServeMux()
	mux.Handle(fleetnodegatewayv1connect.NewFleetNodeGatewayServiceHandler(
		gateway.NewHandler(nil, nil, nil, registry, nil),
		// The interceptors fleetd puts in front of the gateway.
		connect.WithInterceptors(
			interceptors.NewErrorMappingInterceptor(),
			validate.NewInterceptor(),
			agentSubjectInjector{subject: &auth.Subject{FleetNodeID: fleetNodeID, OrgID: 1, Name: "agent-control"}},
		),
	))
	srv := testutil.NewH2CServer(t, mux)
	client := fleetnodegatewayv1connect.NewFleetNodeGatewayServiceClient(testutil.NewH2CClient(), srv.URL, connect.WithGRPC())
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	stream := client.ControlStream(ctx)
	t.Cleanup(func() { _ = stream.CloseRequest(); _ = stream.CloseResponse() })
	require.NoError(t, stream.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_Hello{Hello: &pb.ControlHello{
		MaxCommandProtocolVersion: control.CommandStreamProtocolVersion,
	}}}))
	accepted, err := stream.Receive()
	require.NoError(t, err)
	require.NotNil(t, accepted.GetAccepted())

	type result struct {
		ack *pb.ControlAck
		err error
	}
	open := func(id string, frames chan<- []byte) <-chan result {
		done := make(chan result, 1)
		go func() {
			ack, err := registry.StreamCommand(ctx, fleetNodeID, &pb.ControlCommand{CommandId: id, Payload: []byte{1}}, control.CommandStream{
				Kind: "test", MinimumVersion: control.CommandStreamProtocolVersion,
				OnFrame: func(payload []byte) error { frames <- payload; return nil },
			})
			done <- result{ack, err}
		}()
		dispatched, err := stream.Receive()
		require.NoError(t, err)
		require.Equal(t, id, dispatched.GetCommand().GetCommandId())
		return done
	}
	frame := func(id string, payload []byte) *pb.ControlStreamRequest {
		return &pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_CommandFrame{CommandFrame: &pb.CommandFrame{CommandId: id, Payload: payload}}}
	}
	bigFrames, otherFrames := make(chan []byte, 1), make(chan []byte, 1)
	big := open("big", bigFrames)
	other := open("other", otherFrames)

	require.NoError(t, stream.Send(frame("big", make([]byte, control.MaxCommandFrameBytes+1))))
	got := <-big
	require.True(t, fleeterror.IsUnavailableError(got.err), "got %v", got.err)
	require.Empty(t, bigFrames, "the oversized payload never reaches the caller")
	cancelled, err := stream.Receive()
	require.NoError(t, err, "the control session stays up")
	require.Equal(t, "big", cancelled.GetCancel().GetCommandId(), "the node is told to stop the stream")

	// The other command on the same session still relays frames and its ack.
	require.NoError(t, stream.Send(frame("other", []byte("frame"))))
	require.Equal(t, []byte("frame"), <-otherFrames)
	require.NoError(t, stream.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_Ack{Ack: &pb.ControlAck{CommandId: "other", Code: pb.AckCode_ACK_CODE_OK, Succeeded: true}}}))
	done := <-other
	require.NoError(t, done.err)
	require.True(t, done.ack.GetSucceeded())
}
