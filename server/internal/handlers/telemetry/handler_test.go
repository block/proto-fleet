package telemetry

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	telemetryv1 "github.com/block/proto-fleet/server/generated/grpc/telemetry/v1"
	storesMocks "github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/domain/telemetry"
	mock "github.com/block/proto-fleet/server/internal/domain/telemetry/mocks"
	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
)

var mockTime = time.Now()

func TestHandler_NewHandler(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mocks for all required dependencies
	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
	mockErrorPoller := mock.NewMockErrorPoller(ctrl)

	config := telemetry.Config{}
	service := telemetry.NewTelemetryService(config, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mockErrorPoller, nil)

	handler := NewHandler(service)

	require.NotNil(t, handler)
	require.Equal(t, service, handler.telemetryService)
}

func TestHandler_ConversionFunctions(t *testing.T) {
	// Test the conversion functions work correctly
	t.Run("measurement type conversion", func(t *testing.T) {
		protoType := telemetryv1.MeasurementType_MEASUREMENT_TYPE_TEMPERATURE
		domainType, err := measurementTypeToDomain(protoType)
		require.NoError(t, err)
		require.Equal(t, models.MeasurementTypeTemperature, domainType)

		//  back
		edProto, err := measurementTypeToProto(domainType)
		require.NoError(t, err)
		require.Equal(t, protoType, edProto)
	})

	// Legacy telemetry data conversion test removed - fromTelemetryData() no longer exists
	// Handler now works with DeviceMetrics v2 model only

	t.Run("aggregation type conversion", func(t *testing.T) {
		protoType := telemetryv1.AggregationType_AGGREGATION_TYPE_AVERAGE
		domainType, err := aggregationTypeToDomain(protoType)
		require.NoError(t, err)
		require.Equal(t, models.AggregationTypeAverage, domainType)

		//  back
		edProto, err := aggregationTypeToProto(domainType)
		require.NoError(t, err)
		require.Equal(t, protoType, edProto)
	})
}

func TestHandler_GetCombinedMetrics(t *testing.T) {
	tests := []struct {
		name             string
		request          *telemetryv1.GetCombinedMetricsRequest
		setupMocks       func(*mock.MockTelemetryDataStore)
		expectedError    bool
		errorContains    string
		validateResponse func(*telemetryv1.GetCombinedMetricsResponse)
	}{
		{
			name: "successful request with device list",
			request: &telemetryv1.GetCombinedMetricsRequest{
				DeviceSelector: &telemetryv1.DeviceSelector{
					SelectorValue: &telemetryv1.DeviceSelector_DeviceList{
						DeviceList: &telemetryv1.DeviceList{
							DeviceIds: []string{"device1", "device2"},
						},
					},
				},
				MeasurementTypes: []telemetryv1.MeasurementType{
					telemetryv1.MeasurementType_MEASUREMENT_TYPE_TEMPERATURE,
					telemetryv1.MeasurementType_MEASUREMENT_TYPE_HASHRATE,
				},
				Aggregations: []telemetryv1.AggregationType{
					telemetryv1.AggregationType_AGGREGATION_TYPE_AVERAGE,
					telemetryv1.AggregationType_AGGREGATION_TYPE_MAX,
				},
				StartTime:   timestamppb.New(mockTime.Add(-time.Hour)),
				EndTime:     timestamppb.New(mockTime),
				Granularity: durationpb.New(time.Minute * 5),
				PageSize:    50,
			},
			setupMocks: func(mockStore *mock.MockTelemetryDataStore) {
				mockStore.EXPECT().GetCombinedMetrics(gomock.Any(), gomock.Any()).
					Return(models.CombinedMetric{
						Metrics: []models.Metric{
							{
								MeasurementType: models.MeasurementTypeTemperature,
								OpenTime:        mockTime.Add(-time.Minute * 5),
								AggregatedValues: []models.AggregatedValue{
									{Type: models.AggregationTypeAverage, Value: 67.5},
									{Type: models.AggregationTypeMax, Value: 72.0},
								},
							},
							{
								MeasurementType: models.MeasurementTypeHashrate,
								OpenTime:        mockTime.Add(-time.Minute * 5),
								AggregatedValues: []models.AggregatedValue{
									{Type: models.AggregationTypeAverage, Value: 100.5},
									{Type: models.AggregationTypeMax, Value: 105.0},
								},
							},
						},
						NextPageToken: "next_page_123",
					}, nil)
			},
			expectedError: false,
			validateResponse: func(resp *telemetryv1.GetCombinedMetricsResponse) {
				require.NotNil(t, resp)
				require.Len(t, resp.Metrics, 2)
				require.Equal(t, "next_page_123", resp.NextPageToken)

				// Check first metric (temperature)
				require.Equal(t, telemetryv1.MeasurementType_MEASUREMENT_TYPE_TEMPERATURE, resp.Metrics[0].MeasurementType)
				require.Len(t, resp.Metrics[0].AggregatedValues, 2)
				require.InDelta(t, 67.5, resp.Metrics[0].AggregatedValues[0].Value, 0.001)
				require.InDelta(t, 72.0, resp.Metrics[0].AggregatedValues[1].Value, 0.001)

				// Check second metric (hashrate)
				require.Equal(t, telemetryv1.MeasurementType_MEASUREMENT_TYPE_HASHRATE, resp.Metrics[1].MeasurementType)
				require.Len(t, resp.Metrics[1].AggregatedValues, 2)
				require.InDelta(t, 100.5/1e6, resp.Metrics[1].AggregatedValues[0].Value, 0.001)
				require.InDelta(t, 105.0/1e6, resp.Metrics[1].AggregatedValues[1].Value, 0.001)
			},
		},
		{
			name: "successful request with all devices",
			request: &telemetryv1.GetCombinedMetricsRequest{
				DeviceSelector: &telemetryv1.DeviceSelector{
					SelectorValue: &telemetryv1.DeviceSelector_AllDevices{
						AllDevices: true,
					},
				},
				MeasurementTypes: []telemetryv1.MeasurementType{
					telemetryv1.MeasurementType_MEASUREMENT_TYPE_POWER,
				},
				Aggregations: []telemetryv1.AggregationType{
					telemetryv1.AggregationType_AGGREGATION_TYPE_AVERAGE,
				},
				StartTime: timestamppb.New(mockTime.Add(-time.Hour)),
				EndTime:   timestamppb.New(mockTime),
			},
			setupMocks: func(mockStore *mock.MockTelemetryDataStore) {
				mockStore.EXPECT().GetCombinedMetrics(gomock.Any(), gomock.Any()).
					Return(models.CombinedMetric{
						Metrics: []models.Metric{
							{
								MeasurementType: models.MeasurementTypePower,
								OpenTime:        mockTime.Add(-time.Minute * 10),
								AggregatedValues: []models.AggregatedValue{
									{Type: models.AggregationTypeAverage, Value: 1250.0},
								},
							},
						},
						NextPageToken: "",
					}, nil)
			},
			expectedError: false,
			validateResponse: func(resp *telemetryv1.GetCombinedMetricsResponse) {
				require.NotNil(t, resp)
				require.Len(t, resp.Metrics, 1)
				require.Empty(t, resp.NextPageToken)
				require.Equal(t, telemetryv1.MeasurementType_MEASUREMENT_TYPE_POWER, resp.Metrics[0].MeasurementType)
				require.InDelta(t, 1.2500, resp.Metrics[0].AggregatedValues[0].Value, 0.001)
			},
		},
		{
			name: "service error",
			request: &telemetryv1.GetCombinedMetricsRequest{
				DeviceSelector: &telemetryv1.DeviceSelector{
					SelectorValue: &telemetryv1.DeviceSelector_DeviceList{
						DeviceList: &telemetryv1.DeviceList{
							DeviceIds: []string{"device1"},
						},
					},
				},
				MeasurementTypes: []telemetryv1.MeasurementType{
					telemetryv1.MeasurementType_MEASUREMENT_TYPE_TEMPERATURE,
				},
				Aggregations: []telemetryv1.AggregationType{
					telemetryv1.AggregationType_AGGREGATION_TYPE_AVERAGE,
				},
				StartTime: timestamppb.New(mockTime.Add(-time.Hour)),
				EndTime:   timestamppb.New(mockTime),
			},
			setupMocks: func(mockStore *mock.MockTelemetryDataStore) {
				mockStore.EXPECT().GetCombinedMetrics(gomock.Any(), gomock.Any()).
					Return(models.CombinedMetric{}, errors.New("combined metrics error"))
			},
			expectedError: true,
			errorContains: "combined metrics error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockStore := mock.NewMockTelemetryDataStore(ctrl)
			tt.setupMocks(mockStore)

			config := telemetry.Config{}
			mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
			mockScheduler := mock.NewMockUpdateScheduler(ctrl)
			mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
			mockErrorPoller := mock.NewMockErrorPoller(ctrl)

			service := telemetry.NewTelemetryService(config, mockStore, mockMinerGetter, mockScheduler, mockDeviceStore, mockErrorPoller, nil)
			handler := NewHandler(service)

			resp, err := handler.GetCombinedMetrics(t.Context(), connect.NewRequest(tt.request))

			if tt.expectedError {
				require.Error(t, err, "Expected error but got none - this indicates a bug!")
				require.Contains(t, err.Error(), tt.errorContains)
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)

				if tt.validateResponse != nil {
					tt.validateResponse(resp.Msg)
				}
			}
		})
	}
}

func TestHandler_StreamCombinedMetricUpdates_ConversionFunction(t *testing.T) {
	// Test the conversion function for streaming requests
	t.Run("successful conversion", func(t *testing.T) {
		req := &telemetryv1.StreamCombinedMetricUpdatesRequest{
			DeviceSelector: &telemetryv1.DeviceSelector{
				SelectorValue: &telemetryv1.DeviceSelector_DeviceList{
					DeviceList: &telemetryv1.DeviceList{
						DeviceIds: []string{"device1", "device2"},
					},
				},
			},
			Metrics: []telemetryv1.MeasurementType{
				telemetryv1.MeasurementType_MEASUREMENT_TYPE_TEMPERATURE,
				telemetryv1.MeasurementType_MEASUREMENT_TYPE_HASHRATE,
			},
			Aggregations: []telemetryv1.AggregationType{
				telemetryv1.AggregationType_AGGREGATION_TYPE_AVERAGE,
				telemetryv1.AggregationType_AGGREGATION_TYPE_MAX,
			},
			Granularity:    durationpb.New(time.Minute * 5),
			UpdateInterval: durationpb.New(time.Minute * 10),
		}

		query, err := toStreamCombinedMetricsQuery(req)
		require.NoError(t, err)

		// Verify device IDs
		require.Len(t, query.DeviceIDs, 2)
		require.Equal(t, models.DeviceIdentifier("device1"), query.DeviceIDs[0])
		require.Equal(t, models.DeviceIdentifier("device2"), query.DeviceIDs[1])

		// Verify measurement types
		require.Len(t, query.MeasurementTypes, 2)
		require.Equal(t, models.MeasurementTypeTemperature, query.MeasurementTypes[0])
		require.Equal(t, models.MeasurementTypeHashrate, query.MeasurementTypes[1])

		// Verify aggregation types
		require.Len(t, query.AggregationTypes, 2)
		require.Equal(t, models.AggregationTypeAverage, query.AggregationTypes[0])
		require.Equal(t, models.AggregationTypeMax, query.AggregationTypes[1])

		// Verify timing
		require.Equal(t, time.Minute*5, query.Granularity)
		require.Equal(t, time.Minute*10, query.UpdateInterval)
	})

	t.Run("default values", func(t *testing.T) {
		req := &telemetryv1.StreamCombinedMetricUpdatesRequest{
			DeviceSelector: &telemetryv1.DeviceSelector{
				SelectorValue: &telemetryv1.DeviceSelector_DeviceList{
					DeviceList: &telemetryv1.DeviceList{
						DeviceIds: []string{"device1"},
					},
				},
			},
			Metrics: []telemetryv1.MeasurementType{
				telemetryv1.MeasurementType_MEASUREMENT_TYPE_TEMPERATURE,
			},
			Aggregations: []telemetryv1.AggregationType{
				telemetryv1.AggregationType_AGGREGATION_TYPE_AVERAGE,
			},
			// No granularity or update interval specified
		}

		query, err := toStreamCombinedMetricsQuery(req)
		require.NoError(t, err)

		// Should use default values
		require.Equal(t, time.Minute, query.Granularity)    // Default 1 minute
		require.Equal(t, time.Minute, query.UpdateInterval) // Default to granularity
	})

	t.Run("all devices selector", func(t *testing.T) {
		req := &telemetryv1.StreamCombinedMetricUpdatesRequest{
			DeviceSelector: &telemetryv1.DeviceSelector{
				SelectorValue: &telemetryv1.DeviceSelector_AllDevices{
					AllDevices: true,
				},
			},
			Metrics: []telemetryv1.MeasurementType{
				telemetryv1.MeasurementType_MEASUREMENT_TYPE_TEMPERATURE,
			},
			Aggregations: []telemetryv1.AggregationType{
				telemetryv1.AggregationType_AGGREGATION_TYPE_AVERAGE,
			},
			Granularity:    durationpb.New(time.Minute * 2),
			UpdateInterval: durationpb.New(time.Minute * 3),
		}

		query, err := toStreamCombinedMetricsQuery(req)
		require.NoError(t, err)

		// Verify empty device list for all devices
		require.Empty(t, query.DeviceIDs)

		// Verify measurement types
		require.Len(t, query.MeasurementTypes, 1)
		require.Equal(t, models.MeasurementTypeTemperature, query.MeasurementTypes[0])

		// Verify aggregation types
		require.Len(t, query.AggregationTypes, 1)
		require.Equal(t, models.AggregationTypeAverage, query.AggregationTypes[0])

		// Verify timing
		require.Equal(t, time.Minute*2, query.Granularity)
		require.Equal(t, time.Minute*3, query.UpdateInterval)
	})
}

// Test the core streaming logic by testing the StreamCombinedMetrics method
func TestHandler_StreamCombinedMetricUpdates_Integration(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockStore := mock.NewMockTelemetryDataStore(ctrl)

	// Set up the mock to return data when GetCombinedMetrics is called
	mockStore.EXPECT().GetCombinedMetrics(gomock.Any(), gomock.Any()).
		Return(models.CombinedMetric{
			Metrics: []models.Metric{
				{
					MeasurementType: models.MeasurementTypeTemperature,
					OpenTime:        mockTime.Add(-time.Minute * 5),
					AggregatedValues: []models.AggregatedValue{
						{Type: models.AggregationTypeAverage, Value: 67.5},
					},
				},
			},
			NextPageToken: "",
		}, nil).AnyTimes()

	config := telemetry.Config{}
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
	mockErrorPoller := mock.NewMockErrorPoller(ctrl)

	// Set up the mock to return miner state counts
	mockDeviceStore.EXPECT().GetMinerStateCounts(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(&telemetryv1.MinerStateCounts{
			HashingCount:  10,
			BrokenCount:   2,
			OfflineCount:  3,
			SleepingCount: 1,
		}, nil).AnyTimes()

	service := telemetry.NewTelemetryService(config, mockStore, mockMinerGetter, mockScheduler, mockDeviceStore, mockErrorPoller, nil)

	// Test the streaming functionality
	query := models.StreamCombinedMetricsQuery{
		DeviceIDs: []models.DeviceIdentifier{"device1"},
		MeasurementTypes: []models.MeasurementType{
			models.MeasurementTypeTemperature,
		},
		AggregationTypes: []models.AggregationType{
			models.AggregationTypeAverage,
		},
		Granularity:    time.Minute * 5,
		UpdateInterval: time.Millisecond * 100, // Fast interval for testing
	}

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	// Start the stream
	updateChan, err := service.StreamCombinedMetrics(ctx, query)
	require.NoError(t, err)
	require.NotNil(t, updateChan)

	// We should receive at least one update
	select {
	case update, ok := <-updateChan:
		require.True(t, ok, "Channel should not be closed immediately")
		require.Len(t, update.Metrics, 1)
		require.Equal(t, models.MeasurementTypeTemperature, update.Metrics[0].MeasurementType)
		require.InDelta(t, 67.5, update.Metrics[0].AggregatedValues[0].Value, 0.001)
	case <-time.After(time.Millisecond * 200):
		t.Fatal("Should have received an update within 200ms")
	}

	// Cancel context and verify channel closes
	cancel()

	// Wait for channel to close
	select {
	case _, ok := <-updateChan:
		if ok {
			// Got another update, that's fine, but eventually channel should close
			select {
			case _, ok := <-updateChan:
				require.False(t, ok, "Channel should be closed after context cancellation")
			case <-time.After(time.Second):
				t.Fatal("Channel should have been closed")
			}
		}
		// Channel is closed, which is what we expect
	case <-time.After(time.Second):
		t.Fatal("Channel should have been closed after context cancellation")
	}
}
