package telemetry

import (
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	telemetryv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/telemetry/v1"
	storesMocks "github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/btc-mining/proto-fleet/server/internal/domain/telemetry"
	mock "github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/mocks"
	"github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"
)

var mockTime = time.Now()

func TestHandler_NewHandler(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mocks for all required dependencies
	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

	config := telemetry.Config{}
	service := telemetry.NewTelemetryService(config, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore)

	handler := NewHandler(service)

	require.NotNil(t, handler)
	require.Equal(t, service, handler.telemetryService)
}

func TestHandler_GetSnapshot(t *testing.T) {
	tests := []struct {
		name             string
		request          *telemetryv1.GetSnapshotRequest
		setupMocks       func(*mock.MockTelemetryDataStore)
		expectedError    bool
		errorContains    string
		validateResponse func(*telemetryv1.GetSnapshotResponse)
	}{
		{
			name: "successful request",
			request: &telemetryv1.GetSnapshotRequest{
				DeviceIds: []string{"device1", "device2"},
				MeasurementTypes: []telemetryv1.MeasurementType{
					telemetryv1.MeasurementType_MEASUREMENT_TYPE_TEMPERATURE,
				},
			},
			setupMocks: func(mockStore *mock.MockTelemetryDataStore) {
				mockStore.EXPECT().GetLatestTelemetry(gomock.Any(), gomock.Any()).
					Return([]models.Telemetry{
						{
							Measurement: "temperature",
							Fields:      map[string]any{"value": 65.5},
							Tags:        map[string]string{"device_id": "device1"},
							Timestamp:   mockTime,
						},
					}, nil)
			},
			expectedError: false,
			validateResponse: func(resp *telemetryv1.GetSnapshotResponse) {
				require.NotNil(t, resp)
				require.Len(t, resp.Telemetry, 1)
				require.Equal(t, "device1", resp.Telemetry[0].DeviceId)
			},
		},
		{
			name: "service error",
			request: &telemetryv1.GetSnapshotRequest{
				DeviceIds: []string{"device1"},
				MeasurementTypes: []telemetryv1.MeasurementType{
					telemetryv1.MeasurementType_MEASUREMENT_TYPE_HASHRATE,
				},
			},
			setupMocks: func(mockStore *mock.MockTelemetryDataStore) {
				mockStore.EXPECT().GetLatestTelemetry(gomock.Any(), gomock.Any()).
					Return(nil, errors.New("service error"))
			},
			expectedError: true,
			errorContains: "service error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockStore := mock.NewMockTelemetryDataStore(ctrl)
			tt.setupMocks(mockStore)

			config := telemetry.Config{}
			mockMinerGetter := mock.NewMockMinerGetter(ctrl)
			mockScheduler := mock.NewMockUpdateScheduler(ctrl)
			mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

			service := telemetry.NewTelemetryService(config, mockStore, mockMinerGetter, mockScheduler, mockDeviceStore)
			handler := NewHandler(service)

			resp, err := handler.GetSnapshot(t.Context(), connect.NewRequest(tt.request))

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

func TestHandler_GetTimeSeries(t *testing.T) {
	tests := []struct {
		name             string
		request          *telemetryv1.GetTimeSeriesRequest
		setupMocks       func(*mock.MockTelemetryDataStore)
		expectedError    bool
		errorContains    string
		validateResponse func(*telemetryv1.GetTimeSeriesResponse)
	}{
		{
			name: "successful request",
			request: &telemetryv1.GetTimeSeriesRequest{
				DeviceIds: []string{"device1"},
				MeasurementTypes: []telemetryv1.MeasurementType{
					telemetryv1.MeasurementType_MEASUREMENT_TYPE_POWER,
				},
				TimeRange: &telemetryv1.TimeRange{
					StartTime: timestamppb.New(mockTime.Add(-time.Hour)),
					EndTime:   timestamppb.New(mockTime),
				},
			},
			setupMocks: func(mockStore *mock.MockTelemetryDataStore) {
				mockStore.EXPECT().GetTimeSeriesTelemetry(gomock.Any(), gomock.Any()).
					Return([]models.Telemetry{
						{
							Measurement: "power",
							Fields:      map[string]any{"value": 1200.0},
							Tags:        map[string]string{"device_id": "device1"},
							Timestamp:   mockTime,
						},
					}, nil)
			},
			expectedError: false,
			validateResponse: func(resp *telemetryv1.GetTimeSeriesResponse) {
				require.NotNil(t, resp)
				require.Len(t, resp.Telemetry, 1)
				require.Equal(t, "device1", resp.Telemetry[0].DeviceId)
			},
		},
		{
			name: "service error",
			request: &telemetryv1.GetTimeSeriesRequest{
				DeviceIds: []string{"device1"},
				MeasurementTypes: []telemetryv1.MeasurementType{
					telemetryv1.MeasurementType_MEASUREMENT_TYPE_POWER,
				},
				TimeRange: &telemetryv1.TimeRange{
					StartTime: timestamppb.New(mockTime.Add(-time.Hour)),
					EndTime:   timestamppb.New(mockTime),
				},
			},
			setupMocks: func(mockStore *mock.MockTelemetryDataStore) {
				mockStore.EXPECT().GetTimeSeriesTelemetry(gomock.Any(), gomock.Any()).
					Return(nil, errors.New("database error"))
			},
			expectedError: true,
			errorContains: "database error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockStore := mock.NewMockTelemetryDataStore(ctrl)
			tt.setupMocks(mockStore)

			config := telemetry.Config{}
			mockMinerGetter := mock.NewMockMinerGetter(ctrl)
			mockScheduler := mock.NewMockUpdateScheduler(ctrl)
			mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

			service := telemetry.NewTelemetryService(config, mockStore, mockMinerGetter, mockScheduler, mockDeviceStore)
			handler := NewHandler(service)

			resp, err := handler.GetTimeSeries(t.Context(), connect.NewRequest(tt.request))

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

func TestHandler_GetMetadata(t *testing.T) {
	tests := []struct {
		name             string
		request          *telemetryv1.GetMetadataRequest
		setupMocks       func(*mock.MockTelemetryDataStore)
		expectedError    bool
		errorContains    string
		validateResponse func(*telemetryv1.GetMetadataResponse)
	}{
		{
			name: "successful request",
			request: &telemetryv1.GetMetadataRequest{
				DeviceIds: []string{"device1"},
			},
			setupMocks: func(mockStore *mock.MockTelemetryDataStore) {
				mockStore.EXPECT().GetTelemetryMetadata(gomock.Any(), gomock.Any()).
					Return([]models.DeviceMetadata{
						{
							DeviceID:     models.DeviceIdentifier("device1"),
							DeviceType:   "antminer",
							LastSeen:     mockTime,
							Status:       models.ComponentStatusHealthy,
							Location:     "datacenter1",
							Tags:         map[string]string{"pool": "pool1"},
							Capabilities: []string{"temperature", "hashrate"},
						},
					}, nil)
			},
			expectedError: false,
			validateResponse: func(resp *telemetryv1.GetMetadataResponse) {
				require.NotNil(t, resp)
				require.Len(t, resp.Devices, 1)
				require.Equal(t, "device1", resp.Devices[0].DeviceId)
				require.Equal(t, "antminer", *resp.Devices[0].DeviceType)
			},
		},
		{
			name: "service error",
			request: &telemetryv1.GetMetadataRequest{
				DeviceIds: []string{"device1"},
			},
			setupMocks: func(mockStore *mock.MockTelemetryDataStore) {
				mockStore.EXPECT().GetTelemetryMetadata(gomock.Any(), gomock.Any()).
					Return(nil, errors.New("metadata error"))
			},
			expectedError: true,
			errorContains: "metadata error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockStore := mock.NewMockTelemetryDataStore(ctrl)
			tt.setupMocks(mockStore)

			config := telemetry.Config{}
			mockMinerGetter := mock.NewMockMinerGetter(ctrl)
			mockScheduler := mock.NewMockUpdateScheduler(ctrl)
			mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

			service := telemetry.NewTelemetryService(config, mockStore, mockMinerGetter, mockScheduler, mockDeviceStore)
			handler := NewHandler(service)

			resp, err := handler.GetMetadata(t.Context(), connect.NewRequest(tt.request))

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

func TestHandler_GetAggregated(t *testing.T) {
	tests := []struct {
		name             string
		request          *telemetryv1.GetAggregatedSnapshotRequest
		setupMocks       func(*mock.MockTelemetryDataStore)
		expectedError    bool
		errorContains    string
		validateResponse func(*telemetryv1.GetAggregatedSnapshotResponse)
	}{
		{
			name: "successful request",
			request: &telemetryv1.GetAggregatedSnapshotRequest{
				DeviceIds: []string{"device1"},
				MeasurementTypes: []telemetryv1.MeasurementType{
					telemetryv1.MeasurementType_MEASUREMENT_TYPE_TEMPERATURE,
				},
				TimeRange: &telemetryv1.TimeRange{
					StartTime: timestamppb.New(mockTime.Add(-time.Hour)),
					EndTime:   timestamppb.New(mockTime),
				},
				AggregationType: telemetryv1.AggregationType_AGGREGATION_TYPE_AVERAGE,
			},
			setupMocks: func(mockStore *mock.MockTelemetryDataStore) {
				mockStore.EXPECT().GetAggregatedTelemetry(gomock.Any(), gomock.Any()).
					Return([]models.AggregatedTelemetry{
						{
							DeviceID:        models.DeviceIdentifier("device1"),
							MeasurementType: models.MeasurementTypeTemperature,
							Value:           67.5,
							AggregationType: models.AggregationTypeAverage,
							DataPoints:      10,
							TimeWindow: models.TimeWindow{
								StartTime: mockTime.Add(-time.Hour),
								EndTime:   mockTime,
							},
							Tags: map[string]string{"pool": "pool1"},
						},
					}, nil)
			},
			expectedError: false,
			validateResponse: func(resp *telemetryv1.GetAggregatedSnapshotResponse) {
				require.NotNil(t, resp)
				require.Len(t, resp.AggregatedData, 1)
				require.Equal(t, "device1", resp.AggregatedData[0].DeviceId)
				require.InDelta(t, 67.5, resp.AggregatedData[0].Value, 0.001)
			},
		},
		{
			name: "service error",
			request: &telemetryv1.GetAggregatedSnapshotRequest{
				DeviceIds: []string{"device1"},
				MeasurementTypes: []telemetryv1.MeasurementType{
					telemetryv1.MeasurementType_MEASUREMENT_TYPE_TEMPERATURE,
				},
				TimeRange: &telemetryv1.TimeRange{
					StartTime: timestamppb.New(mockTime.Add(-time.Hour)),
					EndTime:   timestamppb.New(mockTime),
				},
				AggregationType: telemetryv1.AggregationType_AGGREGATION_TYPE_AVERAGE,
			},
			setupMocks: func(mockStore *mock.MockTelemetryDataStore) {
				mockStore.EXPECT().GetAggregatedTelemetry(gomock.Any(), gomock.Any()).
					Return(nil, errors.New("aggregation error"))
			},
			expectedError: true,
			errorContains: "aggregation error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockStore := mock.NewMockTelemetryDataStore(ctrl)
			tt.setupMocks(mockStore)

			config := telemetry.Config{}
			mockMinerGetter := mock.NewMockMinerGetter(ctrl)
			mockScheduler := mock.NewMockUpdateScheduler(ctrl)
			mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

			service := telemetry.NewTelemetryService(config, mockStore, mockMinerGetter, mockScheduler, mockDeviceStore)
			handler := NewHandler(service)

			resp, err := handler.GetAggregatedSnapshot(t.Context(), connect.NewRequest(tt.request))

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

	t.Run("telemetry data conversion", func(t *testing.T) {
		telemetryData := []models.Telemetry{
			{
				Measurement: "temperature",
				Fields:      map[string]any{"value": 65.5},
				Tags:        map[string]string{"device_id": "device1"},
				Timestamp:   mockTime,
			},
		}

		protoData, err := fromTelemetryData(telemetryData)
		require.NoError(t, err)
		require.Len(t, protoData, 1)
		require.Equal(t, "device1", protoData[0].DeviceId)
		require.Equal(t, telemetryv1.MeasurementType_MEASUREMENT_TYPE_TEMPERATURE, protoData[0].MeasurementType)
		require.InDelta(t, 65.5, protoData[0].Value, 0.001)
	})

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

	t.Run("component status conversion", func(t *testing.T) {
		protoStatus := telemetryv1.ComponentStatus_COMPONENT_STATUS_HEALTHY
		domainStatus, err := componentStatusToDomain(protoStatus)
		require.NoError(t, err)
		require.Equal(t, models.ComponentStatusHealthy, domainStatus)

		//  back
		edProto, err := componentStatusToProto(domainStatus)
		require.NoError(t, err)
		require.Equal(t, protoStatus, edProto)
	})

	t.Run("time range conversion", func(t *testing.T) {
		startTime := mockTime.Add(-time.Hour)
		endTime := mockTime

		req := &telemetryv1.GetTimeSeriesRequest{
			DeviceIds: []string{"device1"},
			MeasurementTypes: []telemetryv1.MeasurementType{
				telemetryv1.MeasurementType_MEASUREMENT_TYPE_TEMPERATURE,
			},
			TimeRange: &telemetryv1.TimeRange{
				StartTime: timestamppb.New(startTime),
				EndTime:   timestamppb.New(endTime),
			},
		}

		query, err := toTimeSeriesTelemetryQuery(req)
		require.NoError(t, err)
		require.NotNil(t, query.TimeRange.StartTime)
		require.NotNil(t, query.TimeRange.EndTime)
		require.Equal(t, startTime.Unix(), query.TimeRange.StartTime.Unix())
		require.Equal(t, endTime.Unix(), query.TimeRange.EndTime.Unix())
	})

	t.Run("optional fields handling", func(t *testing.T) {
		// Test request with optional max_age
		maxAge := durationpb.New(time.Hour)
		req := &telemetryv1.GetSnapshotRequest{
			DeviceIds: []string{"device1"},
			MeasurementTypes: []telemetryv1.MeasurementType{
				telemetryv1.MeasurementType_MEASUREMENT_TYPE_TEMPERATURE,
			},
			MaxAge: maxAge,
		}

		query, err := toLatestTelemetryQuery(req)
		require.NoError(t, err)
		require.NotNil(t, query.MaxAge)
		require.Equal(t, time.Hour, *query.MaxAge)
	})
}
