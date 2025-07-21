package antminer

import (
	"strconv"
	"time"

	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/antminer/rpc"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	telemetryModels "github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"
)

const (
	minerComponentType = "miner"
	asicComponentType  = "asic"

	hashrateMHSMeasurement  = "hashrate_mhs"
	temperatureCMeasurement = "temperature_c"

	hashrateTypeAverage = "HASHRATE_TYPE_AVERAGE"
)

type TelemetryMapper struct {
	deviceIdentifier models.DeviceIdentifier
}

func NewTelemetryMapper(deviceIdentifier models.DeviceIdentifier) *TelemetryMapper {
	return &TelemetryMapper{
		deviceIdentifier: deviceIdentifier,
	}
}

func (tm *TelemetryMapper) ToTelemetry(
	summary *rpc.SummaryResponse,
	devs *rpc.DevsResponse,
	timestamp time.Time,
) []telemetryModels.Telemetry {
	var telemetry []telemetryModels.Telemetry

	minerLevelTelemetry := tm.mapMinerLevelTelemetry(summary, devs, timestamp)
	telemetry = append(telemetry, minerLevelTelemetry...)

	asicTelemetry := tm.mapASICTelemetry(devs, timestamp)
	telemetry = append(telemetry, asicTelemetry...)

	return telemetry
}

func (tm *TelemetryMapper) mapMinerLevelTelemetry(
	summary *rpc.SummaryResponse,
	devs *rpc.DevsResponse,
	timestamp time.Time,
) []telemetryModels.Telemetry {
	var result []telemetryModels.Telemetry

	componentID := strconv.Itoa(summary.ID)

	// Add hashrate telemetry
	if len(summary.Summary) > 0 {
		hashrateMHS := summary.Summary[0].GHSAv * 1e3

		result = append(result, telemetryModels.Telemetry{
			Measurement: hashrateMHSMeasurement,
			Fields: map[string]any{
				"value": hashrateMHS,
			},
			Tags: map[string]string{
				"device_id":      tm.deviceIdentifier.String(),
				"component_type": minerComponentType,
				"component_id":   componentID,
				"hashrate_type":  hashrateTypeAverage,
			},
			Timestamp: timestamp,
		})
	}

	if len(devs.Devs) > 0 {
		maxTemp := tm.maxASICTemperature(devs)

		result = append(result, telemetryModels.Telemetry{
			Measurement: temperatureCMeasurement,
			Fields: map[string]any{
				"value": maxTemp,
			},
			Tags: map[string]string{
				"device_id":      tm.deviceIdentifier.String(),
				"component_type": minerComponentType,
				"component_id":   componentID,
			},
			Timestamp: timestamp,
		})
	}

	return result
}

func (tm *TelemetryMapper) mapASICTelemetry(
	devs *rpc.DevsResponse,
	timestamp time.Time,
) []telemetryModels.Telemetry {
	var result []telemetryModels.Telemetry

	for _, dev := range devs.Devs {
		result = append(result, telemetryModels.Telemetry{
			Measurement: temperatureCMeasurement,
			Fields: map[string]any{
				"value": dev.Temperature,
			},
			Tags: map[string]string{
				"device_id":      tm.deviceIdentifier.String(),
				"component_type": asicComponentType,
				"component_id":   strconv.Itoa(dev.ID),
			},
			Timestamp: timestamp,
		})

		// Add device hashrate
		result = append(result, telemetryModels.Telemetry{
			Measurement: hashrateMHSMeasurement,
			Fields: map[string]any{
				"value": dev.MHSAv,
			},
			Tags: map[string]string{
				"device_id":      tm.deviceIdentifier.String(),
				"component_type": asicComponentType,
				"component_id":   strconv.Itoa(dev.ID),
				"hashrate_type":  hashrateTypeAverage,
			},
			Timestamp: timestamp,
		})
	}

	return result
}

func (tm *TelemetryMapper) maxASICTemperature(devs *rpc.DevsResponse) float64 {
	var maxTemp float64
	for i, dev := range devs.Devs {
		if i == 0 || dev.Temperature > maxTemp {
			maxTemp = dev.Temperature
		}
	}
	return maxTemp
}
