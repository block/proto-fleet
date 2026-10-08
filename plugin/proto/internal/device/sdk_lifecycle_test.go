package device

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type lifecycleDriver struct {
	sdk.Driver
	create func(context.Context, string, sdk.DeviceInfo, sdk.SecretBundle) (sdk.NewDeviceResult, error)
}

func (d lifecycleDriver) Handshake(context.Context) (sdk.DriverIdentifier, error) {
	return sdk.DriverIdentifier{DriverName: "proto", APIVersion: "v1"}, nil
}
func (d lifecycleDriver) DescribeDriver(context.Context) (sdk.DriverIdentifier, sdk.Capabilities, error) {
	return sdk.DriverIdentifier{DriverName: "proto", APIVersion: "v1"}, sdk.Capabilities{}, nil
}
func (d lifecycleDriver) NewDevice(ctx context.Context, id string, info sdk.DeviceInfo, secret sdk.SecretBundle) (sdk.NewDeviceResult, error) {
	return d.create(ctx, id, info, secret)
}

type scopedLifecycleDriver interface {
	GetOrCreateDevice(context.Context, string, sdk.DeviceInfo, sdk.SecretBundle, string) (sdk.NewDeviceResult, error)
}

func connectLifecycleDriver(t *testing.T, driver sdk.Driver) sdk.Driver {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	plugin := &sdk.DriverPlugin{Impl: driver}
	require.NoError(t, plugin.GRPCServer(nil, server))
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///device-lifecycle", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	client, err := plugin.GRPCClient(t.Context(), nil, conn)
	require.NoError(t, err)
	return client.(sdk.Driver)
}

func resolveLifecycleDevice(t *testing.T, client sdk.Driver, info sdk.DeviceInfo) sdk.Device {
	t.Helper()
	secret := sdk.SecretBundle{Version: "v1", Kind: sdk.UsernamePassword{Username: "admin", Password: "proto"}}
	var result sdk.NewDeviceResult
	var err error
	if resolver, ok := client.(scopedLifecycleDriver); ok {
		result, err = resolver.GetOrCreateDevice(t.Context(), "device-curtail", info, secret, "trusted-incarnation-owner-epoch")
	} else {
		// RED baseline follows the old factory's unconditional SDK NewDevice path.
		// This test seam is not a production compatibility fallback.
		result, err = client.NewDevice(t.Context(), "device-curtail", info, secret)
	}
	require.NoError(t, err)
	return result.Device
}

func TestSDKResolutionPreservesRealProtoFullStoppedSnapshot(t *testing.T) {
	var starts atomic.Int64
	fixture := newMiningControlTestDeviceWithMiningState(t, 200, "Stopped", func(path string) {
		if path == "/api/v1/mining/start" {
			starts.Add(1)
		}
	})
	var creates atomic.Int64
	client := connectLifecycleDriver(t, lifecycleDriver{create: func(ctx context.Context, id string, info sdk.DeviceInfo, secret sdk.SecretBundle) (sdk.NewDeviceResult, error) {
		creates.Add(1)
		dev, err := New(ctx, id, info, secret.Kind.(sdk.UsernamePassword), SetStatusTTL(0))
		return sdk.NewDeviceResult{Device: dev}, err
	}})
	first := resolveLifecycleDevice(t, client, fixture.deviceInfo).(sdk.DeviceCurtailment)
	require.NoError(t, first.Curtail(t.Context(), sdk.CurtailRequest{Level: sdk.CurtailLevelFull}))
	second := resolveLifecycleDevice(t, client, fixture.deviceInfo).(sdk.DeviceCurtailment)
	require.NoError(t, second.Uncurtail(t.Context(), sdk.UncurtailRequest{}))
	t.Logf("actual SDK backend creations=%d, physical start requests=%d", creates.Load(), starts.Load())
	require.Zero(t, starts.Load(), "fresh facades must not discard the real SDK backend's stopped snapshot")
	require.Equal(t, int64(1), creates.Load())
}

func TestSDKResolutionPreservesRealProtoEfficiencySnapshot(t *testing.T) {
	var requests []powerTargetRequest
	fixture := newFullEfficiencyCurtailmentTestDevice(t, "Mining", targetResponse{PowerTargetWatts: 2800, PowerTargetMinWatts: 1200, PowerTargetMaxWatts: 3200, DefaultPowerTargetWatts: 1800, PerformanceMode: "MaximumHashrate"}, &requests, nil)
	var creates atomic.Int64
	client := connectLifecycleDriver(t, lifecycleDriver{create: func(ctx context.Context, id string, info sdk.DeviceInfo, secret sdk.SecretBundle) (sdk.NewDeviceResult, error) {
		creates.Add(1)
		dev, err := New(ctx, id, info, secret.Kind.(sdk.UsernamePassword), SetStatusTTL(0))
		return sdk.NewDeviceResult{Device: dev}, err
	}})
	first := resolveLifecycleDevice(t, client, fixture.deviceInfo).(sdk.DeviceCurtailment)
	require.NoError(t, first.Curtail(t.Context(), sdk.CurtailRequest{Level: sdk.CurtailLevelEfficiency}))
	second := resolveLifecycleDevice(t, client, fixture.deviceInfo).(sdk.DeviceCurtailment)
	require.NoError(t, second.Uncurtail(t.Context(), sdk.UncurtailRequest{}))
	t.Logf("actual SDK backend creations=%d, power target requests=%d", creates.Load(), len(requests))
	require.Len(t, requests, 2, "real SDK instance must retain the pre-efficiency power target")
	require.Equal(t, 2800, *requests[1].PowerTargetWatts)
	require.Equal(t, "MaximumHashrate", requests[1].PerformanceMode)
	require.Equal(t, int64(1), creates.Load())
}
