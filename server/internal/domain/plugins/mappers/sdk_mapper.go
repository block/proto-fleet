package mappers

import (
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/block/proto-fleet/server/sdk/v1"
)

// SDKDeviceMetricsToV2 converts SDK DeviceMetrics to telemetry V2 DeviceMetrics.
// This mapper translates between the plugin SDK format and the internal telemetry format.
func SDKDeviceMetricsToV2(sdkMetrics sdk.DeviceMetrics) modelsV2.DeviceMetrics {
	return modelsV2.DeviceMetrics{
		DeviceIdentifier: sdkMetrics.DeviceID,
		Timestamp:        sdkMetrics.Timestamp,
		FirmwareVersion:  sdkMetrics.FirmwareVersion,

		Health:       mapHealthStatus(sdkMetrics.Health),
		HealthReason: sdkMetrics.HealthReason,

		// Device-level aggregated metrics
		HashrateHS:   mapMetricValue(sdkMetrics.HashrateHS),
		TempC:        mapMetricValue(sdkMetrics.TempC),
		FanRPM:       mapMetricValue(sdkMetrics.FanRPM),
		PowerW:       mapMetricValue(sdkMetrics.PowerW),
		EfficiencyJH: mapMetricValue(sdkMetrics.EfficiencyJH),

		// Component-level metrics
		HashBoards:          mapHashBoards(sdkMetrics.HashBoards),
		PSUMetrics:          mapPSUMetrics(sdkMetrics.PSUMetrics),
		ControlBoardMetrics: mapControlBoardMetrics(sdkMetrics.ControlBoardMetrics),
		FanMetrics:          mapFanMetrics(sdkMetrics.FanMetrics),
		SensorMetrics:       mapSensorMetrics(sdkMetrics.SensorMetrics),

		StratumV2Support: mapStratumV2Support(sdkMetrics.StratumV2Support),
	}
}

func mapStratumV2Support(s sdk.StratumV2SupportStatus) modelsV2.StratumV2SupportStatus {
	switch s {
	case sdk.StratumV2SupportSupported:
		return modelsV2.StratumV2SupportSupported
	case sdk.StratumV2SupportUnsupported:
		return modelsV2.StratumV2SupportUnsupported
	case sdk.StratumV2SupportUnknown:
		return modelsV2.StratumV2SupportUnknown
	default:
		return modelsV2.StratumV2SupportUnspecified
	}
}

// mapMetricValue converts SDK MetricValue to V2 MetricValue
func mapMetricValue(sdkValue *sdk.MetricValue) *modelsV2.MetricValue {
	if sdkValue == nil {
		return nil
	}

	return &modelsV2.MetricValue{
		Value:    sdkValue.Value,
		Kind:     mapMetricKind(sdkValue.Kind),
		MetaData: mapMetricValueMetaData(sdkValue.MetaData),
	}
}

// mapMetricValueMetaData converts SDK MetricValueMetaData to V2 MetricValueMetaData
func mapMetricValueMetaData(sdkMeta *sdk.MetricValueMetaData) *modelsV2.MetricValueMetaData {
	if sdkMeta == nil {
		return nil
	}

	return &modelsV2.MetricValueMetaData{
		Window:    sdkMeta.Window,
		Min:       sdkMeta.Min,
		Max:       sdkMeta.Max,
		Avg:       sdkMeta.Avg,
		StdDev:    sdkMeta.StdDev,
		Timestamp: sdkMeta.Timestamp,
	}
}

// mapMetricKind converts SDK MetricKind to V2 MetricKind
func mapMetricKind(sdkKind sdk.MetricKind) modelsV2.MetricKind {
	switch sdkKind {
	case sdk.MetricKindGauge:
		return modelsV2.MetricKindGauge
	case sdk.MetricKindRate:
		return modelsV2.MetricKindRate
	case sdk.MetricKindCounter:
		return modelsV2.MetricKindCounter
	case sdk.MetricKindUnspecified:
		return modelsV2.MetricKindUnknown
	default:
		return modelsV2.MetricKindUnknown
	}
}

// mapHealthStatus converts SDK HealthStatus to V2 HealthStatus
func mapHealthStatus(sdkHealth sdk.HealthStatus) modelsV2.HealthStatus {
	switch sdkHealth {
	case sdk.HealthHealthyActive:
		return modelsV2.HealthHealthyActive
	case sdk.HealthHealthyInactive:
		return modelsV2.HealthHealthyInactive
	case sdk.HealthNeedsMiningPool:
		return modelsV2.HealthHealthyInactive
	case sdk.HealthWarning:
		return modelsV2.HealthWarning
	case sdk.HealthCritical:
		return modelsV2.HealthCritical
	case sdk.HealthUnknown:
		return modelsV2.HealthUnknown
	case sdk.HealthStatusUnspecified:
		return modelsV2.HealthUnknown
	default:
		return modelsV2.HealthUnknown
	}
}

// mapComponentStatus converts SDK ComponentStatus to V2 ComponentStatus
func mapComponentStatus(sdkStatus sdk.ComponentStatus) modelsV2.ComponentStatus {
	switch sdkStatus {
	case sdk.ComponentStatusHealthy:
		return modelsV2.ComponentStatusHealthy
	case sdk.ComponentStatusWarning:
		return modelsV2.ComponentStatusWarning
	case sdk.ComponentStatusCritical:
		return modelsV2.ComponentStatusCritical
	case sdk.ComponentStatusOffline:
		return modelsV2.ComponentStatusOffline
	case sdk.ComponentStatusDisabled:
		return modelsV2.ComponentStatusDisabled
	case sdk.ComponentStatusUnknown:
		return modelsV2.ComponentStatusUnknown
	case sdk.ComponentStatusUnspecified:
		return modelsV2.ComponentStatusUnknown
	default:
		return modelsV2.ComponentStatusUnknown
	}
}

// mapComponentInfo converts SDK ComponentInfo to V2 ComponentInfo
func mapComponentInfo(sdkInfo sdk.ComponentInfo) modelsV2.ComponentInfo {
	return modelsV2.ComponentInfo{
		Index:        int(sdkInfo.Index),
		Name:         sdkInfo.Name,
		Status:       mapComponentStatus(sdkInfo.Status),
		StatusReason: sdkInfo.StatusReason,
		Timestamp:    sdkInfo.Timestamp,
	}
}

// mapHashBoards converts SDK HashBoardMetrics slice to V2 HashBoardMetrics slice
func mapHashBoards(sdkHashBoards []sdk.HashBoardMetrics) []modelsV2.HashBoardMetrics {
	if sdkHashBoards == nil {
		return nil
	}

	v2HashBoards := make([]modelsV2.HashBoardMetrics, len(sdkHashBoards))
	for i, sdkHB := range sdkHashBoards {
		v2HashBoards[i] = modelsV2.HashBoardMetrics{
			ComponentInfo: mapComponentInfo(sdkHB.ComponentInfo),
			SerialNumber:  sdkHB.SerialNumber,

			// Performance metrics
			HashRateHS: mapMetricValue(sdkHB.HashRateHS),
			TempC:      mapMetricValue(sdkHB.TempC),

			// Electrical metrics
			VoltageV: mapMetricValue(sdkHB.VoltageV),
			CurrentA: mapMetricValue(sdkHB.CurrentA),

			// Temperature sensors
			InletTempC:   mapMetricValue(sdkHB.InletTempC),
			OutletTempC:  mapMetricValue(sdkHB.OutletTempC),
			AmbientTempC: mapMetricValue(sdkHB.AmbientTempC),

			// Chip information
			ChipCount:        mapInt32ToIntPtr(sdkHB.ChipCount),
			ChipFrequencyMHz: mapMetricValue(sdkHB.ChipFrequencyMHz),

			// Sub-components
			ASICs:      mapASICMetrics(sdkHB.ASICs),
			FanMetrics: mapFanMetrics(sdkHB.FanMetrics),
		}
	}
	return v2HashBoards
}

// mapASICMetrics converts SDK ASICMetrics slice to V2 ASICMetrics slice
func mapASICMetrics(sdkASICs []sdk.ASICMetrics) []modelsV2.ASICMetrics {
	if sdkASICs == nil {
		return nil
	}

	v2ASICs := make([]modelsV2.ASICMetrics, len(sdkASICs))
	for i, sdkASIC := range sdkASICs {
		v2ASICs[i] = modelsV2.ASICMetrics{
			ComponentInfo: mapComponentInfo(sdkASIC.ComponentInfo),
			TempC:         mapMetricValue(sdkASIC.TempC),
			FrequencyMHz:  mapMetricValue(sdkASIC.FrequencyMHz),
			VoltageV:      mapMetricValue(sdkASIC.VoltageV),
			HashrateHS:    mapMetricValue(sdkASIC.HashrateHS),
		}
	}
	return v2ASICs
}

// mapPSUMetrics converts SDK PSUMetrics slice to V2 PSUMetrics slice
func mapPSUMetrics(sdkPSUs []sdk.PSUMetrics) []modelsV2.PSUMetrics {
	if sdkPSUs == nil {
		return nil
	}

	v2PSUs := make([]modelsV2.PSUMetrics, len(sdkPSUs))
	for i, sdkPSU := range sdkPSUs {
		v2PSUs[i] = modelsV2.PSUMetrics{
			ComponentInfo: mapComponentInfo(sdkPSU.ComponentInfo),

			// Output measurements
			OutputPowerW:   mapMetricValue(sdkPSU.OutputPowerW),
			OutputVoltageV: mapMetricValue(sdkPSU.OutputVoltageV),
			OutputCurrentA: mapMetricValue(sdkPSU.OutputCurrentA),

			// Input measurements
			InputPowerW:   mapMetricValue(sdkPSU.InputPowerW),
			InputVoltageV: mapMetricValue(sdkPSU.InputVoltageV),
			InputCurrentA: mapMetricValue(sdkPSU.InputCurrentA),

			// Additional metrics
			HotSpotTempC:      mapMetricValue(sdkPSU.HotSpotTempC),
			EfficiencyPercent: mapMetricValue(sdkPSU.EfficiencyPercent),

			// Sub-components
			FanMetrics: mapFanMetrics(sdkPSU.FanMetrics),
		}
	}
	return v2PSUs
}

// mapFanMetrics converts SDK FanMetrics slice to V2 FanMetrics slice
func mapFanMetrics(sdkFans []sdk.FanMetrics) []modelsV2.FanMetrics {
	if sdkFans == nil {
		return nil
	}

	v2Fans := make([]modelsV2.FanMetrics, len(sdkFans))
	for i, sdkFan := range sdkFans {
		v2Fans[i] = modelsV2.FanMetrics{
			ComponentInfo: mapComponentInfo(sdkFan.ComponentInfo),
			RPM:           mapMetricValue(sdkFan.RPM),
			TempC:         mapMetricValue(sdkFan.TempC),
			Percent:       mapMetricValue(sdkFan.Percent),
		}
	}
	return v2Fans
}

// mapControlBoardMetrics converts SDK ControlBoardMetrics slice to V2 ControlBoardMetrics slice
func mapControlBoardMetrics(sdkCBs []sdk.ControlBoardMetrics) []modelsV2.ControlBoardMetrics {
	if sdkCBs == nil {
		return nil
	}

	v2CBs := make([]modelsV2.ControlBoardMetrics, len(sdkCBs))
	for i, sdkCB := range sdkCBs {
		v2CBs[i] = modelsV2.ControlBoardMetrics{
			ComponentInfo: mapComponentInfo(sdkCB.ComponentInfo),
		}
	}
	return v2CBs
}

// mapSensorMetrics converts SDK SensorMetrics slice to V2 SensorMetrics slice
func mapSensorMetrics(sdkSensors []sdk.SensorMetrics) []modelsV2.SensorMetrics {
	if sdkSensors == nil {
		return nil
	}

	v2Sensors := make([]modelsV2.SensorMetrics, len(sdkSensors))
	for i, sdkSensor := range sdkSensors {
		v2Sensors[i] = modelsV2.SensorMetrics{
			ComponentInfo: mapComponentInfo(sdkSensor.ComponentInfo),
			Type:          sdkSensor.Type,
			Unit:          sdkSensor.Unit,
			Value:         mapMetricValue(sdkSensor.Value),
		}
	}
	return v2Sensors
}

// Helper functions

// mapInt32ToIntPtr converts *int32 to *int
func mapInt32ToIntPtr(val *int32) *int {
	if val == nil {
		return nil
	}
	intVal := int(*val)
	return &intVal
}
