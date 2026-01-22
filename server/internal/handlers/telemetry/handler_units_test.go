package telemetry

import (
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	commonv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/common/v1"
	telemetryv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/telemetry/v1"
	storesMocks "github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/btc-mining/proto-fleet/server/internal/domain/telemetry"
	mock "github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/mocks"
	"github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"
	modelsV2 "github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models/v2"
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

// TestHandler_GetSnapshot_UnitsConversion verifies that GetSnapshot returns
// telemetry values in the correct display units (TH/s, kW, J/TH).
func TestHandler_GetSnapshot_UnitsConversion(t *testing.T) {
	timestamp := time.Now()

	tests := []struct {
		name            string
		measurementType telemetryv1.MeasurementType
		deviceMetrics   modelsV2.DeviceMetrics
		expectedValue   float64
		expectedUnit    commonv1.MeasurementUnit
	}{
		{
			name:            "hashrate converts from H/s to TH/s",
			measurementType: telemetryv1.MeasurementType_MEASUREMENT_TYPE_HASHRATE,
			deviceMetrics: modelsV2.DeviceMetrics{
				DeviceID:   "device1",
				Timestamp:  timestamp,
				HashrateHS: &modelsV2.MetricValue{Value: rawHashrateHS},
			},
			expectedValue: expectedHashrateTHs,
			expectedUnit:  commonv1.MeasurementUnit_MEASUREMENT_UNIT_TERAHASH_PER_SECOND,
		},
		{
			name:            "power converts from W to kW",
			measurementType: telemetryv1.MeasurementType_MEASUREMENT_TYPE_POWER,
			deviceMetrics: modelsV2.DeviceMetrics{
				DeviceID:  "device1",
				Timestamp: timestamp,
				PowerW:    &modelsV2.MetricValue{Value: rawPowerW},
			},
			expectedValue: expectedPowerKW,
			expectedUnit:  commonv1.MeasurementUnit_MEASUREMENT_UNIT_KILOWATT,
		},
		{
			name:            "efficiency converts from J/H to J/TH",
			measurementType: telemetryv1.MeasurementType_MEASUREMENT_TYPE_EFFICIENCY,
			deviceMetrics: modelsV2.DeviceMetrics{
				DeviceID:     "device1",
				Timestamp:    timestamp,
				EfficiencyJH: &modelsV2.MetricValue{Value: rawEfficiencyJH},
			},
			expectedValue: expectedEfficiencyJTH,
			expectedUnit:  commonv1.MeasurementUnit_MEASUREMENT_UNIT_JOULES_PER_TERAHASH,
		},
		{
			name:            "temperature passes through unchanged",
			measurementType: telemetryv1.MeasurementType_MEASUREMENT_TYPE_TEMPERATURE,
			deviceMetrics: modelsV2.DeviceMetrics{
				DeviceID:  "device1",
				Timestamp: timestamp,
				TempC:     &modelsV2.MetricValue{Value: rawTempC},
			},
			expectedValue: expectedTempC,
			expectedUnit:  commonv1.MeasurementUnit_MEASUREMENT_UNIT_CELSIUS,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockStore := mock.NewMockTelemetryDataStore(ctrl)
			mockStore.EXPECT().GetLatestDeviceMetricsBatch(gomock.Any(), gomock.Any()).
				Return(map[models.DeviceIdentifier]modelsV2.DeviceMetrics{
					"device1": tt.deviceMetrics,
				}, nil)

			handler := createTestHandler(ctrl, mockStore)

			req := &telemetryv1.GetSnapshotRequest{
				DeviceIds:        []string{"device1"},
				MeasurementTypes: []telemetryv1.MeasurementType{tt.measurementType},
			}

			resp, err := handler.GetSnapshot(t.Context(), connect.NewRequest(req))

			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Len(t, resp.Msg.Telemetry, 1)

			telemetryData := resp.Msg.Telemetry[0]
			assert.Equal(t, "device1", telemetryData.DeviceId)
			assert.Equal(t, tt.measurementType, telemetryData.MeasurementType)
			assert.InDelta(t, tt.expectedValue, telemetryData.Value, 1e-9,
				"expected value %v but got %v (raw was %v)",
				tt.expectedValue, telemetryData.Value, tt.deviceMetrics)
			assert.Equal(t, tt.expectedUnit, telemetryData.Unit)
		})
	}
}

// TestHandler_GetAggregated_UnitsConversion verifies that GetAggregatedSnapshot
// returns aggregated values in display units.
func TestHandler_GetAggregated_UnitsConversion(t *testing.T) {
	timestamp := time.Now()
	startTime := timestamp.Add(-time.Hour)
	endTime := timestamp

	tests := []struct {
		name            string
		measurementType telemetryv1.MeasurementType
		rawValue        float64
		expectedValue   float64
	}{
		{
			name:            "aggregated hashrate converts from H/s to TH/s",
			measurementType: telemetryv1.MeasurementType_MEASUREMENT_TYPE_HASHRATE,
			rawValue:        rawHashrateHS,
			expectedValue:   expectedHashrateTHs,
		},
		{
			name:            "aggregated power converts from W to kW",
			measurementType: telemetryv1.MeasurementType_MEASUREMENT_TYPE_POWER,
			rawValue:        rawPowerW,
			expectedValue:   expectedPowerKW,
		},
		{
			name:            "aggregated efficiency converts from J/H to J/TH",
			measurementType: telemetryv1.MeasurementType_MEASUREMENT_TYPE_EFFICIENCY,
			rawValue:        rawEfficiencyJH,
			expectedValue:   expectedEfficiencyJTH,
		},
		{
			name:            "aggregated temperature passes through unchanged",
			measurementType: telemetryv1.MeasurementType_MEASUREMENT_TYPE_TEMPERATURE,
			rawValue:        rawTempC,
			expectedValue:   expectedTempC,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			domainMeasurementType := protoToMeasurementTypeMap[tt.measurementType]

			mockStore := mock.NewMockTelemetryDataStore(ctrl)
			mockStore.EXPECT().GetAggregatedTelemetry(gomock.Any(), gomock.Any()).
				Return([]models.AggregatedTelemetry{
					{
						DeviceID:        "device1",
						MeasurementType: domainMeasurementType,
						Value:           tt.rawValue,
						AggregationType: models.AggregationTypeAverage,
						DataPoints:      10,
						TimeWindow: models.TimeWindow{
							StartTime: startTime,
							EndTime:   endTime,
						},
					},
				}, nil)

			handler := createTestHandler(ctrl, mockStore)

			req := &telemetryv1.GetAggregatedSnapshotRequest{
				DeviceIds:        []string{"device1"},
				MeasurementTypes: []telemetryv1.MeasurementType{tt.measurementType},
				AggregationType:  telemetryv1.AggregationType_AGGREGATION_TYPE_AVERAGE,
			}

			resp, err := handler.GetAggregatedSnapshot(t.Context(), connect.NewRequest(req))

			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Len(t, resp.Msg.AggregatedData, 1)

			aggregated := resp.Msg.AggregatedData[0]
			assert.Equal(t, "device1", aggregated.DeviceId)
			assert.Equal(t, tt.measurementType, aggregated.MeasurementType)
			assert.InDelta(t, tt.expectedValue, aggregated.Value, 1e-9,
				"expected value %v but got %v (raw was %v)",
				tt.expectedValue, aggregated.Value, tt.rawValue)
		})
	}
}

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

// createTestHandler creates a handler with all required mocks for unit testing.
func createTestHandler(ctrl *gomock.Controller, mockStore *mock.MockTelemetryDataStore) *Handler {
	config := telemetry.Config{}
	mockMinerGetter := mock.NewMockMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
	mockErrorPoller := mock.NewMockErrorPoller(ctrl)

	service := telemetry.NewTelemetryService(config, mockStore, mockMinerGetter, mockScheduler, mockDeviceStore, mockErrorPoller)
	return NewHandler(service)
}
