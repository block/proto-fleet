package ct1

import (
	"context"
	"net"
	"testing"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type reachabilityDriver struct{ sdk.Driver }

func (reachabilityDriver) NewDevice(ctx context.Context, id string, info sdk.DeviceInfo, _ sdk.SecretBundle) (sdk.NewDeviceResult, error) {
	device, err := NewDevice(ctx, id, info, sdk.UsernamePassword{})
	if err != nil {
		return sdk.NewDeviceResult{}, err
	}
	return sdk.NewDeviceResult{Device: device}, nil
}

func TestCT1ReachabilityAcrossPluginRPC(t *testing.T) {
	for _, tt := range []struct {
		name          string
		stopA         bool
		stopB         bool
		standalone    bool
		stopPlugin    bool
		wantConfirmed bool
	}{
		{name: "both controller connections refused", stopA: true, stopB: true, wantConfirmed: true},
		{name: "standalone controller connection refused on creation", stopA: true, standalone: true, wantConfirmed: true},
		{name: "controller responded unavailable", stopA: true},
		{name: "plugin transport unavailable", stopPlugin: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a, b := startHAServer(t, "a", "b"), startHAServer(t, "b", "a")
			b.haError = status.Error(codes.Unavailable, "controller responded but cannot serve the request")
			info := haInfo(a, b)
			if tt.standalone {
				info.Endpoints = nil
			}
			if tt.stopA {
				a.server.Stop()
			}
			if tt.stopB {
				b.server.Stop()
			}

			listener := bufconn.Listen(1 << 20)
			server := grpc.NewServer()
			plugin := &sdk.DriverPlugin{Impl: reachabilityDriver{}}
			require.NoError(t, plugin.GRPCServer(nil, server))
			go func() { _ = server.Serve(listener) }()
			t.Cleanup(server.Stop)
			conn, err := grpc.NewClient("passthrough:///ct1-plugin", grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, conn.Close()) })
			client, err := plugin.GRPCClient(t.Context(), nil, conn)
			require.NoError(t, err)
			driver, ok := client.(sdk.Driver)
			require.True(t, ok)
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			result, err := driver.NewDevice(ctx, "container", info, sdk.SecretBundle{})
			if !tt.standalone {
				require.NoError(t, err)
				defer result.Device.Close(t.Context())
				if tt.stopPlugin {
					server.Stop()
				}
				_, err = result.Device.Status(ctx)
			}
			require.Error(t, err)
			require.Equal(t, tt.wantConfirmed, sdk.DeviceProbeFailed(err), "%v", err)
		})
	}
}
