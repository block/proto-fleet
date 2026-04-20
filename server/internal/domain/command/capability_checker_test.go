package command

import (
	"context"
	"testing"

	capabilitiespb "github.com/block/proto-fleet/server/generated/grpc/capabilities/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/assert"
)

// mockCapabilitiesProvider implements CapabilitiesProvider for testing.
type mockCapabilitiesProvider struct {
	capabilities map[string]*capabilitiespb.MinerCapabilities
}

func (m *mockCapabilitiesProvider) GetMinerCapabilitiesForDevice(_ context.Context, device *pairingpb.Device) *capabilitiespb.MinerCapabilities {
	key := device.DriverName + "|" + device.Manufacturer + "|" + device.Model
	return m.capabilities[key]
}

func TestHasAnyCapability(t *testing.T) {
	t.Run("returns true when reboot is supported", func(t *testing.T) {
		commands := &capabilitiespb.CommandCapabilities{
			RebootSupported: true,
		}

		result := hasAnyCapability(commands, nil, []string{sdk.CapabilityReboot})

		assert.True(t, result)
	})

	t.Run("returns true when mining start is supported", func(t *testing.T) {
		commands := &capabilitiespb.CommandCapabilities{
			MiningStartSupported: true,
		}

		result := hasAnyCapability(commands, nil, []string{sdk.CapabilityMiningStart})

		assert.True(t, result)
	})

	t.Run("returns true when mining stop is supported", func(t *testing.T) {
		commands := &capabilitiespb.CommandCapabilities{
			MiningStopSupported: true,
		}

		result := hasAnyCapability(commands, nil, []string{sdk.CapabilityMiningStop})

		assert.True(t, result)
	})

	t.Run("returns true when LED blink is supported", func(t *testing.T) {
		commands := &capabilitiespb.CommandCapabilities{
			LedBlinkSupported: true,
		}

		result := hasAnyCapability(commands, nil, []string{sdk.CapabilityLEDBlink})

		assert.True(t, result)
	})

	t.Run("returns true when any cooling mode is supported", func(t *testing.T) {
		commands := &capabilitiespb.CommandCapabilities{
			AirCoolingSupported: true,
		}

		result := hasAnyCapability(commands, nil, []string{sdk.CapabilityCoolingModeAir, sdk.CapabilityCoolingModeImmerse})

		assert.True(t, result)
	})

	t.Run("returns true when immersion cooling is supported", func(t *testing.T) {
		commands := &capabilitiespb.CommandCapabilities{
			ImmersionCoolingSupported: true,
		}

		result := hasAnyCapability(commands, nil, []string{sdk.CapabilityCoolingModeAir, sdk.CapabilityCoolingModeImmerse})

		assert.True(t, result)
	})

	t.Run("returns true when pool switching is supported", func(t *testing.T) {
		commands := &capabilitiespb.CommandCapabilities{
			PoolSwitchingSupported: true,
		}

		result := hasAnyCapability(commands, nil, []string{sdk.CapabilityPoolConfig})

		assert.True(t, result)
	})

	t.Run("returns true when logs download is supported", func(t *testing.T) {
		commands := &capabilitiespb.CommandCapabilities{
			LogsDownloadSupported: true,
		}

		result := hasAnyCapability(commands, nil, []string{sdk.CapabilityLogsDownload})

		assert.True(t, result)
	})

	t.Run("returns true when factory reset is supported", func(t *testing.T) {
		commands := &capabilitiespb.CommandCapabilities{
			FactoryResetSupported: true,
		}

		result := hasAnyCapability(commands, nil, []string{sdk.CapabilityFactoryReset})

		assert.True(t, result)
	})

	t.Run("returns true when power mode efficiency is supported", func(t *testing.T) {
		commands := &capabilitiespb.CommandCapabilities{
			PowerModeEfficiencySupported: true,
		}

		result := hasAnyCapability(commands, nil, []string{sdk.CapabilityPowerModeEfficiency})

		assert.True(t, result)
	})

	t.Run("returns false when power mode efficiency is not supported", func(t *testing.T) {
		commands := &capabilitiespb.CommandCapabilities{
			PowerModeEfficiencySupported: false,
		}

		result := hasAnyCapability(commands, nil, []string{sdk.CapabilityPowerModeEfficiency})

		assert.False(t, result)
	})

	t.Run("returns false when required capability is not supported", func(t *testing.T) {
		commands := &capabilitiespb.CommandCapabilities{
			RebootSupported: false,
		}

		result := hasAnyCapability(commands, nil, []string{sdk.CapabilityReboot})

		assert.False(t, result)
	})

	t.Run("returns false when no capabilities are supported", func(t *testing.T) {
		commands := &capabilitiespb.CommandCapabilities{}

		result := hasAnyCapability(commands, nil, []string{sdk.CapabilityReboot, sdk.CapabilityMiningStart})

		assert.False(t, result)
	})

	t.Run("returns true when manual upload is supported", func(t *testing.T) {
		commands := &capabilitiespb.CommandCapabilities{}
		firmware := &capabilitiespb.FirmwareCapabilities{ManualUploadSupported: true}

		result := hasAnyCapability(commands, firmware, []string{sdk.CapabilityManualUpload})

		assert.True(t, result)
	})

	t.Run("returns false when manual upload is not supported", func(t *testing.T) {
		commands := &capabilitiespb.CommandCapabilities{}
		firmware := &capabilitiespb.FirmwareCapabilities{ManualUploadSupported: false}

		result := hasAnyCapability(commands, firmware, []string{sdk.CapabilityManualUpload})

		assert.False(t, result)
	})

	t.Run("returns false for manual upload when firmware capabilities is nil", func(t *testing.T) {
		commands := &capabilitiespb.CommandCapabilities{}

		result := hasAnyCapability(commands, nil, []string{sdk.CapabilityManualUpload})

		assert.False(t, result)
	})
}

func TestCheckDeviceCapabilities(t *testing.T) {
	ctx := context.Background()

	t.Run("returns all supported when all devices support the command", func(t *testing.T) {
		provider := &mockCapabilitiesProvider{
			capabilities: map[string]*capabilitiespb.MinerCapabilities{
				"proto|Proto|Model1": {Commands: &capabilitiespb.CommandCapabilities{RebootSupported: true}},
				"proto|Proto|Model2": {Commands: &capabilitiespb.CommandCapabilities{RebootSupported: true}},
			},
		}
		checker := NewCapabilityChecker(nil, provider)

		devices := []deviceInfo{
			{DeviceIdentifier: "device-1", DriverName: "proto", Manufacturer: "Proto", Model: "Model1"},
			{DeviceIdentifier: "device-2", DriverName: "proto", Manufacturer: "Proto", Model: "Model2"},
		}

		result := checker.checkDeviceCapabilities(ctx, devices, []string{sdk.CapabilityReboot})

		assert.Equal(t, int32(2), result.SupportedCount)
		assert.Equal(t, int32(0), result.UnsupportedCount)
		assert.Equal(t, int32(2), result.TotalCount)
		assert.True(t, result.AllSupported)
		assert.False(t, result.NoneSupported)
		assert.Len(t, result.UnsupportedGroups, 0)
		assert.ElementsMatch(t, []string{"device-1", "device-2"}, result.SupportedDeviceIdentifiers)
	})

	t.Run("returns none supported when no devices support the command", func(t *testing.T) {
		provider := &mockCapabilitiesProvider{
			capabilities: map[string]*capabilitiespb.MinerCapabilities{
				"antminer|Bitmain|S19": {Commands: &capabilitiespb.CommandCapabilities{RebootSupported: false}},
			},
		}
		checker := NewCapabilityChecker(nil, provider)

		devices := []deviceInfo{
			{DeviceIdentifier: "device-1", DriverName: "antminer", Manufacturer: "Bitmain", Model: "S19", FirmwareVersion: "1.0.0"},
			{DeviceIdentifier: "device-2", DriverName: "antminer", Manufacturer: "Bitmain", Model: "S19", FirmwareVersion: "1.0.0"},
		}

		result := checker.checkDeviceCapabilities(ctx, devices, []string{sdk.CapabilityReboot})

		assert.Equal(t, int32(0), result.SupportedCount)
		assert.Equal(t, int32(2), result.UnsupportedCount)
		assert.Equal(t, int32(2), result.TotalCount)
		assert.False(t, result.AllSupported)
		assert.True(t, result.NoneSupported)
		assert.Len(t, result.UnsupportedGroups, 1)
		assert.Equal(t, "S19", result.UnsupportedGroups[0].Model)
		assert.Equal(t, "1.0.0", result.UnsupportedGroups[0].FirmwareVersion)
		assert.Equal(t, int32(2), result.UnsupportedGroups[0].Count)
		assert.Empty(t, result.SupportedDeviceIdentifiers)
	})

	t.Run("returns mixed results with partial support", func(t *testing.T) {
		provider := &mockCapabilitiesProvider{
			capabilities: map[string]*capabilitiespb.MinerCapabilities{
				"proto|Proto|Model1":   {Commands: &capabilitiespb.CommandCapabilities{RebootSupported: true}},
				"antminer|Bitmain|S19": {Commands: &capabilitiespb.CommandCapabilities{RebootSupported: false}},
			},
		}
		checker := NewCapabilityChecker(nil, provider)

		devices := []deviceInfo{
			{DeviceIdentifier: "device-1", DriverName: "proto", Manufacturer: "Proto", Model: "Model1", FirmwareVersion: "2.0.0"},
			{DeviceIdentifier: "device-2", DriverName: "antminer", Manufacturer: "Bitmain", Model: "S19", FirmwareVersion: "1.0.0"},
			{DeviceIdentifier: "device-3", DriverName: "antminer", Manufacturer: "Bitmain", Model: "S19", FirmwareVersion: "1.0.0"},
		}

		result := checker.checkDeviceCapabilities(ctx, devices, []string{sdk.CapabilityReboot})

		assert.Equal(t, int32(1), result.SupportedCount)
		assert.Equal(t, int32(2), result.UnsupportedCount)
		assert.Equal(t, int32(3), result.TotalCount)
		assert.False(t, result.AllSupported)
		assert.False(t, result.NoneSupported)
		assert.Len(t, result.UnsupportedGroups, 1)
		assert.Equal(t, []string{"device-1"}, result.SupportedDeviceIdentifiers)
	})

	t.Run("groups unsupported devices by model and firmware version", func(t *testing.T) {
		provider := &mockCapabilitiesProvider{
			capabilities: map[string]*capabilitiespb.MinerCapabilities{
				"antminer|Bitmain|S19":    {Commands: &capabilitiespb.CommandCapabilities{RebootSupported: false}},
				"antminer|Bitmain|S19Pro": {Commands: &capabilitiespb.CommandCapabilities{RebootSupported: false}},
			},
		}
		checker := NewCapabilityChecker(nil, provider)

		devices := []deviceInfo{
			{DeviceIdentifier: "device-1", DriverName: "antminer", Manufacturer: "Bitmain", Model: "S19", FirmwareVersion: "1.0.0"},
			{DeviceIdentifier: "device-2", DriverName: "antminer", Manufacturer: "Bitmain", Model: "S19", FirmwareVersion: "1.0.0"},
			{DeviceIdentifier: "device-3", DriverName: "antminer", Manufacturer: "Bitmain", Model: "S19Pro", FirmwareVersion: "2.0.0"},
			{DeviceIdentifier: "device-4", DriverName: "antminer", Manufacturer: "Bitmain", Model: "S19", FirmwareVersion: "1.1.0"},
		}

		result := checker.checkDeviceCapabilities(ctx, devices, []string{sdk.CapabilityReboot})

		assert.Equal(t, int32(0), result.SupportedCount)
		assert.Equal(t, int32(4), result.UnsupportedCount)
		assert.Len(t, result.UnsupportedGroups, 3)

		groupMap := make(map[string]*pb.UnsupportedMinerGroup)
		for _, g := range result.UnsupportedGroups {
			groupMap[g.Model+"|"+g.FirmwareVersion] = g
		}

		assert.Equal(t, int32(2), groupMap["S19|1.0.0"].Count)
		assert.Equal(t, int32(1), groupMap["S19Pro|2.0.0"].Count)
		assert.Equal(t, int32(1), groupMap["S19|1.1.0"].Count)
	})

	t.Run("uses Unknown for empty model and firmware", func(t *testing.T) {
		provider := &mockCapabilitiesProvider{
			capabilities: map[string]*capabilitiespb.MinerCapabilities{
				"antminer||": {Commands: &capabilitiespb.CommandCapabilities{RebootSupported: false}},
			},
		}
		checker := NewCapabilityChecker(nil, provider)

		devices := []deviceInfo{
			{DeviceIdentifier: "device-1", DriverName: "antminer", Manufacturer: "", Model: "", FirmwareVersion: ""},
		}

		result := checker.checkDeviceCapabilities(ctx, devices, []string{sdk.CapabilityReboot})

		assert.Len(t, result.UnsupportedGroups, 1)
		assert.Equal(t, "Unknown", result.UnsupportedGroups[0].Model)
		assert.Equal(t, "Unknown", result.UnsupportedGroups[0].FirmwareVersion)
	})

	t.Run("handles empty device list", func(t *testing.T) {
		provider := &mockCapabilitiesProvider{capabilities: map[string]*capabilitiespb.MinerCapabilities{}}
		checker := NewCapabilityChecker(nil, provider)

		result := checker.checkDeviceCapabilities(ctx, []deviceInfo{}, []string{sdk.CapabilityReboot})

		assert.Equal(t, int32(0), result.SupportedCount)
		assert.Equal(t, int32(0), result.UnsupportedCount)
		assert.Equal(t, int32(0), result.TotalCount)
		assert.True(t, result.AllSupported)
		assert.False(t, result.NoneSupported)
		assert.Empty(t, result.UnsupportedGroups)
		assert.Empty(t, result.SupportedDeviceIdentifiers)
	})

	t.Run("returns unsupported when capabilities provider returns nil", func(t *testing.T) {
		provider := &mockCapabilitiesProvider{
			capabilities: map[string]*capabilitiespb.MinerCapabilities{},
		}
		checker := NewCapabilityChecker(nil, provider)

		devices := []deviceInfo{
			{DeviceIdentifier: "device-1", DriverName: "unknown", Manufacturer: "Unknown", Model: "Unknown"},
		}

		result := checker.checkDeviceCapabilities(ctx, devices, []string{sdk.CapabilityReboot})

		assert.Equal(t, int32(0), result.SupportedCount)
		assert.Equal(t, int32(1), result.UnsupportedCount)
		assert.True(t, result.NoneSupported)
	})

	t.Run("checks firmware capabilities for manual upload", func(t *testing.T) {
		provider := &mockCapabilitiesProvider{
			capabilities: map[string]*capabilitiespb.MinerCapabilities{
				"proto|Proto|Rig1": {
					Commands: &capabilitiespb.CommandCapabilities{},
					Firmware: &capabilitiespb.FirmwareCapabilities{ManualUploadSupported: true},
				},
				"antminer|Bitmain|S19": {
					Commands: &capabilitiespb.CommandCapabilities{},
					Firmware: &capabilitiespb.FirmwareCapabilities{ManualUploadSupported: true},
				},
				"virtual|Virtual|Miner": {
					Commands: &capabilitiespb.CommandCapabilities{},
					Firmware: &capabilitiespb.FirmwareCapabilities{ManualUploadSupported: false},
				},
			},
		}
		checker := NewCapabilityChecker(nil, provider)

		devices := []deviceInfo{
			{DeviceIdentifier: "device-1", DriverName: "proto", Manufacturer: "Proto", Model: "Rig1", FirmwareVersion: "2.0.0"},
			{DeviceIdentifier: "device-2", DriverName: "antminer", Manufacturer: "Bitmain", Model: "S19", FirmwareVersion: "1.0.0"},
			{DeviceIdentifier: "device-3", DriverName: "virtual", Manufacturer: "Virtual", Model: "Miner", FirmwareVersion: "0.0.1"},
		}

		result := checker.checkDeviceCapabilities(ctx, devices, []string{sdk.CapabilityManualUpload})

		assert.Equal(t, int32(2), result.SupportedCount)
		assert.Equal(t, int32(1), result.UnsupportedCount)
		assert.Equal(t, int32(3), result.TotalCount)
		assert.False(t, result.AllSupported)
		assert.False(t, result.NoneSupported)
		assert.ElementsMatch(t, []string{"device-1", "device-2"}, result.SupportedDeviceIdentifiers)
	})

	t.Run("returns unsupported for manual upload when firmware capabilities is nil", func(t *testing.T) {
		provider := &mockCapabilitiesProvider{
			capabilities: map[string]*capabilitiespb.MinerCapabilities{
				"proto|Proto|Model1": {Commands: &capabilitiespb.CommandCapabilities{}},
			},
		}
		checker := NewCapabilityChecker(nil, provider)

		devices := []deviceInfo{
			{DeviceIdentifier: "device-1", DriverName: "proto", Manufacturer: "Proto", Model: "Model1"},
		}

		result := checker.checkDeviceCapabilities(ctx, devices, []string{sdk.CapabilityManualUpload})

		assert.Equal(t, int32(0), result.SupportedCount)
		assert.Equal(t, int32(1), result.UnsupportedCount)
		assert.True(t, result.NoneSupported)
	})

	t.Run("returns unsupported when commands is nil", func(t *testing.T) {
		provider := &mockCapabilitiesProvider{
			capabilities: map[string]*capabilitiespb.MinerCapabilities{
				"proto|Proto|Model1": {Commands: nil},
			},
		}
		checker := NewCapabilityChecker(nil, provider)

		devices := []deviceInfo{
			{DeviceIdentifier: "device-1", DriverName: "proto", Manufacturer: "Proto", Model: "Model1"},
		}

		result := checker.checkDeviceCapabilities(ctx, devices, []string{sdk.CapabilityReboot})

		assert.Equal(t, int32(0), result.SupportedCount)
		assert.Equal(t, int32(1), result.UnsupportedCount)
		assert.True(t, result.NoneSupported)
	})
}

func TestGetRequiredCapabilities(t *testing.T) {
	t.Run("returns correct capabilities for reboot command", func(t *testing.T) {
		caps := GetRequiredCapabilities(pb.CommandType_COMMAND_TYPE_REBOOT)

		assert.Equal(t, []string{sdk.CapabilityReboot}, caps)
	})

	t.Run("returns correct capabilities for start mining command", func(t *testing.T) {
		caps := GetRequiredCapabilities(pb.CommandType_COMMAND_TYPE_START_MINING)

		assert.Equal(t, []string{sdk.CapabilityMiningStart}, caps)
	})

	t.Run("returns correct capabilities for stop mining command", func(t *testing.T) {
		caps := GetRequiredCapabilities(pb.CommandType_COMMAND_TYPE_STOP_MINING)

		assert.Equal(t, []string{sdk.CapabilityMiningStop}, caps)
	})

	t.Run("returns correct capabilities for blink LED command", func(t *testing.T) {
		caps := GetRequiredCapabilities(pb.CommandType_COMMAND_TYPE_BLINK_LED)

		assert.Equal(t, []string{sdk.CapabilityLEDBlink}, caps)
	})

	t.Run("returns multiple capabilities for cooling mode command", func(t *testing.T) {
		caps := GetRequiredCapabilities(pb.CommandType_COMMAND_TYPE_SET_COOLING_MODE)

		assert.ElementsMatch(t, []string{sdk.CapabilityCoolingModeAir, sdk.CapabilityCoolingModeImmerse}, caps)
	})

	t.Run("returns correct capabilities for update mining pools command", func(t *testing.T) {
		caps := GetRequiredCapabilities(pb.CommandType_COMMAND_TYPE_UPDATE_MINING_POOLS)

		assert.Equal(t, []string{sdk.CapabilityPoolConfig}, caps)
	})

	t.Run("returns correct capabilities for download logs command", func(t *testing.T) {
		caps := GetRequiredCapabilities(pb.CommandType_COMMAND_TYPE_DOWNLOAD_LOGS)

		assert.Equal(t, []string{sdk.CapabilityLogsDownload}, caps)
	})

	t.Run("returns correct capabilities for firmware update command", func(t *testing.T) {
		caps := GetRequiredCapabilities(pb.CommandType_COMMAND_TYPE_FIRMWARE_UPDATE)

		assert.Equal(t, []string{sdk.CapabilityManualUpload}, caps)
	})

	t.Run("returns nil for unknown command type", func(t *testing.T) {
		caps := GetRequiredCapabilities(pb.CommandType_COMMAND_TYPE_UNSPECIFIED)

		assert.Nil(t, caps)
	})
}

func TestRequiresCapabilityCheck(t *testing.T) {
	t.Run("returns true for known command types", func(t *testing.T) {
		knownTypes := []pb.CommandType{
			pb.CommandType_COMMAND_TYPE_REBOOT,
			pb.CommandType_COMMAND_TYPE_START_MINING,
			pb.CommandType_COMMAND_TYPE_STOP_MINING,
			pb.CommandType_COMMAND_TYPE_BLINK_LED,
			pb.CommandType_COMMAND_TYPE_SET_COOLING_MODE,
			pb.CommandType_COMMAND_TYPE_UPDATE_MINING_POOLS,
			pb.CommandType_COMMAND_TYPE_DOWNLOAD_LOGS,
			pb.CommandType_COMMAND_TYPE_FIRMWARE_UPDATE,
		}

		for _, cmdType := range knownTypes {
			assert.True(t, RequiresCapabilityCheck(cmdType), "expected true for %v", cmdType)
		}
	})

	t.Run("returns false for unspecified command type", func(t *testing.T) {
		assert.False(t, RequiresCapabilityCheck(pb.CommandType_COMMAND_TYPE_UNSPECIFIED))
	})
}
