package device

import (
	"context"
	"fmt"
	"net"
	"sync/atomic"
	"testing"

	"github.com/block/proto-fleet/plugin/antminer/pkg/antminer/mocks"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type snapshotLifecycleDriver struct {
	sdk.Driver
	create func(context.Context, string, sdk.DeviceInfo, sdk.SecretBundle) (sdk.NewDeviceResult, error)
}

func (d snapshotLifecycleDriver) Handshake(context.Context) (sdk.DriverIdentifier, error) {
	return sdk.DriverIdentifier{DriverName: "antminer", APIVersion: "v1"}, nil
}
func (d snapshotLifecycleDriver) DescribeDriver(ctx context.Context) (sdk.DriverIdentifier, sdk.Capabilities, error) {
	id, err := d.Handshake(ctx)
	return id, sdk.Capabilities{}, err
}
func (d snapshotLifecycleDriver) NewDevice(ctx context.Context, id string, info sdk.DeviceInfo, secret sdk.SecretBundle) (sdk.NewDeviceResult, error) {
	return d.create(ctx, id, info, secret)
}

func TestSDKResolutionPreservesActualAntminerFullRestoreSnapshot(t *testing.T) {
	for _, initiallyStopped := range []bool{true, false} {
		t.Run(map[bool]string{true: "already stopped", false: "mining"}[initiallyStopped], func(t *testing.T) {
			ctrl := gomock.NewController(t)
			client := mocks.NewMockAntminerClient(ctrl)
			status := defaultStatus()
			if initiallyStopped {
				status.State = sdk.HealthHealthyInactive
			}
			setupMockForDeviceCreation(client)
			setupMockForDeviceConnection(client, status, defaultTelemetry())
			client.EXPECT().GetStatus(gomock.Any()).Return(status, nil)
			client.EXPECT().GetTelemetry(gomock.Any()).Return(defaultTelemetry(), nil)
			client.EXPECT().StopMining(gomock.Any()).Return(nil)
			if !initiallyStopped {
				client.EXPECT().StartMining(gomock.Any()).Return(nil)
			}
			client.EXPECT().Close()
			var creates atomic.Int64
			driver := snapshotLifecycleDriver{create: func(ctx context.Context, id string, info sdk.DeviceInfo, secret sdk.SecretBundle) (sdk.NewDeviceResult, error) {
				creates.Add(1)
				credentials, ok := secret.Kind.(sdk.UsernamePassword)
				if !ok {
					return sdk.NewDeviceResult{}, fmt.Errorf("lifecycle fixture requires username/password secret")
				}
				device, err := New(id, info, credentials, mockClientFactory(client))
				if err != nil {
					return sdk.NewDeviceResult{}, err
				}
				err = device.Connect(ctx)
				return sdk.NewDeviceResult{Device: device}, err
			}}
			listener := bufconn.Listen(1024 * 1024)
			server := grpc.NewServer()
			plugin := &sdk.DriverPlugin{Impl: driver}
			require.NoError(t, plugin.GRPCServer(nil, server))
			go func() { _ = server.Serve(listener) }()
			t.Cleanup(server.Stop)
			connection, err := grpc.NewClient("passthrough:///antminer-lifecycle", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
			require.NoError(t, err)
			t.Cleanup(func() { _ = connection.Close() })
			raw, err := plugin.GRPCClient(t.Context(), nil, connection)
			require.NoError(t, err)
			resolver, ok := raw.(sdk.ScopedDeviceResolver)
			require.True(t, ok, "SDK plugin client must implement scoped resolution")
			resolve := func() sdk.Device {
				result, err := resolver.GetOrCreateDevice(t.Context(), testDeviceID, testDeviceInfo(), sdk.SecretBundle{Version: "v1", Kind: testCredentials()}, "same-incarnation-org-epoch")
				require.NoError(t, err)
				return result.Device
			}
			first := resolve()
			require.NoError(t, antminerLifecycleCurtailer(t, first).Curtail(t.Context(), sdk.CurtailRequest{Level: sdk.CurtailLevelFull}))
			second := resolve()
			require.NotSame(t, first, second, "fresh RPC handles must address the retained actual backend")
			require.NoError(t, antminerLifecycleCurtailer(t, second).Uncurtail(t.Context(), sdk.UncurtailRequest{}))
			require.Equal(t, int64(1), creates.Load())
			require.NoError(t, second.Close(t.Context()))
		})
	}
}

func antminerLifecycleCurtailer(t *testing.T, device sdk.Device) sdk.DeviceCurtailment {
	t.Helper()
	curtailer, ok := device.(sdk.DeviceCurtailment)
	require.True(t, ok, "actual Antminer SDK handle must implement curtailment")
	return curtailer
}
