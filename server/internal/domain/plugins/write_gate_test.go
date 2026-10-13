package plugins

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	sdkMocks "github.com/block/proto-fleet/server/sdk/v1/mocks"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type gatedSDKDevice struct {
	*mockSDKDevice
	writes  int
	onWrite func(context.Context, commandtype.Type) error
}

func (d *gatedSDKDevice) write(ctx context.Context, kind commandtype.Type) error {
	d.writes++
	if d.onWrite != nil {
		return d.onWrite(ctx, kind)
	}
	return nil
}

func (d *gatedSDKDevice) ApplyCurtailmentConfig(ctx context.Context, _ sdk.CurtailmentConfig) error {
	return d.write(ctx, commandtype.ApplyCurtailmentConfig)
}
func (d *gatedSDKDevice) Unpair(ctx context.Context) error {
	return d.write(ctx, commandtype.Unpair)
}
func (d *gatedSDKDevice) SetContainerControl(ctx context.Context, _ sdk.ContainerControlRequest) error {
	return d.write(ctx, commandtype.SetContainerControl)
}
func (d *gatedSDKDevice) SetTankMining(ctx context.Context, _ sdk.TankMiningRequest) error {
	return d.write(ctx, commandtype.SetTankMining)
}
func (d *gatedSDKDevice) SetModuleMining(ctx context.Context, _ sdk.ModuleMiningRequest) error {
	return d.write(ctx, commandtype.SetModuleMining)
}

func createGatedTestPluginMiner() (*PluginMiner, *gatedSDKDevice) {
	_, base := createTestPluginMiner()
	device := &gatedSDKDevice{mockSDKDevice: base}
	miner := createTestPluginMinerWithDevice(device)
	miner.caps = sdk.Capabilities{sdk.CapabilityCurtailFull: true, sdk.CapabilityTankMining: true, sdk.CapabilityModuleMining: true}
	device.rebootFunc = func(ctx context.Context) error { return device.write(ctx, commandtype.Reboot) }
	device.startMiningFunc = func(ctx context.Context) error { return device.write(ctx, commandtype.StartMining) }
	device.stopMiningFunc = func(ctx context.Context) error { return device.write(ctx, commandtype.StopMining) }
	device.setCoolingModeFunc = func(ctx context.Context, _ sdk.CoolingMode) error {
		return device.write(ctx, commandtype.SetCoolingMode)
	}
	device.setPowerTargetFunc = func(ctx context.Context, _ sdk.PerformanceMode) error {
		return device.write(ctx, commandtype.SetPowerTarget)
	}
	device.updatePoolsFunc = func(ctx context.Context, _ []sdk.MiningPoolConfig) error {
		return device.write(ctx, commandtype.UpdateMiningPools)
	}
	device.firmwareUpdateFunc = func(ctx context.Context, _ sdk.FirmwareFile) error {
		return device.write(ctx, commandtype.FirmwareUpdate)
	}
	device.updateMinerPasswordFunc = func(ctx context.Context, _, _ string) error {
		return device.write(ctx, commandtype.UpdateMinerPassword)
	}
	device.curtailFunc = func(ctx context.Context, _ sdk.CurtailRequest) error { return device.write(ctx, commandtype.Curtail) }
	device.uncurtailFunc = func(ctx context.Context, _ sdk.UncurtailRequest) error {
		return device.write(ctx, commandtype.Uncurtail)
	}
	return miner, device
}

type pluginPhysicalWriteCase struct {
	kind commandtype.Type
	run  func(context.Context) error
}

func pluginPhysicalWriteCases(miner *PluginMiner) []pluginPhysicalWriteCase {
	return []pluginPhysicalWriteCase{
		{commandtype.SetContainerControl, func(ctx context.Context) error {
			return miner.SetContainerControl(ctx, sdk.ContainerControlRequest{Name: "dry_cooler", Enabled: new(false)})
		}},
		{commandtype.SetTankMining, func(ctx context.Context) error {
			return miner.SetTankMining(ctx, sdk.TankMiningRequest{Positions: []int32{1}, Mining: new(false)})
		}},
		{commandtype.SetModuleMining, func(ctx context.Context) error {
			return miner.SetModuleMining(ctx, sdk.ModuleMiningRequest{Modules: []sdk.ModuleMiningTarget{{Tank: 1, Module: 1, ReportedID: "module-a"}}, Mining: new(false)})
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
	}
}

func TestPluginPhysicalWritesRequireAdmissionAtDispatch(t *testing.T) {
	denied := errors.New("operation admission denied")
	miner, device := createGatedTestPluginMiner()
	for _, tc := range pluginPhysicalWriteCases(miner) {
		t.Run(tc.kind.String(), func(t *testing.T) {
			gateCalls := 0
			miner.writeGate = func(_ context.Context, kind commandtype.Type, _ func(context.Context) error) error {
				gateCalls++
				require.Equal(t, tc.kind, kind)
				return denied
			}
			err := tc.run(t.Context())
			require.ErrorIs(t, err, denied)
			require.True(t, devicewrite.IsNotDispatched(err), "gate refusal is known to precede the SDK call")
			require.Equal(t, 1, gateCalls)
			require.Zero(t, device.writes)
		})
	}
	miner.writeGate = func(context.Context, commandtype.Type, func(context.Context) error) error {
		t.Fatal("reads and indicator control must bypass physical-write admission")
		return nil
	}
	require.NoError(t, miner.BlinkLED(t.Context()), "indicator control does not change payout state")
	_, err := miner.GetMiningPools(t.Context())
	require.NoError(t, err)
}

func TestPluginPhysicalWritesSpanAdmissionAndForwardContext(t *testing.T) {
	type admissionKey struct{}
	miner, device := createGatedTestPluginMiner()
	for _, tc := range pluginPhysicalWriteCases(miner) {
		t.Run(tc.kind.String(), func(t *testing.T) {
			active := false
			gateCalls := 0
			device.writes = 0
			device.onWrite = func(ctx context.Context, kind commandtype.Type) error {
				require.True(t, active, "admission must remain held until the SDK call returns")
				require.Equal(t, tc.kind, kind, "expected SDK mutation must perform the admitted write")
				require.Equal(t, "admitted", ctx.Value(admissionKey{}))
				return nil
			}
			miner.writeGate = func(ctx context.Context, kind commandtype.Type, call func(context.Context) error) error {
				gateCalls++
				require.Equal(t, tc.kind, kind)
				require.Zero(t, device.writes, "SDK write must wait for admission")
				active = true
				defer func() { active = false }()
				return call(context.WithValue(ctx, admissionKey{}, "admitted"))
			}
			require.NoError(t, tc.run(t.Context()))
			require.Equal(t, 1, gateCalls)
			require.Equal(t, 1, device.writes)
			require.False(t, active)
		})
	}
}

func TestPluginWritePreparationFailsBeforeAdmission(t *testing.T) {
	for _, tc := range []struct {
		name      string
		configure func(*PluginMiner)
		run       func(context.Context, *PluginMiner) error
	}{
		{"missing curtail capability", func(miner *PluginMiner) { delete(miner.caps, sdk.CapabilityCurtailFull) }, func(ctx context.Context, miner *PluginMiner) error {
			return miner.Curtail(ctx, sdk.CurtailRequest{Level: sdk.CurtailLevelFull})
		}},
		{"missing tank capability", func(miner *PluginMiner) { delete(miner.caps, sdk.CapabilityTankMining) }, func(ctx context.Context, miner *PluginMiner) error {
			return miner.SetTankMining(ctx, sdk.TankMiningRequest{Positions: []int32{1}, Mining: new(false)})
		}},
		{"nil curtailment config", nil, func(ctx context.Context, miner *PluginMiner) error {
			return miner.ApplyCurtailmentConfig(ctx, dto.ApplyCurtailmentConfigPayload{})
		}},
		{"invalid pool priority", nil, func(ctx context.Context, miner *PluginMiner) error {
			return miner.UpdateMiningPools(ctx, dto.UpdateMiningPoolsPayload{DefaultPool: dto.MiningPool{Priority: math.MaxInt32 + 1}})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			miner, device := createGatedTestPluginMiner()
			if tc.configure != nil {
				tc.configure(miner)
			}
			miner.writeGate = func(context.Context, commandtype.Type, func(context.Context) error) error {
				t.Fatal("preparation failure must not enter admission")
				return nil
			}
			require.Error(t, tc.run(t.Context(), miner))
			require.Zero(t, device.writes)
		})
	}
}

func TestPluginTankPreparationRefusalRemainsNotDispatchedThroughOuterDispatch(t *testing.T) {
	for _, tc := range []struct {
		name      string
		configure func(*PluginMiner)
		request   sdk.TankMiningRequest
	}{
		{
			name:      "missing tank capability",
			configure: func(miner *PluginMiner) { delete(miner.caps, sdk.CapabilityTankMining) },
			request:   sdk.TankMiningRequest{Positions: []int32{1}, Mining: new(false)},
		},
		{
			name:    "invalid tank request",
			request: sdk.TankMiningRequest{Mining: new(false)},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			miner, device := createGatedTestPluginMiner()
			if tc.configure != nil {
				tc.configure(miner)
			}
			miner.writeGate = func(context.Context, commandtype.Type, func(context.Context) error) error {
				t.Fatal("tank preparation refusal must not enter admission")
				return nil
			}
			err := devicewrite.Dispatch(t.Context(), func(ctx context.Context) error {
				return miner.SetTankMining(ctx, tc.request)
			})
			require.Error(t, err)
			require.True(t, devicewrite.IsNotDispatched(err))
			require.Zero(t, device.writes)
		})
	}
}

func TestPluginFactoryCarriesPhysicalWriteAdmission(t *testing.T) {
	_, device := createTestPluginMiner()
	denied := errors.New("operation admission denied")
	miner, err := NewPluginMinerWithCredentials(t.Context(), PluginMinerConfig{
		SDKReuseScopeKey: "physical-admission-binding", DeviceIdentifier: device.ID(), DriverName: "proto", DeviceIPAddress: "192.0.2.1", DevicePort: "8080", DeviceScheme: "http", FilesService: &files.Service{}, DriverGetter: containerDriverGetter{&sdkMocks.ScopedDriverFixture{Driver: storedReadTransportDriver{device: device}}},
		WriteGate: func(context.Context, commandtype.Type, func(context.Context) error) error { return denied },
	})
	require.NoError(t, err)
	require.ErrorIs(t, miner.Reboot(t.Context()), denied)
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
	err := miner.Reboot(t.Context())
	require.ErrorIs(t, err, observed)
	require.False(t, devicewrite.IsNotDispatched(err), "errors after the SDK call remain possibly dispatched")
	require.False(t, active)
}
