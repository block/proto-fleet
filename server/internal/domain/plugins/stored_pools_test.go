package plugins

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"

	"connectrpc.com/connect"
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
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	require.Equal(t, connect.CodeInternal, fleetErr.GRPCCode)
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

func TestPrivateStoredPoolAdapterFailuresThroughGRPC(t *testing.T) {
	const firmwareEcho = "firmware echoed a private configuration value"
	for _, tc := range []struct {
		name    string
		failure error
		code    connect.Code
	}{
		{name: "legacy device", code: connect.CodeUnimplemented},
		{name: "typed SDK unsupported", failure: sdk.NewErrUnsupportedCapability(firmwareEcho), code: connect.CodeUnimplemented},
		{name: "gRPC unsupported", failure: status.Error(codes.Unimplemented, firmwareEcho), code: connect.CodeUnimplemented},
		{name: "SDK transient", failure: sdk.NewErrorDeviceUnavailable(firmwareEcho), code: connect.CodeUnavailable},
		{name: "SDK authentication rejected", failure: sdk.NewErrorAuthenticationFailed(firmwareEcho), code: connect.CodeUnauthenticated},
		{name: "gRPC authentication rejected", failure: status.Error(codes.Unauthenticated, firmwareEcho), code: connect.CodeUnauthenticated},
		{name: "gRPC unavailable", failure: status.Error(codes.Unavailable, firmwareEcho), code: connect.CodeUnavailable},
		{name: "gRPC deadline", failure: status.Error(codes.DeadlineExceeded, firmwareEcho), code: connect.CodeDeadlineExceeded},
		{name: "confirmed incomplete storage", failure: sdk.NewErrorInvalidConfig(firmwareEcho), code: connect.CodeFailedPrecondition},
		{name: "gRPC failed precondition", failure: status.Error(codes.FailedPrecondition, firmwareEcho), code: connect.CodeFailedPrecondition},
	} {
		t.Run(tc.name, func(t *testing.T) {
			miner, device := createTestPluginMiner()
			device.getPoolsFunc = func(context.Context) ([]sdk.ConfiguredPool, error) {
				t.Error("runtime telemetry must not substitute for unsupported stored read")
				return nil, nil
			}
			var remote sdk.Device = device
			if tc.failure != nil {
				remote = &storedReadSDKDevice{mockSDKDevice: device, read: func(context.Context) ([]sdk.StoredMiningPool, error) { return nil, tc.failure }}
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
			var fleetErr fleeterror.FleetError
			require.ErrorAs(t, err, &fleetErr)
			require.Equal(t, tc.code, fleetErr.GRPCCode)
			require.NotContains(t, fmt.Sprintf("%v %+v %#v", err, err, err), firmwareEcho)
		})
	}
}

func TestPrivateStoredPoolAdapterPreservesSanitizedReadFailures(t *testing.T) {
	const privateValue = "private-stored-pool-password"
	for _, tc := range []struct {
		name    string
		failure error
		code    connect.Code
	}{
		{"plugin disconnect", status.Error(codes.Unavailable, privateValue), connect.CodeUnavailable},
		{"read deadline", status.Error(codes.DeadlineExceeded, privateValue), connect.CodeDeadlineExceeded},
		{"SDK device unavailable", sdk.NewErrorDeviceUnavailable(privateValue), connect.CodeUnavailable},
		{"SDK transient", sdk.NewErrCurtailTransient(privateValue, errors.New(privateValue)), connect.CodeUnavailable},
		{"SDK shutdown", sdk.NewErrorDriverShutdown(errors.New(privateValue)), connect.CodeAborted},
		{"unclassified read failure", errors.New(privateValue), connect.CodeInternal},
		{"Fleet transient", fleeterror.NewUnavailableErrorf("%s", privateValue), connect.CodeUnavailable},
		{"confirmed unavailable configuration", status.Error(codes.FailedPrecondition, privateValue), connect.CodeFailedPrecondition},
		{"SDK invalid configuration", sdk.NewErrorInvalidConfig(privateValue), connect.CodeFailedPrecondition},
		{"authentication rejected", status.Error(codes.Unauthenticated, privateValue), connect.CodeUnauthenticated},
		{"SDK authentication rejected", sdk.NewErrorAuthenticationFailed(privateValue), connect.CodeUnauthenticated},
		{"permission denied", status.Error(codes.PermissionDenied, privateValue), connect.CodePermissionDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			miner, device := createTestPluginMiner()
			miner.sdkDevice = &storedReadSDKDevice{mockSDKDevice: device, read: func(context.Context) ([]sdk.StoredMiningPool, error) { return nil, tc.failure }}
			pools, err := miner.GetStoredMiningPools(t.Context())
			require.Nil(t, pools)
			var fleetErr fleeterror.FleetError
			require.ErrorAs(t, err, &fleetErr)
			require.Equal(t, tc.code, fleetErr.GRPCCode)
			require.NotContains(t, fmt.Sprintf("%v %+v %#v", err, err, err), privateValue)
			require.Nil(t, errors.Unwrap(err), "private driver errors must not remain reachable")
		})
	}
}
