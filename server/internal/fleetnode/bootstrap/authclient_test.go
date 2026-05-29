package bootstrap

import (
	"context"
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

	mu              sync.Mutex
	authHeadersSeen []string
}

func (c *captureGateway) UploadHeartbeat(_ context.Context, req *connect.Request[pb.UploadHeartbeatRequest]) (*connect.Response[pb.UploadHeartbeatResponse], error) {
	c.mu.Lock()
	c.authHeadersSeen = append(c.authHeadersSeen, req.Header().Get("Authorization"))
	c.mu.Unlock()
	return connect.NewResponse(&pb.UploadHeartbeatResponse{ReceivedAt: timestamppb.Now()}), nil
}

func (c *captureGateway) headers() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.authHeadersSeen))
	copy(out, c.authHeadersSeen)
	return out
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
