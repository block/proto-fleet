package bootstrap

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
	"github.com/block/proto-fleet/server/internal/testutil"
)

type captureGateway struct {
	fleetnodegatewayv1connect.UnimplementedFleetNodeGatewayServiceHandler

	mu                 sync.Mutex
	authHeadersSeen    []string
	heartbeatProtocols []string
	controlProtocols   []string
}

func (c *captureGateway) UploadHeartbeat(_ context.Context, req *connect.Request[pb.UploadHeartbeatRequest]) (*connect.Response[pb.UploadHeartbeatResponse], error) {
	c.mu.Lock()
	c.authHeadersSeen = append(c.authHeadersSeen, req.Header().Get("Authorization"))
	c.heartbeatProtocols = append(c.heartbeatProtocols, req.Peer().Protocol)
	c.mu.Unlock()
	return connect.NewResponse(&pb.UploadHeartbeatResponse{ReceivedAt: timestamppb.Now()}), nil
}

func (c *captureGateway) ControlStream(_ context.Context, stream *connect.BidiStream[pb.ControlStreamRequest, pb.ControlStreamResponse]) error {
	c.mu.Lock()
	c.authHeadersSeen = append(c.authHeadersSeen, stream.RequestHeader().Get("Authorization"))
	c.controlProtocols = append(c.controlProtocols, stream.Peer().Protocol)
	c.mu.Unlock()
	if _, err := stream.Receive(); err != nil {
		return fmt.Errorf("receive control hello: %w", err)
	}
	if err := stream.Send(&pb.ControlStreamResponse{Kind: &pb.ControlStreamResponse_Accepted{
		Accepted: &pb.ControlAccepted{ServerTime: timestamppb.Now()},
	}}); err != nil {
		return fmt.Errorf("send control accepted: %w", err)
	}
	return nil
}

func (c *captureGateway) headers() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.authHeadersSeen))
	copy(out, c.authHeadersSeen)
	return out
}

func (c *captureGateway) protocols() ([]string, []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.heartbeatProtocols...), append([]string(nil), c.controlProtocols...)
}

func TestAuthenticatedClient_AttachesBearerHeaderPerCall(t *testing.T) {
	t.Parallel()

	// Arrange
	fake := &captureGateway{}
	mux := http.NewServeMux()
	path, h := fleetnodegatewayv1connect.NewFleetNodeGatewayServiceHandler(fake)
	mux.Handle(path, h)
	srv := testutil.NewH2CServer(t, mux)

	var token string
	client, err := NewAuthenticatedGatewayClient(srv.URL, func() string { return token })
	require.NoError(t, err)

	// Act
	token = "t1"
	_, err = client.UploadHeartbeat(context.Background(), connect.NewRequest(&pb.UploadHeartbeatRequest{SentAt: timestamppb.Now()}))
	require.NoError(t, err)
	token = "t2"
	_, err = client.UploadHeartbeat(context.Background(), connect.NewRequest(&pb.UploadHeartbeatRequest{SentAt: timestamppb.Now()}))
	require.NoError(t, err)

	// Assert
	got := fake.headers()
	require.Len(t, got, 2)
	assert.Equal(t, "Bearer t1", got[0])
	assert.Equal(t, "Bearer t2", got[1])
}

func TestAuthenticatedClient_RejectsEmptyToken(t *testing.T) {
	t.Parallel()

	// Arrange
	srv := testutil.NewH2CServer(t, http.NewServeMux())
	client, err := NewAuthenticatedGatewayClient(srv.URL, func() string { return "" })
	require.NoError(t, err)

	// Act
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = client.UploadHeartbeat(ctx, connect.NewRequest(&pb.UploadHeartbeatRequest{SentAt: timestamppb.Now()}))

	// Assert
	require.Error(t, err)
	assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
}

func TestAuthenticatedClient_UsesGRPCOnlyForControlStream(t *testing.T) {
	t.Parallel()

	// Arrange
	fake := &captureGateway{}
	mux := http.NewServeMux()
	path, h := fleetnodegatewayv1connect.NewFleetNodeGatewayServiceHandler(fake)
	mux.Handle(path, h)
	srv := testutil.NewH2CServer(t, mux)
	client, err := NewAuthenticatedGatewayClient(srv.URL, func() string { return "session-token" })
	require.NoError(t, err)

	// Act: a representative unary RPC remains Connect, while ControlStream uses gRPC.
	_, err = client.UploadHeartbeat(t.Context(), connect.NewRequest(&pb.UploadHeartbeatRequest{SentAt: timestamppb.Now()}))
	require.NoError(t, err)
	stream := client.ControlStream(t.Context())
	require.NoError(t, stream.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_Hello{Hello: &pb.ControlHello{}}}))
	_, err = stream.Receive()
	require.NoError(t, err)
	require.NoError(t, stream.CloseRequest())
	require.NoError(t, stream.CloseResponse())

	// Assert
	heartbeatProtocols, controlProtocols := fake.protocols()
	require.Equal(t, []string{connect.ProtocolConnect}, heartbeatProtocols)
	require.Equal(t, []string{connect.ProtocolGRPC}, controlProtocols)
	require.Equal(t, []string{"Bearer session-token", "Bearer session-token"}, fake.headers())
}
