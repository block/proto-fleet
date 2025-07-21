package proto

import (
	"fmt"
	"strconv"
	"time"

	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_common_api"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_data_api"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	telemetryModels "github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"
)

// TelemetryMapper handles mapping between miner data API and fleet telemetry models
type TelemetryMapper struct {
	deviceIdentifier models.DeviceIdentifier
}

// NewTelemetryMapper creates a new telemetry mapper
func NewTelemetryMapper(deviceIdentifier models.DeviceIdentifier) *TelemetryMapper {
	return &TelemetryMapper{
		deviceIdentifier: deviceIdentifier,
	}
}

// MapToTimeSeriesRequests creates time series requests for all relevant data types
func (tm *TelemetryMapper) MapToTimeSeriesRequests(after time.Time) []*miner_data_api.TimeSeriesDataRequest {
	//nolint:gosec // G115: time will not be an overflow risk here and conversion is required.
	afterTimestamp := &miner_common_api.Timestamp{
		Seconds: uint64(after.Unix()),
		Nanos:   uint32(after.Nanosecond()),
	}

	now := time.Now()
	//nolint:gosec // G115: time will not be an overflow risk here and conversion is required.
	nowTimestamp := &miner_common_api.Timestamp{
		Seconds: uint64(now.Unix()),
		Nanos:   uint32(now.Nanosecond()),
	}

	timeInterval := &miner_common_api.Interval{
		StartTime: afterTimestamp,
		EndTime:   nowTimestamp,
	}

	var requests []*miner_data_api.TimeSeriesDataRequest

	// Miner-level metrics
	minerDataTypes := []miner_data_api.DataType{
		miner_data_api.DataType_DATA_TYPE_MINER_HASHRATE_MH_S,
		miner_data_api.DataType_DATA_TYPE_MINER_TEMPERATURE_C,
		miner_data_api.DataType_DATA_TYPE_MINER_POWER_W,
		miner_data_api.DataType_DATA_TYPE_MINER_EFFICIENCY_J_TH,
	}

	for _, dataType := range minerDataTypes {
		req := &miner_data_api.TimeSeriesDataRequest{
			TimeInterval: timeInterval,
			DataType:     dataType,
			ComponentId: &miner_data_api.ComponentId{
				Id: &miner_data_api.ComponentId_ComponentId{
					ComponentId: 0, // 0 for miner-level
				},
			},
		}

		// Add hashrate type for hashrate requests
		if dataType == miner_data_api.DataType_DATA_TYPE_MINER_HASHRATE_MH_S {
			req.HashrateType = miner_data_api.HashrateType_HASHRATE_TYPE_AVERAGE
		}

		requests = append(requests, req)
	}

	// PSU-level metrics
	psuDataTypes := []miner_data_api.DataType{
		miner_data_api.DataType_DATA_TYPE_PSU_TEMPERATURE_C,
		miner_data_api.DataType_DATA_TYPE_PSU_VOLTAGE_MV,
		miner_data_api.DataType_DATA_TYPE_PSU_CURRENT_MA,
		miner_data_api.DataType_DATA_TYPE_PSU_POWER_W,
	}

	for _, dataType := range psuDataTypes {
		req := &miner_data_api.TimeSeriesDataRequest{
			TimeInterval: timeInterval,
			DataType:     dataType,
			ComponentId: &miner_data_api.ComponentId{
				Id: &miner_data_api.ComponentId_ComponentId{
					ComponentId: 1, // 1 for PSU
				},
			},
		}
		requests = append(requests, req)
	}

	// Fan RPM
	fanReq := &miner_data_api.TimeSeriesDataRequest{
		TimeInterval: timeInterval,
		DataType:     miner_data_api.DataType_DATA_TYPE_FAN_RPM,
		ComponentId: &miner_data_api.ComponentId{
			Id: &miner_data_api.ComponentId_ComponentId{
				ComponentId: 2, // 2 for fan
			},
		},
	}
	requests = append(requests, fanReq)

	// Note: Hashboard and ASIC metrics would require specific component IDs
	// For now, we'll focus on miner-level, PSU, and fan metrics
	// TODO: Add hashboard and ASIC metrics when component discovery is implemented

	return requests
}

// MapToTelemetryModels converts protobuf responses to fleet telemetry models
func (tm *TelemetryMapper) MapToTelemetryModels(responses []*miner_data_api.TimeSeriesDataResponse) []telemetryModels.Telemetry {
	var telemetryData []telemetryModels.Telemetry

	for _, response := range responses {
		if response == nil || response.Result != miner_common_api.ApiResult_RESULT_SUCCESS {
			continue
		}

		// Get measurement name and component info
		measurementName := tm.getMeasurementName(response.DataType)
		componentType, componentID := tm.getComponentInfo(response.ComponentId)

		// Convert each data point
		for _, point := range response.DataPoints {
			if point == nil || point.Timestamp == nil {
				continue
			}

			// Convert protobuf timestamp to time.Time
			//nolint:gosec // G115: time will not be an overflow risk here and conversion is required.
			timestamp := time.Unix(int64(point.Timestamp.Seconds), int64(point.Timestamp.Nanos))

			// Create telemetry entry
			telemetry := telemetryModels.Telemetry{
				Measurement: measurementName,
				Fields: map[string]any{
					"value": point.Value,
				},
				Tags: map[string]string{
					"device_id":      tm.deviceIdentifier.String(),
					"component_type": componentType,
					"component_id":   componentID,
					"data_type":      response.DataType.String(),
				},
				Timestamp: timestamp,
			}

			// Add hashrate type tag if applicable
			if response.DataType == miner_data_api.DataType_DATA_TYPE_MINER_HASHRATE_MH_S ||
				response.DataType == miner_data_api.DataType_DATA_TYPE_HB_HASHRATE_MH_S ||
				response.DataType == miner_data_api.DataType_DATA_TYPE_ASIC_HASHRATE_MH_S {
				telemetry.Tags["hashrate_type"] = response.HashrateType.String()
			}

			telemetryData = append(telemetryData, telemetry)
		}
	}

	return telemetryData
}

// getMeasurementName returns the measurement name for a given data type
func (tm *TelemetryMapper) getMeasurementName(dataType miner_data_api.DataType) string {
	switch dataType {
	case miner_data_api.DataType_DATA_TYPE_MINER_HASHRATE_MH_S,
		miner_data_api.DataType_DATA_TYPE_HB_HASHRATE_MH_S,
		miner_data_api.DataType_DATA_TYPE_ASIC_HASHRATE_MH_S:
		return "hashrate_mhs"
	case miner_data_api.DataType_DATA_TYPE_MINER_TEMPERATURE_C,
		miner_data_api.DataType_DATA_TYPE_PSU_TEMPERATURE_C,
		miner_data_api.DataType_DATA_TYPE_HB_TEMPERATURE_C,
		miner_data_api.DataType_DATA_TYPE_ASIC_TEMPERATURE_C:
		return "temperature_c"
	case miner_data_api.DataType_DATA_TYPE_MINER_POWER_W,
		miner_data_api.DataType_DATA_TYPE_PSU_POWER_W,
		miner_data_api.DataType_DATA_TYPE_HB_POWER_W:
		return "power_w"
	case miner_data_api.DataType_DATA_TYPE_MINER_EFFICIENCY_J_TH,
		miner_data_api.DataType_DATA_TYPE_HB_EFFICIENCY_J_TH:
		return "efficiency_jth"
	case miner_data_api.DataType_DATA_TYPE_PSU_VOLTAGE_MV,
		miner_data_api.DataType_DATA_TYPE_HB_VOLTAGE_MV,
		miner_data_api.DataType_DATA_TYPE_ASIC_VOLTAGE_MV:
		return "voltage_mv"
	case miner_data_api.DataType_DATA_TYPE_PSU_CURRENT_MA,
		miner_data_api.DataType_DATA_TYPE_HB_CURRENT_MA:
		return "current_ma"
	case miner_data_api.DataType_DATA_TYPE_FAN_RPM:
		return "fan_rpm"
	case miner_data_api.DataType_DATA_TYPE_NONE:
		return "none"
	default:
		return fmt.Sprintf("unknown_%s", dataType.String())
	}
}

// getComponentInfo extracts component type and ID from ComponentId
func (tm *TelemetryMapper) getComponentInfo(componentID *miner_data_api.ComponentId) (string, string) {
	if componentID == nil {
		return "unknown", "unknown"
	}

	switch id := componentID.Id.(type) {
	case *miner_data_api.ComponentId_ComponentId:
		// Simple component ID (numeric)
		componentType, componentIDStr := tm.inferComponentTypeFromID(id.ComponentId)
		return componentType, componentIDStr
	case *miner_data_api.ComponentId_HashboardAsicId:
		// Hashboard/ASIC component
		if id.HashboardAsicId != nil {
			hashboardSN := id.HashboardAsicId.HashboardSn
			if id.HashboardAsicId.AsicIndex > 0 {
				asicIndex := strconv.FormatUint(uint64(id.HashboardAsicId.AsicIndex), 10)
				return "asic", fmt.Sprintf("hb_%s_asic_%s", hashboardSN, asicIndex)
			}
			return "hashboard", fmt.Sprintf("hb_%s", hashboardSN)
		}
		return "hashboard", "unknown"
	default:
		return "unknown", "unknown"
	}
}

// inferComponentTypeFromID infers the component type from the numeric component ID
func (tm *TelemetryMapper) inferComponentTypeFromID(componentID uint32) (string, string) {
	switch componentID {
	case 0:
		return "miner", "miner"
	case 1:
		return "psu", "psu"
	case 2:
		return "fan", "fan"
	default:
		return "component", strconv.FormatUint(uint64(componentID), 10)
	}
}
