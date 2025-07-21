package proto

import (
	"testing"
	"time"

	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_common_api"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_data_api"
)

func TestTelemetryMapper_MapToTimeSeriesRequests(t *testing.T) {
	mapper := NewTelemetryMapper("123")
	after := time.Now().Add(-1 * time.Hour)

	requests := mapper.MapToTimeSeriesRequests(after)

	if len(requests) == 0 {
		t.Errorf("expected requests but got none")
	}

	// Check that we have requests for different data types
	dataTypes := make(map[miner_data_api.DataType]bool)
	for _, req := range requests {
		dataTypes[req.DataType] = true

		// Verify time interval is set
		if req.TimeInterval == nil {
			t.Errorf("expected time interval but got nil")
		}

		// Verify component ID is set
		if req.ComponentId == nil {
			t.Errorf("expected component ID but got nil")
		}
	}

	// Check that we have miner-level metrics
	expectedTypes := []miner_data_api.DataType{
		miner_data_api.DataType_DATA_TYPE_MINER_HASHRATE_MH_S,
		miner_data_api.DataType_DATA_TYPE_MINER_TEMPERATURE_C,
		miner_data_api.DataType_DATA_TYPE_MINER_POWER_W,
		miner_data_api.DataType_DATA_TYPE_MINER_EFFICIENCY_J_TH,
		miner_data_api.DataType_DATA_TYPE_PSU_TEMPERATURE_C,
		miner_data_api.DataType_DATA_TYPE_FAN_RPM,
	}

	for _, expectedType := range expectedTypes {
		if !dataTypes[expectedType] {
			t.Errorf("missing expected data type: %v", expectedType)
		}
	}
}

func TestTelemetryMapper_MapToTelemetryModels(t *testing.T) {
	mapper := NewTelemetryMapper("123")

	// Create a mock response
	now := time.Now()
	response := &miner_data_api.TimeSeriesDataResponse{
		Result:   miner_common_api.ApiResult_RESULT_SUCCESS,
		DataType: miner_data_api.DataType_DATA_TYPE_MINER_HASHRATE_MH_S,
		ComponentId: &miner_data_api.ComponentId{
			Id: &miner_data_api.ComponentId_ComponentId{
				ComponentId: 0, // miner-level
			},
		},
		DataPoints: []*miner_data_api.TimeSeriesDataResponse_TimeSeriesDataPoint{
			{
				//nolint:gosec // No overflow risk here, just converting time to seconds and nanoseconds
				Timestamp: &miner_common_api.Timestamp{
					Seconds: uint64(now.Unix()),
					Nanos:   uint32(now.Nanosecond()),
				},
				Value: 100.5,
			},
		},
	}

	responses := []*miner_data_api.TimeSeriesDataResponse{response}
	telemetryData := mapper.MapToTelemetryModels(responses)

	if len(telemetryData) != 1 {
		t.Errorf("expected 1 telemetry entry but got %d", len(telemetryData))
	}

	if len(telemetryData) > 0 {
		entry := telemetryData[0]

		if entry.Measurement != "hashrate_mhs" {
			t.Errorf("expected measurement 'hashrate_mhs' but got '%s'", entry.Measurement)
		}

		if entry.Fields["value"] != 100.5 {
			t.Errorf("expected value 100.5 but got %v", entry.Fields["value"])
		}

		if entry.Tags["device_id"] != "123" {
			t.Errorf("expected device_id '123' but got '%s'", entry.Tags["device_id"])
		}

		if entry.Tags["component_type"] != "miner" {
			t.Errorf("expected component_type 'miner' but got '%s'", entry.Tags["component_type"])
		}
	}
}

func TestTelemetryMapper_GetMeasurementName(t *testing.T) {
	mapper := NewTelemetryMapper("123")

	tests := []struct {
		dataType     miner_data_api.DataType
		expectedName string
	}{
		{miner_data_api.DataType_DATA_TYPE_MINER_HASHRATE_MH_S, "hashrate_mhs"},
		{miner_data_api.DataType_DATA_TYPE_MINER_TEMPERATURE_C, "temperature_c"},
		{miner_data_api.DataType_DATA_TYPE_MINER_POWER_W, "power_w"},
		{miner_data_api.DataType_DATA_TYPE_PSU_VOLTAGE_MV, "voltage_mv"},
		{miner_data_api.DataType_DATA_TYPE_FAN_RPM, "fan_rpm"},
	}

	for _, tt := range tests {
		t.Run(tt.expectedName, func(t *testing.T) {
			name := mapper.getMeasurementName(tt.dataType)
			if name != tt.expectedName {
				t.Errorf("expected '%s' but got '%s'", tt.expectedName, name)
			}
		})
	}
}

func TestTelemetryMapper_GetComponentInfo(t *testing.T) {
	mapper := NewTelemetryMapper("123")

	tests := []struct {
		name              string
		componentID       *miner_data_api.ComponentId
		expectedType      string
		expectedComponent string
	}{
		{
			name: "miner component",
			componentID: &miner_data_api.ComponentId{
				Id: &miner_data_api.ComponentId_ComponentId{
					ComponentId: 0,
				},
			},
			expectedType:      "miner",
			expectedComponent: "miner",
		},
		{
			name: "psu component",
			componentID: &miner_data_api.ComponentId{
				Id: &miner_data_api.ComponentId_ComponentId{
					ComponentId: 1,
				},
			},
			expectedType:      "psu",
			expectedComponent: "psu",
		},
		{
			name: "fan component",
			componentID: &miner_data_api.ComponentId{
				Id: &miner_data_api.ComponentId_ComponentId{
					ComponentId: 2,
				},
			},
			expectedType:      "fan",
			expectedComponent: "fan",
		},
		{
			name:              "nil component",
			componentID:       nil,
			expectedType:      "unknown",
			expectedComponent: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compType, compID := mapper.getComponentInfo(tt.componentID)
			if compType != tt.expectedType {
				t.Errorf("expected type '%s' but got '%s'", tt.expectedType, compType)
			}
			if compID != tt.expectedComponent {
				t.Errorf("expected component '%s' but got '%s'", tt.expectedComponent, compID)
			}
		})
	}
}
