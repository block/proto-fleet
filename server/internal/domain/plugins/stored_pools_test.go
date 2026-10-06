package plugins

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type storedReadSDKDevice struct {
	*mockSDKDevice
	read func(context.Context) ([]sdk.StoredMiningPool, error)
}

func (d *storedReadSDKDevice) GetStoredMiningPools(ctx context.Context) ([]sdk.StoredMiningPool, error) {
	return d.read(ctx)
}

func TestPrivateStoredPoolAdapterUsesOnlyActualStoredContract(t *testing.T) {
	miner, device := createTestPluginMiner()
	device.getPoolsFunc = func(context.Context) ([]sdk.ConfiguredPool, error) {
		t.Fatal("runtime telemetry must not substitute for stored configuration")
		return nil, nil
	}
	_, err := miner.GetStoredMiningPools(t.Context())
	require.True(t, fleeterror.IsUnimplementedError(err))
	password, empty := "private-device-password", ""
	actual := []sdk.StoredMiningPool{{Priority: 0, URL: "stratum+tcp://pool.example:3333", Username: "worker", Password: &password}, {Priority: 1, Password: &empty}, {Priority: 2}}
	reader := &storedReadSDKDevice{mockSDKDevice: device, read: func(context.Context) ([]sdk.StoredMiningPool, error) { return actual, nil }}
	miner.sdkDevice = reader
	pools, err := miner.GetStoredMiningPools(t.Context())
	require.NoError(t, err)
	require.Len(t, pools, 3)
	require.Equal(t, password, *pools[0].Password)
	require.NotNil(t, pools[1].Password)
	require.Empty(t, *pools[1].Password)
	require.Nil(t, pools[2].Password)
	reader.read = func(context.Context) ([]sdk.StoredMiningPool, error) {
		return nil, errors.New("firmware echoed " + password)
	}
	_, err = miner.GetStoredMiningPools(t.Context())
	require.True(t, fleeterror.IsFailedPreconditionError(err))
	require.NotContains(t, fmt.Sprintf("%v %+v %#v", err, err, err), password)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = miner.GetStoredMiningPools(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func TestPrivateStoredPoolAdapterPreservesUnsupportedClassification(t *testing.T) {
	const firmwareEcho = "firmware echoed a private configuration value"
	for _, unsupported := range []error{sdk.NewErrUnsupportedCapability(firmwareEcho), status.Error(codes.Unimplemented, firmwareEcho)} {
		t.Run(fmt.Sprintf("%T", unsupported), func(t *testing.T) {
			miner, device := createTestPluginMiner()
			device.getPoolsFunc = func(context.Context) ([]sdk.ConfiguredPool, error) {
				t.Fatal("runtime telemetry must not substitute for unsupported stored read")
				return nil, nil
			}
			miner.sdkDevice = &storedReadSDKDevice{mockSDKDevice: device, read: func(context.Context) ([]sdk.StoredMiningPool, error) { return nil, unsupported }}
			_, err := miner.GetStoredMiningPools(t.Context())
			require.True(t, fleeterror.IsUnimplementedError(err))
			require.NotContains(t, fmt.Sprintf("%v %+v %#v", err, err, err), firmwareEcho)
		})
	}
}

type storedReadTransportDriver struct {
	sdk.Driver
	device sdk.Device
}

func (d storedReadTransportDriver) NewDevice(context.Context, string, sdk.DeviceInfo, sdk.SecretBundle) (sdk.NewDeviceResult, error) {
	return sdk.NewDeviceResult{Device: d.device}, nil
}

func TestPrivateStoredPoolAdapterUnsupportedThroughGRPC(t *testing.T) {
	const firmwareEcho = "firmware echoed a private configuration value"
	for _, tc := range []struct {
		name        string
		unsupported error
	}{
		{name: "legacy device"},
		{name: "typed SDK unsupported", unsupported: sdk.NewErrUnsupportedCapability(firmwareEcho)},
		{name: "gRPC unsupported", unsupported: status.Error(codes.Unimplemented, firmwareEcho)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			miner, device := createTestPluginMiner()
			device.getPoolsFunc = func(context.Context) ([]sdk.ConfiguredPool, error) {
				t.Error("runtime telemetry must not substitute for unsupported stored read")
				return nil, nil
			}
			var remote sdk.Device = device
			if tc.unsupported != nil {
				remote = &storedReadSDKDevice{mockSDKDevice: device, read: func(context.Context) ([]sdk.StoredMiningPool, error) { return nil, tc.unsupported }}
			}
			bridge := &sdk.DriverPlugin{Impl: storedReadTransportDriver{device: remote}}
			listener := bufconn.Listen(1024 * 1024)
			server := grpc.NewServer()
			require.NoError(t, bridge.GRPCServer(nil, server))
			done := make(chan error, 1)
			go func() { done <- server.Serve(listener) }()
			t.Cleanup(func() { server.Stop(); require.NoError(t, listener.Close()); require.NoError(t, <-done) })
			conn, err := grpc.NewClient("passthrough:///private-stored-adapter-test", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, conn.Close()) })
			proxy, err := bridge.GRPCClient(t.Context(), nil, conn)
			require.NoError(t, err)
			driver, ok := proxy.(sdk.Driver)
			require.True(t, ok)
			created, err := driver.NewDevice(t.Context(), device.ID(), sdk.DeviceInfo{}, sdk.SecretBundle{})
			require.NoError(t, err)
			miner.sdkDevice = created.Device
			_, err = miner.GetStoredMiningPools(t.Context())
			require.True(t, fleeterror.IsUnimplementedError(err))
			require.NotContains(t, fmt.Sprintf("%v %+v %#v", err, err, err), firmwareEcho)
		})
	}
}
