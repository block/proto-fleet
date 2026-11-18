package ct1

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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
	c, err := NewClient(sdk.DeviceInfo{Host: "127.0.0.1", Port: 8080, URLScheme: "http", RPCPort: DefaultRPCPort}, sdk.UsernamePassword{})
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
			received := make(chan metadata.MD, 2)
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
			count := 1
			if outcome == "controller" {
				count = 2
			}
			for range count {
				select {
				case md := <-received:
					require.Empty(t, md.Get("authorization"), "neither shared tokens nor saved REST credentials may be sent")
					require.Equal(t, []string{"proto-fleet"}, md.Get("x-frigate-actor"), "audit attribution is not authentication")
				default:
					t.Fatal("discovery did not reach the test endpoint")
				}
			}
		})
	}
}

// newTokenGatedFixture mirrors Frigate's mdk_auth interceptors: every unary and
// streaming RPC needs exactly one matching bearer value.
func newTokenGatedFixture(t *testing.T, token string, fixture *grpcFixture) (sdk.DeviceInfo, <-chan []string) {
	t.Helper()
	return newRotatingTokenFixture(t, func() string { return token }, fixture)
}

func newRotatingTokenFixture(t *testing.T, current func() string, fixture *grpcFixture) (sdk.DeviceInfo, <-chan []string) {
	t.Helper()
	seen := make(chan []string, 64)
	check := func(ctx context.Context) error {
		md, _ := metadata.FromIncomingContext(ctx)
		values := md.Get("authorization")
		select {
		case seen <- values:
		default:
		}
		if len(values) != 1 || values[0] != "Bearer "+current() {
			return status.Error(codes.Unauthenticated, "invalid or missing MDK bearer token") //nolint:wrapcheck // gRPC server status
		}
		return nil
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer(
		grpc.UnaryInterceptor(func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			if err := check(ctx); err != nil {
				return nil, err
			}
			return handler(ctx, req) //nolint:wrapcheck // gRPC server handler
		}),
		grpc.StreamInterceptor(func(srv any, stream grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
			if err := check(stream.Context()); err != nil {
				return err
			}
			return handler(srv, stream)
		}),
	)
	mdk.RegisterSystemServiceServer(server, fixture)
	mdk.RegisterHashboardServiceServer(server, fixture)
	mdk.RegisterMiningServiceServer(server, fixture)
	mdk.RegisterTelemetryServiceServer(server, fixture)
	mdk.RegisterPowerServiceServer(server, fixture)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	host, portText, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	port, err := sdk.ParsePort(portText)
	require.NoError(t, err)
	return sdk.DeviceInfo{Host: host, Port: 8080, URLScheme: "http", RPCPort: port}, seen
}

func TestControllerTokenAuthenticatesUnaryAndStreamRPCs(t *testing.T) {
	t.Setenv("CT1_RPC_TOKEN_FILE", "")
	const token = "0123456789abcdef0123456789abcdef"
	fixture := &grpcFixture{asicHealth: func(*mdk.StreamASICHealthRequest, mdk.TelemetryService_StreamASICHealthServer) error { return nil }}
	info, seen := newTokenGatedFixture(t, token, fixture)
	c, err := NewClient(info, sdk.UsernamePassword{Password: token})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	_, err = c.system.GetSystemInfo(t.Context(), &mdk.GetSystemInfoRequest{})
	require.NoError(t, err)
	stream, err := c.telemetry.StreamASICHealth(t.Context(), &mdk.StreamASICHealthRequest{})
	require.NoError(t, err)
	_, err = stream.Recv()
	require.ErrorIs(t, err, io.EOF, "the stream must pass the server's bearer check")
	for range 2 {
		require.Equal(t, []string{"Bearer " + token}, <-seen)
	}
}

func TestSavedRESTLoginIsNotSentAsControllerToken(t *testing.T) {
	t.Setenv("CT1_RPC_TOKEN_FILE", "")
	info, seen := newTokenGatedFixture(t, "rest-password", &grpcFixture{})
	c, err := NewClient(info, sdk.UsernamePassword{Username: "admin", Password: "rest-password"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	_, err = c.system.GetSystemInfo(t.Context(), &mdk.GetSystemInfoRequest{})
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	require.Empty(t, <-seen, "a credential with a username is a REST login, never a bearer token")
}

func TestMalformedControllerTokenIsRejectedWithoutEcho(t *testing.T) {
	t.Setenv("CT1_RPC_TOKEN_FILE", "")
	info := sdk.DeviceInfo{Host: "127.0.0.1", Port: 8080, URLScheme: "http", RPCPort: DefaultRPCPort}
	for name, token := range map[string]string{
		"whitespace":     "secret value",
		"header split":   "secret\r\nx-injected: 1",
		"only padding":   "==",
		"inner padding":  "ab=cd",
		"too long":       strings.Repeat("a", maxControllerTokenBytes+1),
		"non ascii text": "sécret",
	} {
		t.Run(name, func(t *testing.T) {
			c, err := NewClient(info, sdk.UsernamePassword{Password: token})
			require.Nil(t, c)
			require.ErrorContains(t, err, "CT1 controller token must be one bearer token")
			require.NotContains(t, err.Error(), token)
		})
	}
	c, err := NewClient(info, sdk.UsernamePassword{Password: "abc-._~+/DEF=="})
	require.NoError(t, err, "RFC 6750 b64token syntax with trailing padding is valid")
	require.NoError(t, c.Close())
}

func TestRejectedControllerTokenIsAuthenticationFailure(t *testing.T) {
	t.Setenv("CT1_RPC_TOKEN_FILE", "")
	info, _ := newTokenGatedFixture(t, "current-token", &grpcFixture{})
	for name, credentials := range map[string]sdk.UsernamePassword{
		"rotated token": {Password: "previous-token"},
		"no token":      {},
	} {
		t.Run(name, func(t *testing.T) {
			d, err := NewDevice(t.Context(), "container-1", info, credentials)
			require.Nil(t, d)
			var sdkErr sdk.SDKError
			require.ErrorAs(t, err, &sdkErr)
			require.Equal(t, sdk.ErrCodeAuthenticationFailed, sdkErr.Code, "a rejected credential is an authentication problem, not offline")
			require.Equal(t, codes.Unauthenticated, status.Code(err), "callers inspecting the gRPC code still see Unauthenticated")
			require.NotContains(t, err.Error(), "previous-token")
		})
	}
}

func TestBearerCredentialNeverFormatsToken(t *testing.T) {
	credential := newBearerCredential("secret-token")
	for _, formatted := range []string{fmt.Sprint(credential), fmt.Sprintf("%v", credential), fmt.Sprintf("%+v", credential), fmt.Sprintf("%#v", credential)} {
		require.NotContains(t, formatted, "secret-token")
	}
}

func TestPollingAfterControllerTokenRotationIsAuthenticationFailure(t *testing.T) {
	t.Setenv("CT1_RPC_TOKEN_FILE", "")
	var token atomic.Pointer[string]
	first, second := "first-token", "second-token"
	token.Store(&first)
	info, _ := newRotatingTokenFixture(t, func() string { return *token.Load() }, newGRPCFixture(t, nil))
	d, err := NewDevice(t.Context(), "container-1", info, sdk.UsernamePassword{Password: "first-token"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close(t.Context()) })

	token.Store(&second)
	_, err = d.Status(t.Context())
	var sdkErr sdk.SDKError
	require.ErrorAs(t, err, &sdkErr)
	require.Equal(t, sdk.ErrCodeAuthenticationFailed, sdkErr.Code)
	require.NotContains(t, err.Error(), "first-token")
}
