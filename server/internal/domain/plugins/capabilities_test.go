package plugins

import (
	"testing"

	capabilitiespb "github.com/block/proto-fleet/server/generated/grpc/capabilities/v1"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertToMinerCapabilities_NilCapabilities(t *testing.T) {
	result := ConvertToMinerCapabilities(nil, "Proto")
	assert.Nil(t, result)
}

func TestConvertToMinerCapabilities_EmptyCapabilities(t *testing.T) {
	caps := sdk.Capabilities{}
	result := ConvertToMinerCapabilities(caps, "Proto")

	require.NotNil(t, result)
	assert.Equal(t, "Proto", result.Manufacturer)
	assert.Nil(t, result.Authentication)
}

func TestConvertToMinerCapabilities_ProtoDevice(t *testing.T) {
	caps := sdk.Capabilities{
		sdk.CapabilityAsymmetricAuth:     true,
		sdk.CapabilityReboot:             true,
		sdk.CapabilityMiningStart:        true,
		sdk.CapabilityMiningStop:         true,
		sdk.CapabilityLEDBlink:           true,
		sdk.CapabilityCoolingModeAir:     true,
		sdk.CapabilityCoolingModeImmerse: true,
		sdk.CapabilityPoolConfig:         true,
		sdk.CapabilityPoolPriority:       true,
		sdk.CapabilityLogsDownload:       true,
		sdk.CapabilityRealtimeTelemetry:  true,
		sdk.CapabilityHistoricalData:     true,
		sdk.CapabilityHashrateReported:   true,
		sdk.CapabilityPowerUsage:         true,
		sdk.CapabilityTemperature:        true,
		sdk.CapabilityFanSpeed:           true,
		sdk.CapabilityEfficiency:         true,
		sdk.CapabilityUptime:             true,
		sdk.CapabilityErrorCount:         true,
		sdk.CapabilityMinerStatus:        true,
		sdk.CapabilityPoolStats:          true,
		sdk.CapabilityPerChipStats:       true,
		sdk.CapabilityPerBoardStats:      true,
		sdk.CapabilityPSUStats:           true,
		sdk.CapabilityOTAUpdate:          true,
		sdk.CapabilityManualUpload:       true,
	}

	result := ConvertToMinerCapabilities(caps, "Proto")

	require.NotNil(t, result)
	assert.Equal(t, "Proto", result.Manufacturer)

	// Authentication
	require.NotNil(t, result.Authentication)
	assert.Contains(t, result.Authentication.SupportedMethods, capabilitiespb.AuthenticationMethod_AUTHENTICATION_METHOD_ASYMMETRIC_KEY)

	// Commands
	require.NotNil(t, result.Commands)
	assert.True(t, result.Commands.RebootSupported)
	assert.True(t, result.Commands.MiningStartSupported)
	assert.True(t, result.Commands.MiningStopSupported)
	assert.True(t, result.Commands.LedBlinkSupported)
	assert.True(t, result.Commands.AirCoolingSupported)
	assert.True(t, result.Commands.ImmersionCoolingSupported)
	assert.True(t, result.Commands.PoolSwitchingSupported)
	assert.Equal(t, int32(3), result.Commands.PoolMaxCount)
	assert.True(t, result.Commands.PoolPrioritySupported)
	assert.True(t, result.Commands.LogsDownloadSupported)

	// Telemetry
	require.NotNil(t, result.Telemetry)
	assert.True(t, result.Telemetry.RealtimeTelemetrySupported)
	assert.True(t, result.Telemetry.HistoricalDataSupported)
	assert.True(t, result.Telemetry.HashrateReported)
	assert.True(t, result.Telemetry.PowerUsageReported)
	assert.True(t, result.Telemetry.TemperatureReported)
	assert.True(t, result.Telemetry.FanSpeedReported)
	assert.True(t, result.Telemetry.EfficiencyReported)
	assert.True(t, result.Telemetry.UptimeReported)
	assert.True(t, result.Telemetry.ErrorCountReported)
	assert.True(t, result.Telemetry.MinerStatusReported)
	assert.True(t, result.Telemetry.PoolStatsReported)
	assert.True(t, result.Telemetry.PerChipStatsReported)
	assert.True(t, result.Telemetry.PerBoardStatsReported)
	assert.True(t, result.Telemetry.PsuStatsReported)

	// Firmware
	require.NotNil(t, result.Firmware)
	assert.True(t, result.Firmware.OtaUpdateSupported)
	assert.True(t, result.Firmware.ManualUploadSupported)
}

func TestConvertToMinerCapabilities_AntminerDevice(t *testing.T) {
	caps := sdk.Capabilities{
		sdk.CapabilityBasicAuth:          true,
		sdk.CapabilityReboot:             true,
		sdk.CapabilityMiningStart:        false,
		sdk.CapabilityMiningStop:         false,
		sdk.CapabilityLEDBlink:           true,
		sdk.CapabilityCoolingModeAir:     false,
		sdk.CapabilityCoolingModeImmerse: false,
		sdk.CapabilityPoolConfig:         true,
		sdk.CapabilityPoolPriority:       true,
		sdk.CapabilityLogsDownload:       false,
		sdk.CapabilityRealtimeTelemetry:  true,
		sdk.CapabilityHistoricalData:     false,
		sdk.CapabilityHashrateReported:   true,
		sdk.CapabilityPowerUsage:         true,
		sdk.CapabilityTemperature:        true,
		sdk.CapabilityFanSpeed:           true,
		sdk.CapabilityEfficiency:         true,
		sdk.CapabilityUptime:             true,
		sdk.CapabilityErrorCount:         true,
		sdk.CapabilityMinerStatus:        true,
		sdk.CapabilityPoolStats:          true,
		sdk.CapabilityPerChipStats:       true,
		sdk.CapabilityPerBoardStats:      true,
		sdk.CapabilityPSUStats:           false,
		sdk.CapabilityOTAUpdate:          false,
		sdk.CapabilityManualUpload:       true,
	}

	result := ConvertToMinerCapabilities(caps, "Bitmain")

	require.NotNil(t, result)
	assert.Equal(t, "Bitmain", result.Manufacturer)

	// Authentication
	require.NotNil(t, result.Authentication)
	assert.Contains(t, result.Authentication.SupportedMethods, capabilitiespb.AuthenticationMethod_AUTHENTICATION_METHOD_BASIC)

	// Commands - Antminer specific
	require.NotNil(t, result.Commands)
	assert.True(t, result.Commands.RebootSupported)
	assert.False(t, result.Commands.MiningStartSupported)
	assert.False(t, result.Commands.MiningStopSupported)
	assert.True(t, result.Commands.LedBlinkSupported)
	assert.False(t, result.Commands.AirCoolingSupported)
	assert.False(t, result.Commands.ImmersionCoolingSupported)
	assert.True(t, result.Commands.PoolSwitchingSupported)
	assert.Equal(t, int32(3), result.Commands.PoolMaxCount)
	assert.True(t, result.Commands.PoolPrioritySupported)
	assert.False(t, result.Commands.LogsDownloadSupported)

	// Telemetry
	require.NotNil(t, result.Telemetry)
	assert.True(t, result.Telemetry.RealtimeTelemetrySupported)
	assert.False(t, result.Telemetry.HistoricalDataSupported)

	// Firmware
	require.NotNil(t, result.Firmware)
	assert.False(t, result.Firmware.OtaUpdateSupported)
	assert.True(t, result.Firmware.ManualUploadSupported)
}

func TestConvertToMinerCapabilities_UnknownManufacturer(t *testing.T) {
	caps := sdk.Capabilities{
		sdk.CapabilityBasicAuth:         true,
		sdk.CapabilityRealtimeTelemetry: true,
	}

	result := ConvertToMinerCapabilities(caps, "UnknownManufacturer")

	require.NotNil(t, result)
	assert.Equal(t, "UnknownManufacturer", result.Manufacturer)

	// Telemetry should be set based on capabilities
	require.NotNil(t, result.Telemetry)
	assert.True(t, result.Telemetry.RealtimeTelemetrySupported)
}

func TestConvertAuthenticationCapabilities_NoMethods(t *testing.T) {
	caps := sdk.Capabilities{
		sdk.CapabilityReboot: true, // No auth capabilities
	}

	result := convertAuthenticationCapabilities(caps)
	assert.Nil(t, result)
}

func TestConvertAuthenticationCapabilities_BothMethods(t *testing.T) {
	caps := sdk.Capabilities{
		sdk.CapabilityBasicAuth:      true,
		sdk.CapabilityAsymmetricAuth: true,
	}

	result := convertAuthenticationCapabilities(caps)

	require.NotNil(t, result)
	assert.Len(t, result.SupportedMethods, 2)
	assert.Contains(t, result.SupportedMethods, capabilitiespb.AuthenticationMethod_AUTHENTICATION_METHOD_BASIC)
	assert.Contains(t, result.SupportedMethods, capabilitiespb.AuthenticationMethod_AUTHENTICATION_METHOD_ASYMMETRIC_KEY)
}

func TestConvertCommandCapabilities_CoolingModes(t *testing.T) {
	tests := []struct {
		name                       string
		airCooling                 bool
		immersionCooling           bool
		expectedAirSupported       bool
		expectedImmersionSupported bool
	}{
		{
			name:                       "Both cooling modes supported",
			airCooling:                 true,
			immersionCooling:           true,
			expectedAirSupported:       true,
			expectedImmersionSupported: true,
		},
		{
			name:                       "Only air cooling supported",
			airCooling:                 true,
			immersionCooling:           false,
			expectedAirSupported:       true,
			expectedImmersionSupported: false,
		},
		{
			name:                       "Only immersion cooling supported",
			airCooling:                 false,
			immersionCooling:           true,
			expectedAirSupported:       false,
			expectedImmersionSupported: true,
		},
		{
			name:                       "No cooling modes supported",
			airCooling:                 false,
			immersionCooling:           false,
			expectedAirSupported:       false,
			expectedImmersionSupported: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			caps := sdk.Capabilities{
				sdk.CapabilityCoolingModeAir:     tt.airCooling,
				sdk.CapabilityCoolingModeImmerse: tt.immersionCooling,
			}

			result := convertCommandCapabilities(caps)

			assert.Equal(t, tt.expectedAirSupported, result.AirCoolingSupported)
			assert.Equal(t, tt.expectedImmersionSupported, result.ImmersionCoolingSupported)
		})
	}
}

func TestConvertCommandCapabilities_PoolMaxCount(t *testing.T) {
	tests := []struct {
		name            string
		poolConfigCap   bool
		expectedPoolMax int32
	}{
		{"Pool config enabled", true, 3},
		{"Pool config disabled", false, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			caps := sdk.Capabilities{
				sdk.CapabilityPoolConfig: tt.poolConfigCap,
			}

			result := convertCommandCapabilities(caps)
			assert.Equal(t, tt.expectedPoolMax, result.PoolMaxCount)
		})
	}
}

func TestConvertCommandCapabilities_CurtailmentLevels(t *testing.T) {
	caps := sdk.Capabilities{
		sdk.CapabilityCurtailFull:       true,
		sdk.CapabilityCurtailEfficiency: true,
		sdk.CapabilityCurtailPartial:    false,
	}

	result := convertCommandCapabilities(caps)

	assert.True(t, result.CurtailFullSupported)
	assert.True(t, result.CurtailEfficiencySupported)
	assert.False(t, result.CurtailPartialSupported)
}
