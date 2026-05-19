package fleetnodebootstrap

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
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
	srv := httptest.NewUnstartedServer(h2c.NewHandler(mux, &http2.Server{}))
	srv.Start()
	t.Cleanup(srv.Close)

	var token string
	client := NewAuthenticatedGatewayClient(srv.URL, func() string { return token })

	// Act
	token = "t1"
	_, err := client.UploadHeartbeat(context.Background(), connect.NewRequest(&pb.UploadHeartbeatRequest{SentAt: timestamppb.Now()}))
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
	srv := httptest.NewUnstartedServer(h2c.NewHandler(http.NewServeMux(), &http2.Server{}))
	srv.Start()
	t.Cleanup(srv.Close)
	client := NewAuthenticatedGatewayClient(srv.URL, func() string { return "" })

	// Act
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := client.UploadHeartbeat(ctx, connect.NewRequest(&pb.UploadHeartbeatRequest{SentAt: timestamppb.Now()}))

	// Assert
	require.Error(t, err)
	assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
}
