package plugins

import (
	capabilitiespb "github.com/block/proto-fleet/server/generated/grpc/capabilities/v1"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

const (
	// Default values for capabilities that require specific parameters
	defaultPoolMaxCount = 3
)

// ConvertToMinerCapabilities converts SDK capabilities to protobuf MinerCapabilities.
// This function maps the boolean capability flags from plugins to the structured
// protobuf message used by the fleet management API.
func ConvertToMinerCapabilities(caps sdk.Capabilities, manufacturer string) *capabilitiespb.MinerCapabilities {
	if caps == nil {
		return nil
	}

	return &capabilitiespb.MinerCapabilities{
		Manufacturer:   manufacturer,
		Authentication: convertAuthenticationCapabilities(caps),
		Commands:       convertCommandCapabilities(caps),
		Telemetry:      convertTelemetryCapabilities(caps),
		Firmware:       convertFirmwareCapabilities(caps),
	}
}

func convertAuthenticationCapabilities(caps sdk.Capabilities) *capabilitiespb.AuthenticationCapabilities {
	var methods []capabilitiespb.AuthenticationMethod

	if caps[sdk.CapabilityBasicAuth] {
		methods = append(methods, capabilitiespb.AuthenticationMethod_AUTHENTICATION_METHOD_BASIC)
	}
	if caps[sdk.CapabilityAsymmetricAuth] {
		methods = append(methods, capabilitiespb.AuthenticationMethod_AUTHENTICATION_METHOD_ASYMMETRIC_KEY)
	}

	if len(methods) == 0 {
		return nil
	}

	return &capabilitiespb.AuthenticationCapabilities{
		SupportedMethods: methods,
	}
}

func convertCommandCapabilities(caps sdk.Capabilities) *capabilitiespb.CommandCapabilities {
	poolMaxCount := int32(0)
	if caps[sdk.CapabilityPoolConfig] {
		poolMaxCount = defaultPoolMaxCount
	}

	return &capabilitiespb.CommandCapabilities{
		RebootSupported:              caps[sdk.CapabilityReboot],
		MiningStartSupported:         caps[sdk.CapabilityMiningStart],
		MiningStopSupported:          caps[sdk.CapabilityMiningStop],
		LedBlinkSupported:            caps[sdk.CapabilityLEDBlink],
		FactoryResetSupported:        caps[sdk.CapabilityFactoryReset],
		AirCoolingSupported:          caps[sdk.CapabilityCoolingModeAir],
		ImmersionCoolingSupported:    caps[sdk.CapabilityCoolingModeImmerse],
		PoolSwitchingSupported:       caps[sdk.CapabilityPoolConfig],
		PoolMaxCount:                 poolMaxCount,
		PoolPrioritySupported:        caps[sdk.CapabilityPoolPriority],
		LogsDownloadSupported:        caps[sdk.CapabilityLogsDownload],
		PowerModeEfficiencySupported: caps[sdk.CapabilityPowerModeEfficiency],
		UpdateMinerPasswordSupported: caps[sdk.CapabilityUpdateMinerPassword],
		CurtailFullSupported:         caps[sdk.CapabilityCurtailFull],
		CurtailEfficiencySupported:   caps[sdk.CapabilityCurtailEfficiency],
		CurtailPartialSupported:      caps[sdk.CapabilityCurtailPartial],
	}
}

func convertTelemetryCapabilities(caps sdk.Capabilities) *capabilitiespb.TelemetryCapabilities {
	return &capabilitiespb.TelemetryCapabilities{
		RealtimeTelemetrySupported: caps[sdk.CapabilityRealtimeTelemetry],
		HistoricalDataSupported:    caps[sdk.CapabilityHistoricalData],
		HashrateReported:           caps[sdk.CapabilityHashrateReported],
		PowerUsageReported:         caps[sdk.CapabilityPowerUsage],
		TemperatureReported:        caps[sdk.CapabilityTemperature],
		FanSpeedReported:           caps[sdk.CapabilityFanSpeed],
		EfficiencyReported:         caps[sdk.CapabilityEfficiency],
		UptimeReported:             caps[sdk.CapabilityUptime],
		ErrorCountReported:         caps[sdk.CapabilityErrorCount],
		MinerStatusReported:        caps[sdk.CapabilityMinerStatus],
		PoolStatsReported:          caps[sdk.CapabilityPoolStats],
		PerChipStatsReported:       caps[sdk.CapabilityPerChipStats],
		PerBoardStatsReported:      caps[sdk.CapabilityPerBoardStats],
		PsuStatsReported:           caps[sdk.CapabilityPSUStats],
	}
}

func convertFirmwareCapabilities(caps sdk.Capabilities) *capabilitiespb.FirmwareCapabilities {
	return &capabilitiespb.FirmwareCapabilities{
		OtaUpdateSupported:    caps[sdk.CapabilityOTAUpdate],
		ManualUploadSupported: caps[sdk.CapabilityManualUpload],
	}
}
