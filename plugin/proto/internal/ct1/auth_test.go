package ct1

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestConfiguredNativeBearerTokenIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(path, []byte("test-token\n"), 0o600))
	t.Setenv("CT1_RPC_TOKEN_FILE", path)
	c, err := NewClient(sdk.DeviceInfo{Host: "127.0.0.1", Port: 8080, URLScheme: "http"}, sdk.UsernamePassword{})
	if c != nil {
		t.Cleanup(func() { _ = c.Close() })
	}
	require.ErrorContains(t, err, "CT1_RPC_TOKEN_FILE is unsupported")
	require.Nil(t, c, "unsupported credentials must prevent creating a controller connection")
	require.NotContains(t, err.Error(), path)
	require.NotContains(t, err.Error(), "test-token")
}

func TestDiscoveryNeverSendsCredentials(t *testing.T) {
	t.Setenv("CT1_RPC_TOKEN_FILE", "")
	for _, outcome := range []string{"controller", "unknown_endpoint", "authentication_required"} {
		t.Run(outcome, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			received := make(chan metadata.MD, 1)
			server := grpc.NewServer(grpc.UnaryInterceptor(func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
				md, _ := metadata.FromIncomingContext(ctx)
				received <- md.Copy()
				switch outcome {
				case "unknown_endpoint":
					return &mdk.GetSystemInfoResponse{Info: &mdk.SystemInfo{Id: "unknown", Product: "other"}}, nil
				case "authentication_required":
					return nil, status.Error(codes.Unauthenticated, "authentication required") //nolint:wrapcheck // gRPC server status
				default:
					return handler(ctx, req) //nolint:wrapcheck // gRPC server handler
				}
			}))
			mdk.RegisterSystemServiceServer(server, &grpcFixture{})
			go func() { _ = server.Serve(listener) }()
			t.Cleanup(server.Stop)
			host, portText, err := net.SplitHostPort(listener.Addr().String())
			require.NoError(t, err)
			port, err := sdk.ParsePort(portText)
			require.NoError(t, err)
			info := sdk.DeviceInfo{Host: host, Port: 8080, URLScheme: "http", RPCPort: port}
			c, err := NewClient(info, sdk.UsernamePassword{Username: "admin", Password: "rest-password"})
			require.NoError(t, err)
			t.Cleanup(func() { _ = c.Close() })
			_, err = c.Discover(t.Context(), info)
			switch outcome {
			case "controller":
				require.NoError(t, err)
			case "unknown_endpoint":
				require.ErrorContains(t, err, "endpoint is not a CT1 container")
			case "authentication_required":
				require.Equal(t, codes.Unauthenticated, status.Code(err))
			}
			select {
			case md := <-received:
				require.Empty(t, md.Get("authorization"), "neither shared tokens nor saved REST credentials may be sent")
				require.Equal(t, []string{"proto-fleet"}, md.Get("x-frigate-actor"), "audit attribution is not authentication")
			default:
				t.Fatal("discovery did not reach the test endpoint")
			}
		})
	}
}
