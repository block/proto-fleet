package plugins

import (
	"context"
	"errors"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type gatedSDKDevice struct {
	*mockSDKDevice
	writes int
}

func (d *gatedSDKDevice) ApplyCurtailmentConfig(context.Context, sdk.CurtailmentConfig) error {
	d.writes++
	return nil
}
func (d *gatedSDKDevice) Unpair(context.Context) error { d.writes++; return nil }
func (d *gatedSDKDevice) SetContainerControl(context.Context, sdk.ContainerControlRequest) error {
	d.writes++
	return nil
}

func TestPluginPhysicalWritesRequireAdmissionAtDispatch(t *testing.T) {
	denied := errors.New("operation admission denied")
	_, base := createTestPluginMiner()
	device := &gatedSDKDevice{mockSDKDevice: base}
	miner := createTestPluginMinerWithDevice(device)
	miner.caps = sdk.Capabilities{sdk.CapabilityCurtailFull: true}
	device.rebootFunc = func(context.Context) error { device.writes++; return nil }
	device.startMiningFunc = func(context.Context) error { device.writes++; return nil }
	device.stopMiningFunc = func(context.Context) error { device.writes++; return nil }
	device.setCoolingModeFunc = func(context.Context, sdk.CoolingMode) error { device.writes++; return nil }
	device.setPowerTargetFunc = func(context.Context, sdk.PerformanceMode) error { device.writes++; return nil }
	device.updatePoolsFunc = func(context.Context, []sdk.MiningPoolConfig) error { device.writes++; return nil }
	device.firmwareUpdateFunc = func(context.Context, sdk.FirmwareFile) error { device.writes++; return nil }
	device.updateMinerPasswordFunc = func(context.Context, string, string) error { device.writes++; return nil }
	device.curtailFunc = func(context.Context, sdk.CurtailRequest) error { device.writes++; return nil }
	device.uncurtailFunc = func(context.Context, sdk.UncurtailRequest) error { device.writes++; return nil }
	var admitted commandtype.Type
	miner.writeGate = func(_ context.Context, kind commandtype.Type, _ func(context.Context) error) error {
		admitted = kind
		return denied
	}
	for _, tc := range []struct {
		kind commandtype.Type
		run  func(context.Context) error
	}{
		{commandtype.SetContainerControl, func(ctx context.Context) error {
			return miner.SetContainerControl(ctx, sdk.ContainerControlRequest{Name: "dry_cooler", Enabled: new(false)})
		}},
		{commandtype.Reboot, miner.Reboot},
		{commandtype.StartMining, miner.StartMining},
		{commandtype.StopMining, miner.StopMining},
		{commandtype.SetCoolingMode, func(ctx context.Context) error { return miner.SetCoolingMode(ctx, dto.CoolingModePayload{}) }},
		{commandtype.SetPowerTarget, func(ctx context.Context) error { return miner.SetPowerTarget(ctx, dto.PowerTargetPayload{}) }},
		{commandtype.UpdateMiningPools, func(ctx context.Context) error {
			return miner.UpdateMiningPools(ctx, dto.UpdateMiningPoolsPayload{DefaultPool: dto.MiningPool{URL: "stratum+tcp://pool.example:3333", Username: "worker"}})
		}},
		{commandtype.FirmwareUpdate, func(ctx context.Context) error { return miner.FirmwareUpdate(ctx, sdk.FirmwareFile{}) }},
		{commandtype.UpdateMinerPassword, func(ctx context.Context) error {
			return miner.UpdateMinerPassword(ctx, dto.UpdateMinerPasswordPayload{})
		}},
		{commandtype.Unpair, miner.Unpair},
		{commandtype.Curtail, func(ctx context.Context) error {
			return miner.Curtail(ctx, sdk.CurtailRequest{Level: sdk.CurtailLevelFull})
		}},
		{commandtype.Uncurtail, func(ctx context.Context) error { return miner.Uncurtail(ctx, sdk.UncurtailRequest{}) }},
		{commandtype.ApplyCurtailmentConfig, func(ctx context.Context) error {
			return miner.ApplyCurtailmentConfig(ctx, dto.ApplyCurtailmentConfigPayload{Config: &sdk.CurtailmentConfig{}})
		}},
	} {
		t.Run(tc.kind.String(), func(t *testing.T) {
			require.ErrorIs(t, tc.run(t.Context()), denied)
			require.Equal(t, tc.kind, admitted)
			require.Zero(t, device.writes)
		})
	}
	require.NoError(t, miner.BlinkLED(t.Context()), "indicator control does not change payout state")
	_, err := miner.GetMiningPools(t.Context())
	require.NoError(t, err)
}

func TestPluginFactoryCarriesPhysicalWriteAdmission(t *testing.T) {
	_, device := createTestPluginMiner()
	denied := errors.New("operation admission denied")
	miner, err := NewPluginMinerWithCredentials(t.Context(), PluginMinerConfig{
		DeviceIdentifier: device.ID(), DriverName: "proto", DeviceIPAddress: "192.0.2.1", DevicePort: "8080", DeviceScheme: "http", FilesService: &files.Service{}, DriverGetter: containerDriverGetter{storedReadTransportDriver{device: device}},
		WriteGate: func(context.Context, commandtype.Type, func(context.Context) error) error { return denied },
	})
	require.NoError(t, err)
	require.ErrorIs(t, miner.Reboot(t.Context()), denied)
}

func TestPluginAdmissionSpansSDKCallAndForwardsItsContext(t *testing.T) {
	miner, device := createTestPluginMiner()
	type admissionKey struct{}
	active := false
	device.rebootFunc = func(ctx context.Context) error {
		require.True(t, active, "admission must remain held until the SDK call returns")
		require.Equal(t, "admitted", ctx.Value(admissionKey{}))
		return nil
	}
	miner.writeGate = func(ctx context.Context, kind commandtype.Type, call func(context.Context) error) error {
		require.Equal(t, commandtype.Reboot, kind)
		active = true
		defer func() { active = false }()
		return call(context.WithValue(ctx, admissionKey{}, "admitted"))
	}
	require.NoError(t, miner.Reboot(t.Context()))
	require.False(t, active)
}

func TestPluginAdmissionObservesClassifiedSDKFailure(t *testing.T) {
	miner, device := createTestPluginMiner()
	type admissionKey struct{}
	active := false
	device.rebootFunc = func(ctx context.Context) error {
		require.True(t, active)
		require.Equal(t, "admitted", ctx.Value(admissionKey{}))
		return grpcstatus.Error(codes.Unauthenticated, "device rejected credentials")
	}
	var observed error
	miner.writeGate = func(ctx context.Context, _ commandtype.Type, call func(context.Context) error) error {
		active = true
		defer func() { active = false }()
		observed = call(context.WithValue(ctx, admissionKey{}, "admitted"))
		require.True(t, fleeterror.IsAuthenticationError(observed), "callback must receive the classified SDK failure")
		return observed
	}
	require.ErrorIs(t, miner.Reboot(t.Context()), observed)
	require.False(t, active)
}
