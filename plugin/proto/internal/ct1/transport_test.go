package ct1

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestTransportPortAndValidation(t *testing.T) {
	info := sdk.DeviceInfo{Host: "controller.example", Port: 8181, URLScheme: "https", RPCPort: 9443}
	c, err := NewClient(info, sdk.UsernamePassword{})
	require.NoError(t, err)
	defer c.Close()
	require.Regexp(t, `^(dns|passthrough):///controller\.example:9443$`, c.conn.CanonicalTarget())
	require.Equal(t, info.Host, c.endpoint.Host)
	require.Equal(t, info.Port, c.endpoint.Port)
	require.Equal(t, info.URLScheme, c.endpoint.URLScheme)
	for _, port := range []int32{0, -1, 65536} {
		info.RPCPort = port
		_, err = NewClient(info, sdk.UsernamePassword{})
		require.ErrorContains(t, err, "invalid CT1 RPC port")
	}
}

func TestTLSRejectsUntrustedCertificate(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("untrusted TLS peer must not receive an application request")
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	host, portText, err := net.SplitHostPort(server.Listener.Addr().String())
	require.NoError(t, err)
	port, err := sdk.ParsePort(portText)
	require.NoError(t, err)
	info := sdk.DeviceInfo{Host: host, Port: 8080, URLScheme: "http", RPCPort: port, RPCTLS: true}
	c, err := NewClient(info, sdk.UsernamePassword{})
	require.NoError(t, err)
	defer c.Close()
	_, err = c.Discover(t.Context(), sdk.DeviceInfo{})
	require.Equal(t, codes.Unavailable, status.Code(err))
	require.Contains(t, err.Error(), "certificate")
	require.False(t, isControllerConnectionFailure(err))
	_, err = NewDevice(t.Context(), "ct1", info, sdk.UsernamePassword{})
	require.Equal(t, codes.Unavailable, status.Code(err))
	require.False(t, sdk.DeviceProbeFailed(err))
}

func TestConnectionRecoversAfterControllerRestart(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	start := func(listener net.Listener) *grpc.Server {
		server := grpc.NewServer()
		mdk.RegisterSystemServiceServer(server, &grpcFixture{})
		go func() { _ = server.Serve(listener) }()
		t.Cleanup(server.Stop)
		return server
	}
	server := start(listener)
	host, portText, err := net.SplitHostPort(address)
	require.NoError(t, err)
	port, err := sdk.ParsePort(portText)
	require.NoError(t, err)
	info := sdk.DeviceInfo{Host: host, Port: 8080, URLScheme: "http", RPCPort: port, SerialNumber: "ct1"}
	c, err := NewClient(info, sdk.UsernamePassword{})
	require.NoError(t, err)
	defer c.Close()
	_, err = c.Discover(t.Context(), info)
	require.NoError(t, err)
	server.Stop()
	listener, err = net.Listen("tcp", address)
	require.NoError(t, err)
	start(listener)
	require.Eventually(t, func() bool {
		ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
		defer cancel()
		_, err := c.Discover(ctx, info)
		return err == nil
	}, 5*time.Second, 50*time.Millisecond)
}
