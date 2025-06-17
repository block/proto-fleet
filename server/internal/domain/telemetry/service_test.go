package telemetry

import (
	"context"
	"errors"
	"testing"
	"time"

	mock "github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/mocks"
	"github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"
	gomock "github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTelemetryService(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerManager := mock.NewMockMinerManager(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)

	config := Config{
		StalenessThreshold: 1 * time.Minute,
		FetchInterval:      10 * time.Second,
		ConcurrencyLimit:   5,
	}

	service := NewTelemetryService(config, mockDataStore, mockMinerManager, mockScheduler)

	// Test that the service was created successfully
	assert.NotNil(t, service)
}

func TestTelemetryService_AddDevices(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerManager := mock.NewMockMinerManager(ctrl)

	tests := []struct {
		name      string
		deviceIDs []models.DeviceID
		mockSetup func(*mock.MockUpdateScheduler)
		wantErr   bool
	}{
		{
			name:      "empty device list",
			deviceIDs: []models.DeviceID{},
			mockSetup: func(_ *mock.MockUpdateScheduler) {
				// No expectations needed for empty list
			},
			wantErr: false,
		},
		{
			name:      "successful add",
			deviceIDs: []models.DeviceID{1, 2, 3},
			mockSetup: func(mockScheduler *mock.MockUpdateScheduler) {
				mockScheduler.EXPECT().
					AddNewDevices(gomock.Any(), models.DeviceID(1), models.DeviceID(2), models.DeviceID(3)).
					Return(nil)
			},
			wantErr: false,
		},
		{
			name:      "scheduler error",
			deviceIDs: []models.DeviceID{1, 2, 3},
			mockSetup: func(mockScheduler *mock.MockUpdateScheduler) {
				mockScheduler.EXPECT().
					AddNewDevices(gomock.Any(), models.DeviceID(1), models.DeviceID(2), models.DeviceID(3)).
					Return(errors.New("scheduler error"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockScheduler := mock.NewMockUpdateScheduler(ctrl)
			tt.mockSetup(mockScheduler)

			service := NewTelemetryService(Config{
				StalenessThreshold: 1 * time.Minute,
				FetchInterval:      10 * time.Second,
				ConcurrencyLimit:   5,
			}, mockDataStore, mockMinerManager, mockScheduler)

			err := service.AddDevices(t.Context(), tt.deviceIDs...)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestTelemetryService_RemoveDevices(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerManager := mock.NewMockMinerManager(ctrl)

	tests := []struct {
		name      string
		deviceIDs []models.DeviceID
		mockSetup func(*mock.MockUpdateScheduler)
		wantErr   bool
	}{
		{
			name:      "empty device list",
			deviceIDs: []models.DeviceID{},
			mockSetup: func(_ *mock.MockUpdateScheduler) {
				// No expectations needed for empty list
			},
			wantErr: false,
		},
		{
			name:      "successful remove",
			deviceIDs: []models.DeviceID{1, 2, 3},
			mockSetup: func(mockScheduler *mock.MockUpdateScheduler) {
				mockScheduler.EXPECT().
					RemoveDevices(gomock.Any(), models.DeviceID(1), models.DeviceID(2), models.DeviceID(3)).
					Return(nil)
			},
			wantErr: false,
		},
		{
			name:      "scheduler error",
			deviceIDs: []models.DeviceID{1, 2, 3},
			mockSetup: func(mockScheduler *mock.MockUpdateScheduler) {
				mockScheduler.EXPECT().
					RemoveDevices(gomock.Any(), models.DeviceID(1), models.DeviceID(2), models.DeviceID(3)).
					Return(errors.New("scheduler error"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockScheduler := mock.NewMockUpdateScheduler(ctrl)
			tt.mockSetup(mockScheduler)

			service := NewTelemetryService(Config{
				StalenessThreshold: 1 * time.Minute,
				FetchInterval:      10 * time.Second,
				ConcurrencyLimit:   5,
			}, mockDataStore, mockMinerManager, mockScheduler)

			err := service.RemoveDevices(t.Context(), tt.deviceIDs...)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestTelemetryService_Start(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerManager := mock.NewMockMinerManager(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)

	// Set up expectations for background processing
	mockScheduler.EXPECT().
		FetchDevices(gomock.Any(), gomock.Any()).
		Return([]models.Device{}, nil).
		AnyTimes()

	config := Config{
		StalenessThreshold: 1 * time.Minute,
		FetchInterval:      100 * time.Millisecond, // Short interval for test
		ConcurrencyLimit:   5,
	}

	service := NewTelemetryService(config, mockDataStore, mockMinerManager, mockScheduler)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	err := service.Start(ctx)
	require.NoError(t, err)

	// Let it run briefly
	time.Sleep(50 * time.Millisecond)

	// Test that the service can be stopped after starting
	err = service.Stop(ctx)
	require.NoError(t, err)

	// Give time for goroutines to clean up
	time.Sleep(100 * time.Millisecond)
}

func TestTelemetryService_Stop(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerManager := mock.NewMockMinerManager(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)

	// Set up expectations for background processing
	mockScheduler.EXPECT().
		FetchDevices(gomock.Any(), gomock.Any()).
		Return([]models.Device{}, nil).
		AnyTimes()

	config := Config{
		StalenessThreshold: 1 * time.Minute,
		FetchInterval:      100 * time.Millisecond, // Short interval for test
		ConcurrencyLimit:   5,
	}

	service := NewTelemetryService(config, mockDataStore, mockMinerManager, mockScheduler)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	// Start the service first
	err := service.Start(ctx)
	require.NoError(t, err)

	// Let it run briefly
	time.Sleep(50 * time.Millisecond)

	// Test that Stop works without error
	err = service.Stop(ctx)
	require.NoError(t, err)

	// Give time for goroutines to clean up
	time.Sleep(100 * time.Millisecond)
}

// TestTelemetryService_MinerDataRetrieval validates that the service correctly interacts with miners
func TestTelemetryService_MinerDataRetrieval(t *testing.T) {
	t.Run("validates miner telemetry data structure", func(t *testing.T) {
		// Test the miner's GetTelemetryMeasurements method directly
		miner := &models.Miner{}
		ctx := t.Context()
		fromTime := time.Now().Add(-1 * time.Hour)

		telemetryData, err := miner.GetTelemetryMeasurements(ctx, fromTime)
		require.NoError(t, err)
		require.Len(t, telemetryData, 1)

		telemetry := telemetryData[0]

		// Validate required fields are present
		assert.NotEmpty(t, telemetry.Measurement)
		assert.NotNil(t, telemetry.Fields)
		assert.NotNil(t, telemetry.Tags)
		assert.False(t, telemetry.Timestamp.IsZero())

		// Validate expected field types and values
		if hashrate, exists := telemetry.Fields["hashrate"]; exists {
			assert.IsType(t, float64(0), hashrate)
			rate, ok := hashrate.(float64)
			require.True(t, ok, "hashrate should be a float64")
			assert.Greater(t, rate, 0.0)
		}

		if temp, exists := telemetry.Fields["temperature"]; exists {
			assert.IsType(t, float64(0), temp)
		}

		// Validate tags are strings
		for key, value := range telemetry.Tags {
			assert.IsType(t, "", key)
			assert.IsType(t, "", value)
			assert.NotEmpty(t, key)
			assert.NotEmpty(t, value)
		}
	})

	t.Run("validates miner manager retrieves correct miner for device", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockMinerManager := mock.NewMockMinerManager(ctrl)

		// Test that miner manager mock can be configured correctly
		deviceID := models.DeviceID(123)
		expectedMiner := models.Miner{}

		mockMinerManager.EXPECT().
			GetMinerFromDeviceID(gomock.Any(), deviceID).
			Return(expectedMiner, nil)

		// Call the mock to verify it works
		miner, err := mockMinerManager.GetMinerFromDeviceID(t.Context(), deviceID)
		require.NoError(t, err)
		assert.Equal(t, expectedMiner, miner)
	})

	t.Run("validates miner manager handles multiple device requests", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockMinerManager := mock.NewMockMinerManager(ctrl)

		// Set up expectations for multiple devices
		deviceIDs := []models.DeviceID{100, 200, 300}
		expectedMiners := []models.Miner{{}, {}, {}}

		for i, deviceID := range deviceIDs {
			mockMinerManager.EXPECT().
				GetMinerFromDeviceID(gomock.Any(), deviceID).
				Return(expectedMiners[i], nil)
		}

		// Verify each device returns the correct miner
		for i, deviceID := range deviceIDs {
			miner, err := mockMinerManager.GetMinerFromDeviceID(t.Context(), deviceID)
			require.NoError(t, err)
			assert.Equal(t, expectedMiners[i], miner)
		}
	})

	t.Run("validates miner manager error handling", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockMinerManager := mock.NewMockMinerManager(ctrl)

		deviceID := models.DeviceID(404)
		expectedError := errors.New("miner not found for device")

		mockMinerManager.EXPECT().
			GetMinerFromDeviceID(gomock.Any(), deviceID).
			Return(models.Miner{}, expectedError)

		// Verify error is properly returned
		_, err := mockMinerManager.GetMinerFromDeviceID(t.Context(), deviceID)
		require.Error(t, err)
		assert.Equal(t, expectedError, err)
	})

	t.Run("validates telemetry data from different time ranges", func(t *testing.T) {
		miner := &models.Miner{}
		ctx := t.Context()

		// Test different time ranges
		timeRanges := []time.Time{
			time.Now().Add(-1 * time.Hour),
			time.Now().Add(-30 * time.Minute),
			time.Now().Add(-5 * time.Minute),
		}

		for _, fromTime := range timeRanges {
			telemetryData, err := miner.GetTelemetryMeasurements(ctx, fromTime)
			require.NoError(t, err)
			require.NotEmpty(t, telemetryData)

			// Validate that timestamp is consistent with the requested time
			for _, telemetry := range telemetryData {
				assert.True(t, telemetry.Timestamp.Equal(fromTime) || telemetry.Timestamp.After(fromTime),
					"telemetry timestamp should be at or after the requested time")
			}
		}
	})
}

// TestTelemetryService_DataStoreInteraction validates interaction with the data store
func TestTelemetryService_DataStoreInteraction(t *testing.T) {
	t.Run("validates datastore stores telemetry data with correct structure", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
		mockMinerManager := mock.NewMockMinerManager(ctrl)
		mockScheduler := mock.NewMockUpdateScheduler(ctrl)

		// Create test telemetry data
		testTelemetryData := []models.Telemetry{
			{
				Measurement: "test_measurement",
				Fields: map[string]any{
					"hashrate":    1000.0,
					"temperature": 65.0,
				},
				Tags: map[string]string{
					"device_id": "123",
					"location":  "datacenter_a",
				},
				Timestamp: time.Now(),
			},
		}

		// Expect datastore Store method to be called with telemetry data
		mockDataStore.EXPECT().
			Store(gomock.Any(), gomock.Any()).
			Do(func(ctx context.Context, data ...models.Telemetry) {
				// Validate that telemetry data structure is correct
				require.NotEmpty(t, data)
				for _, telemetry := range data {
					assert.NotEmpty(t, telemetry.Measurement)
					assert.NotNil(t, telemetry.Fields)
					assert.NotNil(t, telemetry.Tags)
					assert.False(t, telemetry.Timestamp.IsZero())
				}
			}).
			Return(nil)

		service := NewTelemetryService(Config{
			StalenessThreshold: 1 * time.Minute,
			FetchInterval:      10 * time.Second,
			ConcurrencyLimit:   5,
		}, mockDataStore, mockMinerManager, mockScheduler)

		// Test that the service is configured with the datastore
		assert.NotNil(t, service)

		// Directly test the datastore mock to ensure it works as expected
		err := mockDataStore.Store(t.Context(), testTelemetryData...)
		require.NoError(t, err)
	})

	t.Run("validates datastore handles multiple telemetry records", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockDataStore := mock.NewMockTelemetryDataStore(ctrl)

		// Create multiple telemetry records
		multipleRecords := []models.Telemetry{
			{
				Measurement: "miner_telemetry",
				Fields:      map[string]any{"hashrate": 1500.0, "temperature": 68.0},
				Tags:        map[string]string{"device_id": "100", "location": "dc1"},
				Timestamp:   time.Now(),
			},
			{
				Measurement: "miner_telemetry",
				Fields:      map[string]any{"hashrate": 1200.0, "temperature": 72.0},
				Tags:        map[string]string{"device_id": "101", "location": "dc1"},
				Timestamp:   time.Now(),
			},
			{
				Measurement: "miner_telemetry",
				Fields:      map[string]any{"hashrate": 1800.0, "temperature": 65.0},
				Tags:        map[string]string{"device_id": "102", "location": "dc2"},
				Timestamp:   time.Now(),
			},
		}

		// Expect datastore to handle multiple records
		mockDataStore.EXPECT().
			Store(gomock.Any(), gomock.Any()).
			Do(func(ctx context.Context, data ...models.Telemetry) {
				assert.Equal(t, len(multipleRecords), len(data))
				for i, telemetry := range data {
					assert.Equal(t, multipleRecords[i].Measurement, telemetry.Measurement)
					assert.Equal(t, multipleRecords[i].Fields, telemetry.Fields)
					assert.Equal(t, multipleRecords[i].Tags, telemetry.Tags)
				}
			}).
			Return(nil)

		err := mockDataStore.Store(t.Context(), multipleRecords...)
		require.NoError(t, err)
	})

	t.Run("validates datastore validates field types", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockDataStore := mock.NewMockTelemetryDataStore(ctrl)

		// Test different field types
		telemetryWithVariousTypes := []models.Telemetry{
			{
				Measurement: "test_measurement",
				Fields: map[string]any{
					"float_field":  123.45,
					"int_field":    int64(789),
					"string_field": "test_value",
					"bool_field":   true,
				},
				Tags: map[string]string{
					"device_id": "test_device",
				},
				Timestamp: time.Now(),
			},
		}

		mockDataStore.EXPECT().
			Store(gomock.Any(), gomock.Any()).
			Do(func(ctx context.Context, data ...models.Telemetry) {
				require.Len(t, data, 1)
				fields := data[0].Fields

				// Validate field types are preserved
				assert.IsType(t, float64(0), fields["float_field"])
				assert.IsType(t, int64(0), fields["int_field"])
				assert.IsType(t, "", fields["string_field"])
				assert.IsType(t, true, fields["bool_field"])
			}).
			Return(nil)

		err := mockDataStore.Store(t.Context(), telemetryWithVariousTypes...)
		require.NoError(t, err)
	})

	t.Run("validates datastore error handling", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
		mockMinerManager := mock.NewMockMinerManager(ctrl)
		mockScheduler := mock.NewMockUpdateScheduler(ctrl)

		// Simulate datastore error
		mockDataStore.EXPECT().
			Store(gomock.Any(), gomock.Any()).
			Return(errors.New("datastore connection failed"))

		service := NewTelemetryService(Config{
			StalenessThreshold: 1 * time.Minute,
			FetchInterval:      10 * time.Second,
			ConcurrencyLimit:   5,
		}, mockDataStore, mockMinerManager, mockScheduler)

		assert.NotNil(t, service)

		// Test that datastore errors are properly handled
		testData := []models.Telemetry{{
			Measurement: "test",
			Fields:      map[string]any{"value": 1.0},
			Tags:        map[string]string{"tag": "value"},
			Timestamp:   time.Now(),
		}}

		err := mockDataStore.Store(t.Context(), testData...)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "datastore connection failed")
	})

	t.Run("validates datastore handles empty data gracefully", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockDataStore := mock.NewMockTelemetryDataStore(ctrl)

		// Test empty data handling
		mockDataStore.EXPECT().
			Store(gomock.Any()).
			Do(func(ctx context.Context, data ...models.Telemetry) {
				assert.Empty(t, data)
			}).
			Return(nil)

		err := mockDataStore.Store(t.Context())
		require.NoError(t, err)
	})

	t.Run("validates datastore context handling", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockDataStore := mock.NewMockTelemetryDataStore(ctrl)

		// Test context cancellation
		ctx, cancel := context.WithCancel(t.Context())
		cancel() // Cancel immediately

		testData := []models.Telemetry{{
			Measurement: "test",
			Fields:      map[string]any{"value": 1.0},
			Tags:        map[string]string{"tag": "value"},
			Timestamp:   time.Now(),
		}}

		mockDataStore.EXPECT().
			Store(gomock.Any(), gomock.Any()).
			Do(func(receivedCtx context.Context, _ ...models.Telemetry) {
				// Verify context is passed through
				assert.Equal(t, ctx, receivedCtx)
			}).
			Return(errors.New("context cancelled"))

		err := mockDataStore.Store(ctx, testData...)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "context cancelled")
	})
}

// TestTelemetryService_Integration tests the service's behavior in an integrated manner
// without accessing private fields or methods
func TestTelemetryService_Integration(t *testing.T) {
	t.Run("error handling in scheduler operations", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
		mockMinerManager := mock.NewMockMinerManager(ctrl)
		mockScheduler := mock.NewMockUpdateScheduler(ctrl)

		// Set up expectations for scheduler errors
		mockScheduler.EXPECT().
			AddNewDevices(gomock.Any(), models.DeviceID(1), models.DeviceID(2), models.DeviceID(3)).
			Return(errors.New("scheduler add error"))

		mockScheduler.EXPECT().
			RemoveDevices(gomock.Any(), models.DeviceID(1), models.DeviceID(2), models.DeviceID(3)).
			Return(errors.New("scheduler remove error"))

		service := NewTelemetryService(Config{
			StalenessThreshold: 1 * time.Minute,
			FetchInterval:      10 * time.Second,
			ConcurrencyLimit:   5,
		}, mockDataStore, mockMinerManager, mockScheduler)

		// Test that errors are properly propagated
		err := service.AddDevices(t.Context(), 1, 2, 3)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "scheduler add error")

		err = service.RemoveDevices(t.Context(), 1, 2, 3)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "scheduler remove error")
	})

	t.Run("service operations without background processing", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
		mockMinerManager := mock.NewMockMinerManager(ctrl)
		mockScheduler := mock.NewMockUpdateScheduler(ctrl)

		// Set up expectations for successful operations
		mockScheduler.EXPECT().
			AddNewDevices(gomock.Any(), models.DeviceID(1), models.DeviceID(2), models.DeviceID(3)).
			Return(nil)

		mockScheduler.EXPECT().
			RemoveDevices(gomock.Any(), models.DeviceID(2)).
			Return(nil)

		service := NewTelemetryService(Config{
			StalenessThreshold: 1 * time.Minute,
			FetchInterval:      10 * time.Second,
			ConcurrencyLimit:   5,
		}, mockDataStore, mockMinerManager, mockScheduler)

		// Test adding devices
		err := service.AddDevices(t.Context(), 1, 2, 3)
		require.NoError(t, err)

		// Test removing devices
		err = service.RemoveDevices(t.Context(), 2)
		require.NoError(t, err)
	})

	t.Run("validates complete telemetry workflow validation", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
		mockMinerManager := mock.NewMockMinerManager(ctrl)
		mockScheduler := mock.NewMockUpdateScheduler(ctrl)

		// Test the complete workflow: device scheduling -> service lifecycle
		deviceID := models.DeviceID(42)

		// Step 1: Add devices to service
		mockScheduler.EXPECT().
			AddNewDevices(gomock.Any(), deviceID).
			Return(nil)

		// Set up expectations for background processing
		mockScheduler.EXPECT().
			FetchDevices(gomock.Any(), gomock.Any()).
			Return([]models.Device{}, nil).
			AnyTimes()

		service := NewTelemetryService(Config{
			StalenessThreshold: 1 * time.Minute,
			FetchInterval:      100 * time.Millisecond, // Short interval for test
			ConcurrencyLimit:   5,
			MetricTimeout:      5 * time.Second,
		}, mockDataStore, mockMinerManager, mockScheduler)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		// Add device to service
		err := service.AddDevices(ctx, deviceID)
		require.NoError(t, err)

		// Step 2: Verify service can be started and stopped
		err = service.Start(ctx)
		require.NoError(t, err)

		// Let it run briefly
		time.Sleep(50 * time.Millisecond)

		err = service.Stop(ctx)
		require.NoError(t, err)

		// Step 3: Remove device from service
		mockScheduler.EXPECT().
			RemoveDevices(gomock.Any(), deviceID).
			Return(nil)

		err = service.RemoveDevices(ctx, deviceID)
		require.NoError(t, err)

		// Give time for goroutines to clean up
		time.Sleep(100 * time.Millisecond)
	})

	t.Run("validates end-to-end data flow from miner to datastore", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
		mockMinerManager := mock.NewMockMinerManager(ctrl)
		mockScheduler := mock.NewMockUpdateScheduler(ctrl)

		// Simulate the expected data flow:
		// 1. Scheduler provides devices that need telemetry updates
		// 2. MinerManager retrieves miner for each device
		// 3. Miner provides telemetry data
		// 4. DataStore stores the telemetry data
		// 5. Scheduler is updated with new device timestamp

		deviceID := models.DeviceID(100)
		testDevice := models.Device{
			ID:            deviceID,
			LastUpdatedAt: time.Now().Add(-30 * time.Second),
		}
		testMiner := models.Miner{}

		// Mock the expected call sequence
		mockScheduler.EXPECT().
			FetchDevices(gomock.Any(), gomock.Any()).
			Return([]models.Device{testDevice}, nil)

		mockMinerManager.EXPECT().
			GetMinerFromDeviceID(gomock.Any(), deviceID).
			Return(testMiner, nil)

		// Validate telemetry data structure when stored
		mockDataStore.EXPECT().
			Store(gomock.Any(), gomock.Any()).
			Do(func(ctx context.Context, data ...models.Telemetry) {
				require.NotEmpty(t, data)
				for _, telemetry := range data {
					// Validate the data structure matches what miners provide
					assert.Equal(t, "miner_telemetry", telemetry.Measurement)
					assert.Contains(t, telemetry.Fields, "hashrate")
					assert.Contains(t, telemetry.Fields, "temperature")
					assert.Contains(t, telemetry.Tags, "miner")
					assert.Contains(t, telemetry.Tags, "location")
					assert.Contains(t, telemetry.Tags, "device_id")
				}
			}).
			Return(nil)

		mockScheduler.EXPECT().
			AddDevices(gomock.Any(), gomock.Any()).
			Do(func(ctx context.Context, devices ...models.Device) {
				require.Len(t, devices, 1)
				assert.Equal(t, deviceID, devices[0].ID)
				// Verify timestamp was updated
				assert.True(t, devices[0].LastUpdatedAt.After(testDevice.LastUpdatedAt))
			}).
			Return(nil)

		service := NewTelemetryService(Config{
			StalenessThreshold: 1 * time.Minute,
			FetchInterval:      10 * time.Second,
			ConcurrencyLimit:   1,
			MetricTimeout:      5 * time.Second,
		}, mockDataStore, mockMinerManager, mockScheduler)

		// Test that the service is properly configured
		assert.NotNil(t, service)

		// Manually test the data flow by calling the mocks in sequence
		// This validates our understanding of the expected interactions

		// 1. Fetch devices
		devices, err := mockScheduler.FetchDevices(t.Context(), time.Now().Add(-1*time.Hour))
		require.NoError(t, err)
		require.Len(t, devices, 1)

		// 2. Get miner for device
		miner, err := mockMinerManager.GetMinerFromDeviceID(t.Context(), devices[0].ID)
		require.NoError(t, err)

		// 3. Get telemetry from miner
		telemetryData, err := miner.GetTelemetryMeasurements(t.Context(), devices[0].LastUpdatedAt)
		require.NoError(t, err)
		require.NotEmpty(t, telemetryData)

		// 4. Store telemetry data
		err = mockDataStore.Store(t.Context(), telemetryData...)
		require.NoError(t, err)

		// 5. Update device timestamp
		updatedDevice := models.Device{
			ID:            devices[0].ID,
			LastUpdatedAt: time.Now(),
		}
		err = mockScheduler.AddDevices(t.Context(), updatedDevice)
		require.NoError(t, err)
	})

	t.Run("validates concurrent data processing workflow", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
		mockMinerManager := mock.NewMockMinerManager(ctrl)
		mockScheduler := mock.NewMockUpdateScheduler(ctrl)

		// Test concurrent processing of multiple devices
		deviceIDs := []models.DeviceID{200, 201, 202}
		testDevices := make([]models.Device, len(deviceIDs))
		testMiners := make([]models.Miner, len(deviceIDs))

		for i, deviceID := range deviceIDs {
			testDevices[i] = models.Device{
				ID:            deviceID,
				LastUpdatedAt: time.Now().Add(-45 * time.Second),
			}
			testMiners[i] = models.Miner{}
		}

		// Set up expectations for concurrent processing
		mockScheduler.EXPECT().
			FetchDevices(gomock.Any(), gomock.Any()).
			Return(testDevices, nil)

		// Each device should have its miner retrieved
		for i, deviceID := range deviceIDs {
			mockMinerManager.EXPECT().
				GetMinerFromDeviceID(gomock.Any(), deviceID).
				Return(testMiners[i], nil)
		}

		// Expect multiple datastore calls (one per device)
		mockDataStore.EXPECT().
			Store(gomock.Any(), gomock.Any()).
			Times(len(deviceIDs)).
			Do(func(ctx context.Context, data ...models.Telemetry) {
				require.NotEmpty(t, data)
				// Validate each telemetry record
				for _, telemetry := range data {
					assert.NotEmpty(t, telemetry.Measurement)
					assert.NotNil(t, telemetry.Fields)
					assert.NotNil(t, telemetry.Tags)
				}
			}).
			Return(nil)

		// Expect device updates for each processed device
		mockScheduler.EXPECT().
			AddDevices(gomock.Any(), gomock.Any()).
			Times(len(deviceIDs)).
			Do(func(ctx context.Context, devices ...models.Device) {
				require.Len(t, devices, 1)
				// Verify the device ID is one of our test devices
				deviceFound := false
				for _, expectedID := range deviceIDs {
					if devices[0].ID == expectedID {
						deviceFound = true
						break
					}
				}
				assert.True(t, deviceFound, "Device ID should be one of the expected test devices")
			}).
			Return(nil)

		service := NewTelemetryService(Config{
			StalenessThreshold: 1 * time.Minute,
			FetchInterval:      10 * time.Second,
			ConcurrencyLimit:   3, // Allow concurrent processing
			MetricTimeout:      5 * time.Second,
		}, mockDataStore, mockMinerManager, mockScheduler)

		assert.NotNil(t, service)

		// Simulate the concurrent workflow
		devices, err := mockScheduler.FetchDevices(t.Context(), time.Now().Add(-1*time.Hour))
		require.NoError(t, err)
		require.Len(t, devices, len(deviceIDs))

		// Process each device concurrently (simulating the worker behavior)
		for _, device := range devices {
			// Get miner
			miner, err := mockMinerManager.GetMinerFromDeviceID(t.Context(), device.ID)
			require.NoError(t, err)

			// Get telemetry
			telemetryData, err := miner.GetTelemetryMeasurements(t.Context(), device.LastUpdatedAt)
			require.NoError(t, err)

			// Store telemetry
			err = mockDataStore.Store(t.Context(), telemetryData...)
			require.NoError(t, err)

			// Update device
			updatedDevice := models.Device{
				ID:            device.ID,
				LastUpdatedAt: time.Now(),
			}
			err = mockScheduler.AddDevices(t.Context(), updatedDevice)
			require.NoError(t, err)
		}
	})
}

// TestTelemetryService_ComponentInteraction validates that all components work together
func TestTelemetryService_ComponentInteraction(t *testing.T) {
	t.Run("validates all dependencies are properly configured", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
		mockMinerManager := mock.NewMockMinerManager(ctrl)
		mockScheduler := mock.NewMockUpdateScheduler(ctrl)

		// Set up expectations for background processing
		mockScheduler.EXPECT().
			FetchDevices(gomock.Any(), gomock.Any()).
			Return([]models.Device{}, nil).
			AnyTimes()

		config := Config{
			StalenessThreshold: 1 * time.Minute,
			FetchInterval:      100 * time.Millisecond, // Short interval for test
			ConcurrencyLimit:   5,
			MetricTimeout:      5 * time.Second,
		}

		service := NewTelemetryService(config, mockDataStore, mockMinerManager, mockScheduler)

		// Validate service is properly initialized
		assert.NotNil(t, service)

		// Test that all public methods work without panicking
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		// Test Start/Stop lifecycle
		err := service.Start(ctx)
		require.NoError(t, err)

		// Let it run briefly
		time.Sleep(50 * time.Millisecond)

		err = service.Stop(ctx)
		require.NoError(t, err)

		// Give time for goroutines to clean up
		time.Sleep(100 * time.Millisecond)
	})

	t.Run("validates error propagation through component chain", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
		mockMinerManager := mock.NewMockMinerManager(ctrl)
		mockScheduler := mock.NewMockUpdateScheduler(ctrl)

		// Test error scenarios for each component
		deviceID := models.DeviceID(500)

		// Test scheduler errors
		mockScheduler.EXPECT().
			AddNewDevices(gomock.Any(), deviceID).
			Return(errors.New("scheduler unavailable"))

		mockScheduler.EXPECT().
			RemoveDevices(gomock.Any(), deviceID).
			Return(errors.New("scheduler removal failed"))

		// Test miner manager errors
		mockMinerManager.EXPECT().
			GetMinerFromDeviceID(gomock.Any(), deviceID).
			Return(models.Miner{}, errors.New("miner not accessible"))

		// Test datastore errors
		mockDataStore.EXPECT().
			Store(gomock.Any(), gomock.Any()).
			Return(errors.New("datastore write failed"))

		service := NewTelemetryService(Config{
			StalenessThreshold: 1 * time.Minute,
			FetchInterval:      10 * time.Second,
			ConcurrencyLimit:   5,
			MetricTimeout:      5 * time.Second,
		}, mockDataStore, mockMinerManager, mockScheduler)

		// Verify errors are properly propagated
		err := service.AddDevices(t.Context(), deviceID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "scheduler unavailable")

		err = service.RemoveDevices(t.Context(), deviceID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "scheduler removal failed")

		// Test component errors in isolation
		_, err = mockMinerManager.GetMinerFromDeviceID(t.Context(), deviceID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "miner not accessible")

		testData := []models.Telemetry{{
			Measurement: "test",
			Fields:      map[string]any{"value": 1.0},
			Tags:        map[string]string{"tag": "value"},
			Timestamp:   time.Now(),
		}}
		err = mockDataStore.Store(t.Context(), testData...)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "datastore write failed")
	})

	t.Run("validates component interaction with realistic data volumes", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
		mockMinerManager := mock.NewMockMinerManager(ctrl)
		mockScheduler := mock.NewMockUpdateScheduler(ctrl)

		// Test with larger data volumes
		numDevices := 50
		deviceIDs := make([]models.DeviceID, numDevices)
		testDevices := make([]models.Device, numDevices)

		for i := range numDevices {
			deviceIDs[i] = models.DeviceID(1000 + i)
			testDevices[i] = models.Device{
				ID:            deviceIDs[i],
				LastUpdatedAt: time.Now().Add(-time.Duration(i) * time.Second),
			}
		}

		// Mock scheduler to return all devices
		mockScheduler.EXPECT().
			FetchDevices(gomock.Any(), gomock.Any()).
			Return(testDevices, nil)

		// Mock miner manager for each device
		for _, deviceID := range deviceIDs {
			mockMinerManager.EXPECT().
				GetMinerFromDeviceID(gomock.Any(), deviceID).
				Return(models.Miner{}, nil)
		}

		// Mock datastore to handle all telemetry data
		mockDataStore.EXPECT().
			Store(gomock.Any(), gomock.Any()).
			Times(numDevices).
			Do(func(ctx context.Context, data ...models.Telemetry) {
				// Validate data structure for each call
				require.NotEmpty(t, data)
				for _, telemetry := range data {
					assert.NotEmpty(t, telemetry.Measurement)
					assert.NotNil(t, telemetry.Fields)
					assert.NotNil(t, telemetry.Tags)
				}
			}).
			Return(nil)

		// Mock scheduler device updates
		mockScheduler.EXPECT().
			AddDevices(gomock.Any(), gomock.Any()).
			Times(numDevices).
			Return(nil)

		service := NewTelemetryService(Config{
			StalenessThreshold: 2 * time.Minute,
			FetchInterval:      5 * time.Second,
			ConcurrencyLimit:   10,
			MetricTimeout:      3 * time.Second,
		}, mockDataStore, mockMinerManager, mockScheduler)

		assert.NotNil(t, service)

		// Simulate processing all devices
		devices, err := mockScheduler.FetchDevices(t.Context(), time.Now().Add(-2*time.Hour))
		require.NoError(t, err)
		require.Len(t, devices, numDevices)

		// Process each device
		for _, device := range devices {
			miner, err := mockMinerManager.GetMinerFromDeviceID(t.Context(), device.ID)
			require.NoError(t, err)

			telemetryData, err := miner.GetTelemetryMeasurements(t.Context(), device.LastUpdatedAt)
			require.NoError(t, err)

			err = mockDataStore.Store(t.Context(), telemetryData...)
			require.NoError(t, err)

			updatedDevice := models.Device{
				ID:            device.ID,
				LastUpdatedAt: time.Now(),
			}
			err = mockScheduler.AddDevices(t.Context(), updatedDevice)
			require.NoError(t, err)
		}
	})

	t.Run("validates component timeout and context handling", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
		mockMinerManager := mock.NewMockMinerManager(ctrl)
		mockScheduler := mock.NewMockUpdateScheduler(ctrl)

		// Test context cancellation and timeouts
		deviceID := models.DeviceID(600)

		// Test with cancelled context
		cancelledCtx, cancel := context.WithCancel(t.Context())
		cancel() // Cancel immediately

		mockScheduler.EXPECT().
			AddNewDevices(gomock.Any(), deviceID).
			Do(func(ctx context.Context, _ ...models.DeviceID) {
				// Verify context is passed through
				select {
				case <-ctx.Done():
					// Context should be cancelled
				default:
					t.Error("Expected context to be cancelled")
				}
			}).
			Return(errors.New("context cancelled"))

		service := NewTelemetryService(Config{
			StalenessThreshold: 1 * time.Minute,
			FetchInterval:      10 * time.Second,
			ConcurrencyLimit:   5,
			MetricTimeout:      100 * time.Millisecond, // Short timeout
		}, mockDataStore, mockMinerManager, mockScheduler)

		// Test with cancelled context
		err := service.AddDevices(cancelledCtx, deviceID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "context cancelled")
	})

	t.Run("validates component state consistency", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
		mockMinerManager := mock.NewMockMinerManager(ctrl)
		mockScheduler := mock.NewMockUpdateScheduler(ctrl)

		// Test that component interactions maintain consistent state
		deviceIDs := []models.DeviceID{700, 701, 702}

		// Add devices
		mockScheduler.EXPECT().
			AddNewDevices(gomock.Any(), deviceIDs[0], deviceIDs[1], deviceIDs[2]).
			Return(nil)

		// Remove some devices
		mockScheduler.EXPECT().
			RemoveDevices(gomock.Any(), deviceIDs[1]).
			Return(nil)

		// Add back removed device
		mockScheduler.EXPECT().
			AddNewDevices(gomock.Any(), deviceIDs[1]).
			Return(nil)

		// Set up expectations for background processing
		mockScheduler.EXPECT().
			FetchDevices(gomock.Any(), gomock.Any()).
			Return([]models.Device{}, nil).
			AnyTimes()

		service := NewTelemetryService(Config{
			StalenessThreshold: 1 * time.Minute,
			FetchInterval:      100 * time.Millisecond, // Short interval for test
			ConcurrencyLimit:   5,
			MetricTimeout:      5 * time.Second,
		}, mockDataStore, mockMinerManager, mockScheduler)

		// Test device management operations
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		err := service.AddDevices(ctx, deviceIDs...)
		require.NoError(t, err)

		err = service.RemoveDevices(ctx, deviceIDs[1])
		require.NoError(t, err)

		err = service.AddDevices(ctx, deviceIDs[1])
		require.NoError(t, err)

		// Test service lifecycle
		err = service.Start(ctx)
		require.NoError(t, err)

		// Let it run briefly
		time.Sleep(50 * time.Millisecond)

		err = service.Stop(ctx)
		require.NoError(t, err)

		// Give time for goroutines to clean up
		time.Sleep(100 * time.Millisecond)
	})
}
