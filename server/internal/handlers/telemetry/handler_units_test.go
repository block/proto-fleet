package telemetry

import (
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	telemetryv1 "github.com/block/proto-fleet/server/generated/grpc/telemetry/v1"
	storesMocks "github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/domain/telemetry"
	mock "github.com/block/proto-fleet/server/internal/domain/telemetry/mocks"
	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
)

// Unit conversion test constants - raw storage values
const (
	// Raw hashrate: 100 TH/s = 100e12 H/s (storage unit)
	rawHashrateHS = 100e12
	// Expected display: 100 TH/s
	expectedHashrateTHs = 100.0

	// Raw power: 3 kW = 3000 W (storage unit)
	rawPowerW = 3000.0
	// Expected display: 3 kW
	expectedPowerKW = 3.0

	// Raw efficiency: 30 J/TH = 30e-12 J/H (storage unit)
	rawEfficiencyJH = 30e-12
	// Expected display: 30 J/TH
	expectedEfficiencyJTH = 30.0

	// Temperature passes through unchanged
	rawTempC      = 75.5
	expectedTempC = 75.5
)

// TestHandler_GetCombinedMetrics_UnitsConversion verifies that GetCombinedMetrics
// returns values in display units.
func TestHandler_GetCombinedMetrics_UnitsConversion(t *testing.T) {
	timestamp := time.Now()

	tests := []struct {
		name            string
		measurementType telemetryv1.MeasurementType
		rawValue        float64
		expectedValue   float64
	}{
		{
			name:            "combined hashrate converts from H/s to TH/s",
			measurementType: telemetryv1.MeasurementType_MEASUREMENT_TYPE_HASHRATE,
			rawValue:        rawHashrateHS,
			expectedValue:   expectedHashrateTHs,
		},
		{
			name:            "combined power converts from W to kW",
			measurementType: telemetryv1.MeasurementType_MEASUREMENT_TYPE_POWER,
			rawValue:        rawPowerW,
			expectedValue:   expectedPowerKW,
		},
		{
			name:            "combined efficiency converts from J/H to J/TH",
			measurementType: telemetryv1.MeasurementType_MEASUREMENT_TYPE_EFFICIENCY,
			rawValue:        rawEfficiencyJH,
			expectedValue:   expectedEfficiencyJTH,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			domainMeasurementType := protoToMeasurementTypeMap[tt.measurementType]

			mockStore := mock.NewMockTelemetryDataStore(ctrl)
			mockStore.EXPECT().GetCombinedMetrics(gomock.Any(), gomock.Any()).
				Return(models.CombinedMetric{
					Metrics: []models.Metric{
						{
							MeasurementType: domainMeasurementType,
							OpenTime:        timestamp,
							AggregatedValues: []models.AggregatedValue{
								{
									Type:  models.AggregationTypeSum,
									Value: tt.rawValue,
								},
							},
							DeviceCount: 5,
						},
					},
				}, nil)

			handler := createTestHandler(ctrl, mockStore)

			req := &telemetryv1.GetCombinedMetricsRequest{
				DeviceSelector: &telemetryv1.DeviceSelector{
					SelectorValue: &telemetryv1.DeviceSelector_DeviceList{
						DeviceList: &telemetryv1.DeviceList{
							DeviceIds: []string{"device1"},
						},
					},
				},
				MeasurementTypes: []telemetryv1.MeasurementType{tt.measurementType},
				Aggregations:     []telemetryv1.AggregationType{telemetryv1.AggregationType_AGGREGATION_TYPE_SUM},
			}

			resp, err := handler.GetCombinedMetrics(t.Context(), connect.NewRequest(req))

			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Len(t, resp.Msg.Metrics, 1)

			metric := resp.Msg.Metrics[0]
			assert.Equal(t, tt.measurementType, metric.MeasurementType)
			require.Len(t, metric.AggregatedValues, 1)
			assert.InDelta(t, tt.expectedValue, metric.AggregatedValues[0].Value, 1e-9,
				"expected value %v but got %v (raw was %v)",
				tt.expectedValue, metric.AggregatedValues[0].Value, tt.rawValue)
		})
	}
}

// TestHandler_StreamCombinedMetricUpdates_UnitsConversion verifies that
// StreamCombinedMetricUpdates returns values in display units (TH/s, kW, J/TH).
func TestHandler_StreamCombinedMetricUpdates_UnitsConversion(t *testing.T) {
	tests := []struct {
		name            string
		measurementType telemetryv1.MeasurementType
		rawValue        float64
		expectedValue   float64
	}{
		{
			name:            "streaming hashrate converts from H/s to TH/s",
			measurementType: telemetryv1.MeasurementType_MEASUREMENT_TYPE_HASHRATE,
			rawValue:        rawHashrateHS,
			expectedValue:   expectedHashrateTHs,
		},
		{
			name:            "streaming power converts from W to kW",
			measurementType: telemetryv1.MeasurementType_MEASUREMENT_TYPE_POWER,
			rawValue:        rawPowerW,
			expectedValue:   expectedPowerKW,
		},
		{
			name:            "streaming efficiency converts from J/H to J/TH",
			measurementType: telemetryv1.MeasurementType_MEASUREMENT_TYPE_EFFICIENCY,
			rawValue:        rawEfficiencyJH,
			expectedValue:   expectedEfficiencyJTH,
		},
		{
			name:            "streaming temperature passes through unchanged",
			measurementType: telemetryv1.MeasurementType_MEASUREMENT_TYPE_TEMPERATURE,
			rawValue:        rawTempC,
			expectedValue:   expectedTempC,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			// Arrange
			domainMeasurementType := protoToMeasurementTypeMap[tt.measurementType]
			handler := createTestHandler(ctrl, mock.NewMockTelemetryDataStore(ctrl))
			timestamp := time.Now()
			updateInterval := time.Minute

			combinedMetric := models.CombinedMetric{
				Metrics: []models.Metric{
					{
						MeasurementType: domainMeasurementType,
						OpenTime:        timestamp,
						AggregatedValues: []models.AggregatedValue{
							{Type: models.AggregationTypeAverage, Value: tt.rawValue},
							{Type: models.AggregationTypeMin, Value: tt.rawValue * 0.9},
							{Type: models.AggregationTypeMax, Value: tt.rawValue * 1.1},
							{Type: models.AggregationTypeSum, Value: tt.rawValue * 5},
						},
						DeviceCount: 5,
					},
				},
			}

			// Act
			resp, err := handler.convertCombinedMetricsToStreamResponse(combinedMetric, updateInterval)

			// Assert
			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Len(t, resp.Metrics, 1)

			metric := resp.Metrics[0]
			assert.Equal(t, tt.measurementType, metric.MeasurementType)
			require.Len(t, metric.AggregatedValues, 4)

			assert.InDelta(t, tt.expectedValue, metric.AggregatedValues[0].Value, 1e-9,
				"average: expected %v but got %v (raw was %v)",
				tt.expectedValue, metric.AggregatedValues[0].Value, tt.rawValue)
			assert.InDelta(t, tt.expectedValue*0.9, metric.AggregatedValues[1].Value, 1e-9,
				"min: expected %v but got %v", tt.expectedValue*0.9, metric.AggregatedValues[1].Value)
			assert.InDelta(t, tt.expectedValue*1.1, metric.AggregatedValues[2].Value, 1e-9,
				"max: expected %v but got %v", tt.expectedValue*1.1, metric.AggregatedValues[2].Value)
			assert.InDelta(t, tt.expectedValue*5, metric.AggregatedValues[3].Value, 1e-9,
				"sum: expected %v but got %v", tt.expectedValue*5, metric.AggregatedValues[3].Value)
		})
	}
}

// createTestHandler creates a handler with all required mocks for unit testing.
func createTestHandler(ctrl *gomock.Controller, mockStore *mock.MockTelemetryDataStore) *Handler {
	config := telemetry.Config{}
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
	mockErrorPoller := mock.NewMockErrorPoller(ctrl)

	service := telemetry.NewTelemetryService(config, mockStore, mockMinerGetter, mockScheduler, mockDeviceStore, mockErrorPoller, nil)

	return NewHandler(service)
}
