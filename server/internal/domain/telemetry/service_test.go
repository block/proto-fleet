package telemetry

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gomock "go.uber.org/mock/gomock"

	telemetryv1 "github.com/block/proto-fleet/server/generated/grpc/telemetry/v1"
	"github.com/block/proto-fleet/server/internal/domain/diagnostics"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	minerMocks "github.com/block/proto-fleet/server/internal/domain/miner/interfaces/mocks"
	mm "github.com/block/proto-fleet/server/internal/domain/miner/models"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	storesMocks "github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	mock "github.com/block/proto-fleet/server/internal/domain/telemetry/mocks"
	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
)

func TestNewTelemetryService(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

	config := Config{
		StalenessThreshold: 1 * time.Minute,
		FetchInterval:      10 * time.Second,
		ConcurrencyLimit:   5,
	}

	service := NewTelemetryService(config, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

	// Test that the service was created successfully
	assert.NotNil(t, service)
}

func TestTelemetryService_AddDevices(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

	tests := []struct {
		name      string
		deviceIDs []models.DeviceIdentifier
		mockSetup func(*mock.MockUpdateScheduler)
		wantErr   bool
	}{
		{
			name:      "empty device list",
			deviceIDs: []models.DeviceIdentifier{},
			mockSetup: func(_ *mock.MockUpdateScheduler) {
				// No expectations needed for empty list
			},
			wantErr: false,
		},
		{
			name:      "successful add",
			deviceIDs: []models.DeviceIdentifier{"1", "2", "3"},
			mockSetup: func(mockScheduler *mock.MockUpdateScheduler) {
				mockScheduler.EXPECT().
					AddNewDevices(gomock.Any(), models.DeviceIdentifier("1"), models.DeviceIdentifier("2"), models.DeviceIdentifier("3")).
					Return(nil)
			},
			wantErr: false,
		},
		{
			name:      "scheduler error",
			deviceIDs: []models.DeviceIdentifier{"1", "2", "3"},
			mockSetup: func(mockScheduler *mock.MockUpdateScheduler) {
				mockScheduler.EXPECT().
					AddNewDevices(gomock.Any(), models.DeviceIdentifier("1"), models.DeviceIdentifier("2"), models.DeviceIdentifier("3")).
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
			}, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

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
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

	tests := []struct {
		name      string
		deviceIDs []models.DeviceIdentifier
		mockSetup func(*mock.MockUpdateScheduler)
		wantErr   bool
	}{
		{
			name:      "empty device list",
			deviceIDs: []models.DeviceIdentifier{},
			mockSetup: func(_ *mock.MockUpdateScheduler) {
				// No expectations needed for empty list
			},
			wantErr: false,
		},
		{
			name:      "successful remove",
			deviceIDs: []models.DeviceIdentifier{"1", "2", "3"},
			mockSetup: func(mockScheduler *mock.MockUpdateScheduler) {
				mockScheduler.EXPECT().
					RemoveDevices(gomock.Any(), models.DeviceIdentifier("1"), models.DeviceIdentifier("2"), models.DeviceIdentifier("3")).
					Return(nil)
			},
			wantErr: false,
		},
		{
			name:      "scheduler error",
			deviceIDs: []models.DeviceIdentifier{"1", "2", "3"},
			mockSetup: func(mockScheduler *mock.MockUpdateScheduler) {
				mockScheduler.EXPECT().
					RemoveDevices(gomock.Any(), models.DeviceIdentifier("1"), models.DeviceIdentifier("2"), models.DeviceIdentifier("3")).
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
			}, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

			service.devicesForStatusPolling.Store(models.DeviceIdentifier("1"), struct{}{})
			err := service.RemoveDevices(t.Context(), tt.deviceIDs...)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			if slices.Contains(tt.deviceIDs, models.DeviceIdentifier("1")) {
				_, ok := service.devicesForStatusPolling.Load(models.DeviceIdentifier("1"))
				assert.False(t, ok)
			}
		})
	}
}

func TestTelemetryService_Start(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

	// Set up expectations for background processing
	mockScheduler.EXPECT().
		FetchDevices(gomock.Any(), gomock.Any()).
		Return([]models.Device{}, nil).
		AnyTimes()

	// Set up expectations for device polling
	mockDeviceStore.EXPECT().
		GetAllPairedDeviceIdentifiers(gomock.Any()).
		Return([]models.DeviceIdentifier{}, nil).
		AnyTimes()

	// Snapshot routine fires once on Start and then on the ticker.
	mockDataStore.EXPECT().
		InsertMinerStateSnapshot(gomock.Any(), gomock.Any()).
		Return(nil).
		AnyTimes()

	config := Config{
		StalenessThreshold: 1 * time.Minute,
		FetchInterval:      100 * time.Millisecond, // Short interval for test
		ConcurrencyLimit:   5,
		DevicePollInterval: 100 * time.Millisecond, // Short interval for test
	}

	service := NewTelemetryService(config, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

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
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

	// Set up expectations for background processing
	mockScheduler.EXPECT().
		FetchDevices(gomock.Any(), gomock.Any()).
		Return([]models.Device{}, nil).
		AnyTimes()

	// Set up expectations for device polling
	mockDeviceStore.EXPECT().
		GetAllPairedDeviceIdentifiers(gomock.Any()).
		Return([]models.DeviceIdentifier{}, nil).
		AnyTimes()

	mockDataStore.EXPECT().
		InsertMinerStateSnapshot(gomock.Any(), gomock.Any()).
		Return(nil).
		AnyTimes()

	config := Config{
		StalenessThreshold: 1 * time.Minute,
		FetchInterval:      100 * time.Millisecond, // Short interval for test
		ConcurrencyLimit:   5,
		DevicePollInterval: 100 * time.Millisecond, // Short interval for test
	}

	service := NewTelemetryService(config, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

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

// FakeTelemetryData is no longer used - tests now use DeviceMetrics v2 model

func TestTelemetryService_DataStoreInteraction(t *testing.T) {
	type deviceScenario struct {
		device                     models.Device
		deviceMetrics              *modelsV2.DeviceMetrics
		hasSchedulerError          bool
		hasDiscoveryError          bool
		hasDeviceMetricsError      bool
		hasDeviceMetricsStoreError bool
	}

	tests := []struct {
		name            string
		devicesScenario []deviceScenario
	}{
		{
			name: "validates GetDeviceMetrics succeeds and stores device metrics",
			devicesScenario: []deviceScenario{
				{
					device: models.Device{
						ID:            "200",
						LastUpdatedAt: time.Now().Add(-5 * time.Minute),
					},
					deviceMetrics: &modelsV2.DeviceMetrics{
						DeviceIdentifier: "200",
						Timestamp:        time.Now(),
					},
				},
			},
		},
		{
			name: "validates GetDeviceMetrics fails with not implemented",
			devicesScenario: []deviceScenario{
				{
					device: models.Device{
						ID:            "201",
						LastUpdatedAt: time.Now().Add(-5 * time.Minute),
					},
					hasDeviceMetricsError: true,
				},
			},
		},
		{
			name: "validates GetDeviceMetrics succeeds but StoreDeviceMetrics fails",
			devicesScenario: []deviceScenario{
				{
					device: models.Device{
						ID:            "203",
						LastUpdatedAt: time.Now().Add(-5 * time.Minute),
					},
					deviceMetrics: &modelsV2.DeviceMetrics{
						DeviceIdentifier: "203",
						Timestamp:        time.Now(),
					},
					hasDeviceMetricsStoreError: true,
				},
			},
		},
		{
			name: "gets error when device discovery fails",
			devicesScenario: []deviceScenario{
				{
					device: models.Device{
						ID:            "125",
						LastUpdatedAt: time.Now().Add(-5 * time.Minute),
					},
					hasDiscoveryError: true,
				},
			},
		},
		{
			name: "validates multiple devices with successful device metrics",
			devicesScenario: []deviceScenario{
				{
					device: models.Device{
						ID:            "300",
						LastUpdatedAt: time.Now().Add(-5 * time.Minute),
					},
					deviceMetrics: &modelsV2.DeviceMetrics{
						DeviceIdentifier: "300",
						Timestamp:        time.Now(),
					},
				},
				{
					device: models.Device{
						ID:            "301",
						LastUpdatedAt: time.Now().Add(-2 * time.Minute),
					},
					deviceMetrics: &modelsV2.DeviceMetrics{
						DeviceIdentifier: "301",
						Timestamp:        time.Now(),
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
			mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
			mockScheduler := mock.NewMockUpdateScheduler(ctrl)
			mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

			for _, scenario := range test.devicesScenario {
				if scenario.hasDiscoveryError {
					mockMinerGetter.EXPECT().
						GetMinerFromDeviceIdentifier(gomock.Any(), scenario.device.ID).
						Return(nil, errors.New("discovery error"))
					continue
				}
				mockMiner := minerMocks.NewMockMiner(ctrl)
				mockMinerGetter.EXPECT().
					GetMinerFromDeviceIdentifier(gomock.Any(), scenario.device.ID).
					Return(mockMiner, nil)

				// Setup GetDeviceMetrics expectation
				if scenario.deviceMetrics != nil {
					mockMiner.EXPECT().
						GetDeviceMetrics(gomock.Any()).
						Return(*scenario.deviceMetrics, nil)
					// StoreDeviceMetrics is now called asynchronously by metricsWriterRoutine,
					// not inline by GetTelemetryFromDevice.
					mockScheduler.EXPECT().
						AddDevices(gomock.Any(), gomock.Any()).
						Do(func(ctx context.Context, devices ...models.Device) {
							require.Len(t, devices, 1)
							assert.Equal(t, scenario.device.ID, devices[0].ID)
						}).Return(nil).Times(1)
				} else if scenario.hasDeviceMetricsError {
					mockMiner.EXPECT().
						GetDeviceMetrics(gomock.Any()).
						Return(modelsV2.DeviceMetrics{}, errors.New("not implemented"))
					// Even when GetDeviceMetrics fails, service still calls AddDevices to update last_updated_at
					mockScheduler.EXPECT().
						AddDevices(gomock.Any(), gomock.Any()).
						Do(func(ctx context.Context, devices ...models.Device) {
							require.Len(t, devices, 1)
							assert.Equal(t, scenario.device.ID, devices[0].ID)
						}).Return(nil).Times(1)
				}
			}

			service := NewTelemetryService(Config{
				StalenessThreshold: 1 * time.Minute,
				FetchInterval:      10 * time.Second,
				ConcurrencyLimit:   5,
			}, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

			for _, scenario := range test.devicesScenario {
				_, _, err := service.GetTelemetryFromDevice(t.Context(), scenario.device)
				// Only discovery errors and scheduler errors bubble up to caller
				// StoreDeviceMetrics errors are logged but don't fail the operation
				if scenario.hasDiscoveryError || scenario.hasSchedulerError {
					require.Error(t, err)
					continue
				}
				assert.NoError(t, err)
			}
		})
	}

}

func TestTelemetryService_Integration(t *testing.T) {
	t.Run("error handling in scheduler operations", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
		mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
		mockScheduler := mock.NewMockUpdateScheduler(ctrl)
		mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

		// Set up expectations for scheduler errors
		mockScheduler.EXPECT().
			AddNewDevices(gomock.Any(), models.DeviceIdentifier("1"), models.DeviceIdentifier("2"), models.DeviceIdentifier("3")).
			Return(errors.New("scheduler add error"))

		mockScheduler.EXPECT().
			RemoveDevices(gomock.Any(), models.DeviceIdentifier("1"), models.DeviceIdentifier("2"), models.DeviceIdentifier("3")).
			Return(errors.New("scheduler remove error"))

		service := NewTelemetryService(Config{
			StalenessThreshold: 1 * time.Minute,
			FetchInterval:      10 * time.Second,
			ConcurrencyLimit:   5,
		}, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

		// Test that errors are properly propagated
		err := service.AddDevices(t.Context(), "1", "2", "3")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "scheduler add error")

		err = service.RemoveDevices(t.Context(), "1", "2", "3")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "scheduler remove error")
	})

	t.Run("service operations without background processing", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
		mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
		mockScheduler := mock.NewMockUpdateScheduler(ctrl)
		mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

		// Set up expectations for successful operations
		mockScheduler.EXPECT().
			AddNewDevices(gomock.Any(), models.DeviceIdentifier("1"), models.DeviceIdentifier("2"), models.DeviceIdentifier("3")).
			Return(nil)

		mockScheduler.EXPECT().
			RemoveDevices(gomock.Any(), models.DeviceIdentifier("2")).
			Return(nil)

		service := NewTelemetryService(Config{
			StalenessThreshold: 1 * time.Minute,
			FetchInterval:      10 * time.Second,
			ConcurrencyLimit:   5,
		}, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

		// Test adding devices
		err := service.AddDevices(t.Context(), "1", "2", "3")
		require.NoError(t, err)

		// Test removing devices
		err = service.RemoveDevices(t.Context(), "2")
		require.NoError(t, err)
	})

	t.Run("validates complete telemetry workflow validation", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
		mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
		mockScheduler := mock.NewMockUpdateScheduler(ctrl)
		mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

		// Test the complete workflow: device scheduling -> service lifecycle
		deviceID := models.DeviceIdentifier("42")

		// Step 1: Add devices to service
		mockScheduler.EXPECT().
			AddNewDevices(gomock.Any(), deviceID).
			Return(nil)

		// Set up expectations for background processing
		mockScheduler.EXPECT().
			FetchDevices(gomock.Any(), gomock.Any()).
			Return([]models.Device{}, nil).
			AnyTimes()

		// Set up expectations for device polling
		mockDeviceStore.EXPECT().
			GetAllPairedDeviceIdentifiers(gomock.Any()).
			Return([]models.DeviceIdentifier{}, nil).
			AnyTimes()

		mockMinerGetter.EXPECT().
			GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).
			Return(nil, nil).AnyTimes()

		mockDataStore.EXPECT().
			InsertMinerStateSnapshot(gomock.Any(), gomock.Any()).
			Return(nil).
			AnyTimes()

		service := NewTelemetryService(Config{
			StalenessThreshold: 1 * time.Minute,
			FetchInterval:      100 * time.Millisecond, // Short interval for test
			ConcurrencyLimit:   5,
			MetricTimeout:      5 * time.Second,
			DevicePollInterval: 100 * time.Millisecond, // Short interval for test
		}, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		// Add device to service
		err := service.AddDevices(ctx, deviceID)
		require.NoError(t, err)

		// shows that the task was added to get polled as soon as the service starts
		task := <-service.tasks
		require.Equal(t, task.ID, deviceID)

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
}

// TestTelemetryService_ComponentInteraction validates that all components work together
func TestTelemetryService_ComponentInteraction(t *testing.T) {
	t.Run("validates all dependencies are properly configured", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
		mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
		mockScheduler := mock.NewMockUpdateScheduler(ctrl)
		mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

		// Set up expectations for background processing
		mockScheduler.EXPECT().
			FetchDevices(gomock.Any(), gomock.Any()).
			Return([]models.Device{}, nil).
			AnyTimes()

		// Set up expectations for device polling
		mockDeviceStore.EXPECT().
			GetAllPairedDeviceIdentifiers(gomock.Any()).
			Return([]models.DeviceIdentifier{}, nil).
			AnyTimes()

		mockDataStore.EXPECT().
			InsertMinerStateSnapshot(gomock.Any(), gomock.Any()).
			Return(nil).
			AnyTimes()

		config := Config{
			StalenessThreshold: 1 * time.Minute,
			FetchInterval:      100 * time.Millisecond, // Short interval for test
			ConcurrencyLimit:   5,
			MetricTimeout:      5 * time.Second,
			DevicePollInterval: 100 * time.Millisecond, // Short interval for test
		}

		service := NewTelemetryService(config, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

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
		mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
		mockScheduler := mock.NewMockUpdateScheduler(ctrl)
		mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

		// Test error scenarios for each component
		deviceID := models.DeviceIdentifier("500")

		// Test scheduler errors
		mockScheduler.EXPECT().
			AddNewDevices(gomock.Any(), deviceID).
			Return(errors.New("scheduler unavailable"))

		mockScheduler.EXPECT().
			RemoveDevices(gomock.Any(), deviceID).
			Return(errors.New("scheduler removal failed"))

		service := NewTelemetryService(Config{
			StalenessThreshold: 1 * time.Minute,
			FetchInterval:      10 * time.Second,
			ConcurrencyLimit:   5,
			MetricTimeout:      5 * time.Second,
		}, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

		// Verify errors are properly propagated
		err := service.AddDevices(t.Context(), deviceID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "scheduler unavailable")

		err = service.RemoveDevices(t.Context(), deviceID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "scheduler removal failed")
	})

	t.Run("validates component state consistency", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
		mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
		mockScheduler := mock.NewMockUpdateScheduler(ctrl)
		mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

		// Test that component interactions maintain consistent state
		deviceIDs := []models.DeviceIdentifier{"700", "701", "702"}

		mockMinerGetter.EXPECT().
			GetMinerFromDeviceIdentifier(gomock.Any(), deviceIDs[0]).
			Return(nil, nil).AnyTimes()

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

		// Set up expectations for device polling
		mockDeviceStore.EXPECT().
			GetAllPairedDeviceIdentifiers(gomock.Any()).
			Return([]models.DeviceIdentifier{}, nil).
			AnyTimes()

		mockDataStore.EXPECT().
			InsertMinerStateSnapshot(gomock.Any(), gomock.Any()).
			Return(nil).
			AnyTimes()

		service := NewTelemetryService(Config{
			StalenessThreshold: 1 * time.Minute,
			FetchInterval:      100 * time.Millisecond, // Short interval for test
			ConcurrencyLimit:   5,
			MetricTimeout:      5 * time.Second,
			DevicePollInterval: 100 * time.Millisecond, // Short interval for test
		}, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

		// Test device management operations
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		err := service.AddDevices(ctx, deviceIDs...)
		require.NoError(t, err)

		for range deviceIDs {
			<-service.tasks
		}

		err = service.RemoveDevices(ctx, deviceIDs[1])
		require.NoError(t, err)

		err = service.AddDevices(ctx, deviceIDs[1])
		require.NoError(t, err)

		task := <-service.tasks
		require.Equal(t, task.ID, deviceIDs[1])

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

func TestTelemetryService_StreamCombinedMetrics(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	t.Run("successfully streams initial update", func(t *testing.T) {
		mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
		mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
		mockScheduler := mock.NewMockUpdateScheduler(ctrl)
		mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

		mockDeviceStore.EXPECT().GetMinerStateCounts(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(&telemetryv1.MinerStateCounts{}, nil).AnyTimes()

		service := NewTelemetryService(Config{
			StalenessThreshold: 1 * time.Minute,
			FetchInterval:      10 * time.Second,
			ConcurrencyLimit:   5,
		}, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

		deviceIDs := []models.DeviceIdentifier{"device1", "device2"}
		measurementTypes := []models.MeasurementType{models.MeasurementTypeHashrate}
		aggregationTypes := []models.AggregationType{models.AggregationTypeAverage}
		granularity := 1 * time.Minute

		query := models.StreamCombinedMetricsQuery{
			DeviceIDs:        deviceIDs,
			MeasurementTypes: measurementTypes,
			AggregationTypes: aggregationTypes,
			Granularity:      granularity,
			UpdateInterval:   granularity,
		}

		// Mock GetCombinedMetrics to return test data
		expectedMetrics := models.CombinedMetric{
			Metrics: []models.Metric{
				{
					MeasurementType: models.MeasurementTypeHashrate,
					AggregatedValues: []models.AggregatedValue{
						{Type: models.AggregationTypeAverage, Value: 100.0},
					},
					OpenTime: time.Now(),
				},
			},
		}

		mockDataStore.EXPECT().
			GetCombinedMetrics(gomock.Any(), gomock.Any()).
			Return(expectedMetrics, nil).
			Times(1)

		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()

		updateChan, err := service.StreamCombinedMetrics(ctx, query)
		require.NoError(t, err)
		require.NotNil(t, updateChan)

		// Should receive initial update immediately
		select {
		case metrics, ok := <-updateChan:
			require.True(t, ok, "Channel should not be closed")
			assert.Len(t, metrics.Metrics, 1)
			assert.Equal(t, models.MeasurementTypeHashrate, metrics.Metrics[0].MeasurementType)
			assert.Len(t, metrics.Metrics[0].AggregatedValues, 1)
			// The metric value is returned as-is from the mock (no conversion happens in the service layer)
			assert.Greater(t, metrics.Metrics[0].AggregatedValues[0].Value, 0.0)
		case <-time.After(2 * time.Second):
			t.Fatal("Did not receive initial update within timeout")
		}

		// Cancel context to stop stream
		cancel()

		// Channel should eventually close
		select {
		case _, ok := <-updateChan:
			assert.False(t, ok, "Channel should be closed after context cancellation")
		case <-time.After(2 * time.Second):
			t.Fatal("Channel did not close after context cancellation")
		}
	})

	t.Run("handles GetCombinedMetrics error on initial update", func(t *testing.T) {
		mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
		mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
		mockScheduler := mock.NewMockUpdateScheduler(ctrl)
		mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

		mockDeviceStore.EXPECT().GetMinerStateCounts(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(&telemetryv1.MinerStateCounts{}, nil).AnyTimes()

		service := NewTelemetryService(Config{
			StalenessThreshold: 1 * time.Minute,
			FetchInterval:      10 * time.Second,
			ConcurrencyLimit:   5,
		}, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

		query := models.StreamCombinedMetricsQuery{
			DeviceIDs:        []models.DeviceIdentifier{"device1"},
			MeasurementTypes: []models.MeasurementType{models.MeasurementTypeHashrate},
			AggregationTypes: []models.AggregationType{models.AggregationTypeAverage},
			Granularity:      1 * time.Minute,
			UpdateInterval:   1 * time.Minute,
		}

		// Mock GetCombinedMetrics to return error
		mockDataStore.EXPECT().
			GetCombinedMetrics(gomock.Any(), gomock.Any()).
			Return(models.CombinedMetric{}, errors.New("database error")).
			Times(1)

		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()

		updateChan, err := service.StreamCombinedMetrics(ctx, query)
		require.NoError(t, err)
		require.NotNil(t, updateChan)

		// Channel should close due to error
		select {
		case _, ok := <-updateChan:
			assert.False(t, ok, "Channel should be closed after error")
		case <-time.After(2 * time.Second):
			t.Fatal("Channel did not close after error")
		}
	})

	t.Run("sends multiple updates over time", func(t *testing.T) {
		mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
		mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
		mockScheduler := mock.NewMockUpdateScheduler(ctrl)
		mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

		mockDeviceStore.EXPECT().GetMinerStateCounts(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(&telemetryv1.MinerStateCounts{}, nil).AnyTimes()

		service := NewTelemetryService(Config{
			StalenessThreshold: 1 * time.Minute,
			FetchInterval:      10 * time.Second,
			ConcurrencyLimit:   5,
		}, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

		// Use short intervals for testing
		shortInterval := 200 * time.Millisecond

		query := models.StreamCombinedMetricsQuery{
			DeviceIDs:        []models.DeviceIdentifier{"device1"},
			MeasurementTypes: []models.MeasurementType{models.MeasurementTypeHashrate},
			AggregationTypes: []models.AggregationType{models.AggregationTypeAverage},
			Granularity:      shortInterval,
			UpdateInterval:   shortInterval,
		}

		expectedMetrics := models.CombinedMetric{
			Metrics: []models.Metric{
				{
					MeasurementType: models.MeasurementTypeHashrate,
					AggregatedValues: []models.AggregatedValue{
						{Type: models.AggregationTypeAverage, Value: 100.0},
					},
					OpenTime: time.Now(),
				},
			},
		}

		// Expect multiple calls to GetCombinedMetrics (initial + aligned + at least 2 periodic)
		mockDataStore.EXPECT().
			GetCombinedMetrics(gomock.Any(), gomock.Any()).
			Return(expectedMetrics, nil).
			MinTimes(3)

		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()

		updateChan, err := service.StreamCombinedMetrics(ctx, query)
		require.NoError(t, err)
		require.NotNil(t, updateChan)

		// Receive multiple updates
		updateCount := 0
		timeout := time.After(1 * time.Second)

	receiveLoop:
		for {
			select {
			case metrics, ok := <-updateChan:
				if !ok {
					break receiveLoop
				}
				updateCount++
				assert.Len(t, metrics.Metrics, 1)
				if updateCount >= 3 {
					// We've received enough updates to verify periodic behavior
					cancel()
				}
			case <-timeout:
				break receiveLoop
			}
		}

		assert.GreaterOrEqual(t, updateCount, 3, "Should receive at least 3 updates (initial + aligned + periodic)")
	})

	t.Run("uses default update interval when not specified", func(t *testing.T) {
		mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
		mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
		mockScheduler := mock.NewMockUpdateScheduler(ctrl)
		mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

		mockDeviceStore.EXPECT().GetMinerStateCounts(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(&telemetryv1.MinerStateCounts{}, nil).AnyTimes()

		service := NewTelemetryService(Config{
			StalenessThreshold: 1 * time.Minute,
			FetchInterval:      10 * time.Second,
			ConcurrencyLimit:   5,
		}, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

		query := models.StreamCombinedMetricsQuery{
			DeviceIDs:        []models.DeviceIdentifier{"device1"},
			MeasurementTypes: []models.MeasurementType{models.MeasurementTypeHashrate},
			AggregationTypes: []models.AggregationType{models.AggregationTypeAverage},
			Granularity:      0, // Not specified
			UpdateInterval:   0, // Not specified
		}

		expectedMetrics := models.CombinedMetric{
			Metrics: []models.Metric{},
		}

		mockDataStore.EXPECT().
			GetCombinedMetrics(gomock.Any(), gomock.Any()).
			Return(expectedMetrics, nil).
			Times(1)

		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()

		updateChan, err := service.StreamCombinedMetrics(ctx, query)
		require.NoError(t, err)
		require.NotNil(t, updateChan)

		// Should still receive initial update with default interval
		select {
		case _, ok := <-updateChan:
			require.True(t, ok, "Channel should not be closed immediately")
		case <-time.After(2 * time.Second):
			t.Fatal("Did not receive initial update within timeout")
		}

		cancel()
	})

	t.Run("handles empty device list", func(t *testing.T) {
		mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
		mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
		mockScheduler := mock.NewMockUpdateScheduler(ctrl)
		mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

		mockDeviceStore.EXPECT().GetMinerStateCounts(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(&telemetryv1.MinerStateCounts{}, nil).AnyTimes()

		service := NewTelemetryService(Config{
			StalenessThreshold: 1 * time.Minute,
			FetchInterval:      10 * time.Second,
			ConcurrencyLimit:   5,
		}, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

		query := models.StreamCombinedMetricsQuery{
			DeviceIDs:        []models.DeviceIdentifier{}, // Empty device list
			MeasurementTypes: []models.MeasurementType{models.MeasurementTypeHashrate},
			AggregationTypes: []models.AggregationType{models.AggregationTypeAverage},
			Granularity:      1 * time.Minute,
			UpdateInterval:   1 * time.Minute,
		}

		expectedMetrics := models.CombinedMetric{
			Metrics: []models.Metric{}, // Empty metrics
		}

		mockDataStore.EXPECT().
			GetCombinedMetrics(gomock.Any(), gomock.Any()).
			Return(expectedMetrics, nil).
			Times(1)

		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()

		updateChan, err := service.StreamCombinedMetrics(ctx, query)
		require.NoError(t, err)
		require.NotNil(t, updateChan)

		// Should receive initial update even with empty device list
		select {
		case metrics, ok := <-updateChan:
			require.True(t, ok, "Channel should not be closed")
			assert.Empty(t, metrics.Metrics, "Metrics should be empty for empty device list")
		case <-time.After(2 * time.Second):
			t.Fatal("Did not receive initial update within timeout")
		}

		cancel()
	})
}

// Tests for pollErrorsForDevice integration with ErrorPoller

func TestPollErrorsForDevice_WithValidMiner_ShouldCallPollErrors(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
	mockErrorPoller := mock.NewMockErrorPoller(ctrl)
	mockMiner := minerMocks.NewMockMiner(ctrl)

	deviceID := models.DeviceIdentifier("test-device-123")

	// Expect miner lookup to succeed
	mockMinerGetter.EXPECT().
		GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).
		Return(mockMiner, nil)

	// Expect PollErrors to be called with the miner
	mockErrorPoller.EXPECT().
		PollErrors(gomock.Any(), mockMiner).
		Return(diagnostics.PollResult{MinersProcessed: 1, ErrorsUpserted: 2})

	config := Config{
		StalenessThreshold: 1 * time.Minute,
		FetchInterval:      10 * time.Second,
		ConcurrencyLimit:   5,
	}

	service := NewTelemetryService(config, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mockErrorPoller)

	device := models.Device{ID: deviceID}
	service.pollErrorsForDevice(t.Context(), device)
	// gomock verifies PollErrors was called
}

func TestPollErrorsForDevice_WhenMinerLookupFails_ShouldNotCallPollErrors(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
	mockErrorPoller := mock.NewMockErrorPoller(ctrl)

	deviceID := models.DeviceIdentifier("test-device-123")

	// Miner lookup fails
	mockMinerGetter.EXPECT().
		GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).
		Return(nil, errors.New("miner not found"))

	// No expectations on mockErrorPoller - PollErrors should NOT be called

	config := Config{
		StalenessThreshold: 1 * time.Minute,
		FetchInterval:      10 * time.Second,
		ConcurrencyLimit:   5,
	}

	service := NewTelemetryService(config, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mockErrorPoller)

	device := models.Device{ID: deviceID}
	service.pollErrorsForDevice(t.Context(), device)
	// gomock verifies PollErrors was NOT called (no expectations set)
}

func TestPollErrorsForDevice_WithUpsertFailures_ShouldComplete(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
	mockErrorPoller := mock.NewMockErrorPoller(ctrl)
	mockMiner := minerMocks.NewMockMiner(ctrl)

	deviceID := models.DeviceIdentifier("test-device-456")

	// Miner lookup succeeds
	mockMinerGetter.EXPECT().
		GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).
		Return(mockMiner, nil)

	// PollErrors returns a result with some upsert failures
	mockErrorPoller.EXPECT().
		PollErrors(gomock.Any(), mockMiner).
		Return(diagnostics.PollResult{
			MinersProcessed: 1,
			ErrorsUpserted:  3,
			UpsertsFailed:   2,
		})

	config := Config{
		StalenessThreshold: 1 * time.Minute,
		FetchInterval:      10 * time.Second,
		ConcurrencyLimit:   5,
	}

	service := NewTelemetryService(config, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mockErrorPoller)

	device := models.Device{ID: deviceID}
	// Should complete without panic even with upsert failures
	service.pollErrorsForDevice(t.Context(), device)
}

func TestIsConnectionError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "direct ConnectionError",
			err:      fleeterror.NewConnectionError("device-123", errors.New("connection refused")),
			expected: true,
		},
		{
			name:     "wrapped ConnectionError",
			err:      fmt.Errorf("failed to get status: %w", fleeterror.NewConnectionError("device-456", errors.New("timeout"))),
			expected: true,
		},
		{
			name:     "authentication error",
			err:      fleeterror.NewUnauthenticatedError("authentication failed"),
			expected: false,
		},
		{
			name:     "generic error",
			err:      errors.New("something went wrong"),
			expected: false,
		},
		{
			name:     "not found error",
			err:      fleeterror.NewNotFoundError("device not found"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := fleeterror.IsConnectionError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Tests for statusWriterRoutine batch operations

func TestStatusWriterRoutine_BatchFlushesOnInterval(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Arrange
	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

	deviceID := models.DeviceIdentifier("test-device-1")

	mockDeviceStore.EXPECT().
		GetDeviceStatusForDeviceIdentifiers(gomock.Any(), gomock.Any()).
		Return(map[models.DeviceIdentifier]mm.MinerStatus{}, nil).
		AnyTimes()

	mockDeviceStore.EXPECT().
		UpsertDeviceStatuses(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, updates []stores.DeviceStatusUpdate) error {
			require.Len(t, updates, 1)
			assert.Equal(t, deviceID, updates[0].DeviceIdentifier)
			assert.Equal(t, mm.MinerStatusActive, updates[0].Status)
			return nil
		}).
		Times(1)

	config := Config{
		StalenessThreshold:  1 * time.Minute,
		FetchInterval:       10 * time.Second,
		ConcurrencyLimit:    5,
		StatusFlushInterval: 50 * time.Millisecond,
	}

	service := NewTelemetryService(config, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	// Act
	go service.statusWriterRoutine(ctx)
	service.statusResults <- statusResult{
		deviceIdentifier: deviceID,
		status:           mm.MinerStatusActive,
	}

	// Assert - wait for flush interval to trigger (mock expectations verify the batch write)
	time.Sleep(100 * time.Millisecond)
	cancel()
	time.Sleep(50 * time.Millisecond)
}

func TestStatusWriterRoutine_BroadcastsStatusChanges(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Arrange
	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

	deviceID := models.DeviceIdentifier("test-device-1")

	mockDeviceStore.EXPECT().
		GetDeviceStatusForDeviceIdentifiers(gomock.Any(), gomock.Any()).
		Return(map[models.DeviceIdentifier]mm.MinerStatus{}, nil).
		AnyTimes()

	mockDeviceStore.EXPECT().
		UpsertDeviceStatuses(gomock.Any(), gomock.Any()).
		Return(nil).
		AnyTimes()

	config := Config{
		StalenessThreshold:  1 * time.Minute,
		FetchInterval:       10 * time.Second,
		ConcurrencyLimit:    5,
		StatusFlushInterval: 50 * time.Millisecond,
	}

	service := NewTelemetryService(config, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

	// Pre-populate in-memory state with OFFLINE so change to ACTIVE triggers broadcast
	service.lastKnownStatuses.Store(deviceID, mm.MinerStatusOffline)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	// Act
	go service.statusWriterRoutine(ctx)
	service.statusResults <- statusResult{
		deviceIdentifier: deviceID,
		status:           mm.MinerStatusActive,
	}

	// Assert - wait for flush interval to trigger broadcast
	time.Sleep(100 * time.Millisecond)
	cancel()
	time.Sleep(50 * time.Millisecond)
}

func TestStatusWriterRoutine_FlushesOnContextCancel(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Arrange
	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

	deviceID := models.DeviceIdentifier("test-device-1")

	mockDeviceStore.EXPECT().
		GetDeviceStatusForDeviceIdentifiers(gomock.Any(), gomock.Any()).
		Return(map[models.DeviceIdentifier]mm.MinerStatus{}, nil).
		AnyTimes()

	mockDeviceStore.EXPECT().
		UpsertDeviceStatuses(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, updates []stores.DeviceStatusUpdate) error {
			require.Len(t, updates, 1)
			assert.Equal(t, deviceID, updates[0].DeviceIdentifier)
			return nil
		}).
		Times(1)

	config := Config{
		StalenessThreshold:  1 * time.Minute,
		FetchInterval:       10 * time.Second,
		ConcurrencyLimit:    5,
		StatusFlushInterval: 10 * time.Second, // Long interval so flush happens on cancel
	}

	service := NewTelemetryService(config, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

	ctx, cancel := context.WithCancel(t.Context())

	done := make(chan struct{})
	go func() {
		service.statusWriterRoutine(ctx)
		close(done)
	}()

	// Act
	service.statusResults <- statusResult{
		deviceIdentifier: deviceID,
		status:           mm.MinerStatusActive,
	}
	time.Sleep(20 * time.Millisecond) // Ensure result is received
	cancel()                          // Trigger final flush

	// Assert
	select {
	case <-done:
		// Success - routine finished and flushed
	case <-time.After(1 * time.Second):
		t.Fatal("statusWriterRoutine did not finish after context cancel")
	}
}

// Tests for metricsWriterRoutine batch operations

func TestMetricsWriterRoutine_FlushesOnInterval(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Arrange
	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

	metric := modelsV2.DeviceMetrics{DeviceIdentifier: "test-device-1"}

	mockDataStore.EXPECT().
		StoreDeviceMetrics(gomock.Any(), metric).
		Return(nil).
		Times(1)

	config := Config{
		StalenessThreshold:  1 * time.Minute,
		FetchInterval:       10 * time.Second,
		ConcurrencyLimit:    5,
		StatusFlushInterval: 50 * time.Millisecond,
	}

	service := NewTelemetryService(config, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	// Act
	go service.metricsWriterRoutine(ctx)
	service.metricsResults <- metricsResult{metrics: metric}

	// Assert - wait for flush interval to trigger (mock expectations verify the batch write)
	time.Sleep(100 * time.Millisecond)
	cancel()
	time.Sleep(50 * time.Millisecond)
}

func TestMetricsWriterRoutine_FlushesOnContextCancel(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Arrange
	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

	metric := modelsV2.DeviceMetrics{DeviceIdentifier: "test-device-1"}

	mockDataStore.EXPECT().
		StoreDeviceMetrics(gomock.Any(), metric).
		Return(nil).
		Times(1)

	config := Config{
		StalenessThreshold:  1 * time.Minute,
		FetchInterval:       10 * time.Second,
		ConcurrencyLimit:    5,
		StatusFlushInterval: 10 * time.Second, // Long interval so flush happens on cancel
	}

	service := NewTelemetryService(config, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

	ctx, cancel := context.WithCancel(t.Context())

	done := make(chan struct{})
	go func() {
		service.metricsWriterRoutine(ctx)
		close(done)
	}()

	// Act
	service.metricsResults <- metricsResult{metrics: metric}
	time.Sleep(20 * time.Millisecond) // Ensure result is received
	cancel()                          // Trigger final flush

	// Assert
	select {
	case <-done:
		// Success - routine finished and flushed
	case <-time.After(1 * time.Second):
		t.Fatal("metricsWriterRoutine did not finish after context cancel")
	}
}

func TestMetricsWriterRoutine_DrainsChannelOnContextCancel(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Arrange
	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

	metric1 := modelsV2.DeviceMetrics{DeviceIdentifier: "test-device-1"}
	metric2 := modelsV2.DeviceMetrics{DeviceIdentifier: "test-device-2"}

	// Expect a single batch write containing both metrics
	mockDataStore.EXPECT().
		StoreDeviceMetrics(gomock.Any(), metric1, metric2).
		Return(nil).
		Times(1)

	config := Config{
		StalenessThreshold:  1 * time.Minute,
		FetchInterval:       10 * time.Second,
		ConcurrencyLimit:    5,
		StatusFlushInterval: 10 * time.Second, // Long interval so neither metric is flushed early
	}

	service := NewTelemetryService(config, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

	ctx, cancel := context.WithCancel(t.Context())

	done := make(chan struct{})
	go func() {
		service.metricsWriterRoutine(ctx)
		close(done)
	}()

	// Act - send first metric via channel so the routine consumes it, then queue the
	// second directly in the buffered channel so it can only be written via the drain.
	service.metricsResults <- metricsResult{metrics: metric1}
	time.Sleep(20 * time.Millisecond) // Let routine pick up metric1 into pending
	service.metricsResults <- metricsResult{metrics: metric2}
	cancel() // Trigger drain + flush

	// Assert
	select {
	case <-done:
		// Success - routine drained channel and flushed both metrics
	case <-time.After(1 * time.Second):
		t.Fatal("metricsWriterRoutine did not finish after context cancel")
	}
}

func TestMetricsWriterRoutine_RetriesIndividuallyOnBatchError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Arrange
	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

	metric1 := modelsV2.DeviceMetrics{DeviceIdentifier: "test-device-1"}
	metric2 := modelsV2.DeviceMetrics{DeviceIdentifier: "test-device-2"}
	batchErr := errors.New("batch write failed")

	// Batch call fails
	mockDataStore.EXPECT().
		StoreDeviceMetrics(gomock.Any(), metric1, metric2).
		Return(batchErr).
		Times(1)

	// Individual retries succeed
	mockDataStore.EXPECT().
		StoreDeviceMetrics(gomock.Any(), metric1).
		Return(nil).
		Times(1)
	mockDataStore.EXPECT().
		StoreDeviceMetrics(gomock.Any(), metric2).
		Return(nil).
		Times(1)

	config := Config{
		StalenessThreshold:  1 * time.Minute,
		FetchInterval:       10 * time.Second,
		ConcurrencyLimit:    5,
		StatusFlushInterval: 50 * time.Millisecond,
	}

	service := NewTelemetryService(config, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	// Act
	go service.metricsWriterRoutine(ctx)
	service.metricsResults <- metricsResult{metrics: metric1}
	service.metricsResults <- metricsResult{metrics: metric2}

	// Assert - wait for flush interval to trigger batch + individual retries
	time.Sleep(100 * time.Millisecond)
	cancel()
	time.Sleep(50 * time.Millisecond)
}

// Tests for processStatusOnly failed device recovery

func TestProcessStatusOnly_RecoversFailedDevice(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Arrange
	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
	mockMiner := minerMocks.NewMockMiner(ctrl)

	deviceID := models.DeviceIdentifier("failed-device-123")
	failedAt := time.Now().Add(-5 * time.Minute)

	mockMinerGetter.EXPECT().
		GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).
		Return(mockMiner, nil)

	mockMiner.EXPECT().
		GetDeviceStatus(gomock.Any()).
		Return(mm.MinerStatusActive, nil)

	mockScheduler.EXPECT().
		IsFailedDevice(gomock.Any(), deviceID).
		Return(true, failedAt, nil)

	mockScheduler.EXPECT().
		AddDevices(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, devices ...models.Device) error {
			require.Len(t, devices, 1)
			assert.Equal(t, deviceID, devices[0].ID)
			assert.Equal(t, failedAt, devices[0].LastUpdatedAt)
			return nil
		}).
		Return(nil)

	config := Config{
		StalenessThreshold: 1 * time.Minute,
		FetchInterval:      10 * time.Second,
		ConcurrencyLimit:   5,
	}

	service := NewTelemetryService(config, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

	ctx := t.Context()
	device := models.Device{ID: deviceID}

	// Drain the status results channel
	go func() {
		select {
		case <-service.statusResults:
		case <-time.After(1 * time.Second):
		}
	}()

	// Act
	service.processStatusOnly(ctx, device)

	// Assert - mock expectations verify AddDevices was called with recovered device
}

func TestProcessStatusOnly_DoesNotRecoverNonFailedDevice(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Arrange
	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
	mockMiner := minerMocks.NewMockMiner(ctrl)

	deviceID := models.DeviceIdentifier("normal-device-123")

	mockMinerGetter.EXPECT().
		GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).
		Return(mockMiner, nil)

	mockMiner.EXPECT().
		GetDeviceStatus(gomock.Any()).
		Return(mm.MinerStatusActive, nil)

	mockScheduler.EXPECT().
		IsFailedDevice(gomock.Any(), deviceID).
		Return(false, time.Time{}, nil)

	// NOTE: AddDevices should NOT be called since device was not failed

	config := Config{
		StalenessThreshold: 1 * time.Minute,
		FetchInterval:      10 * time.Second,
		ConcurrencyLimit:   5,
	}

	service := NewTelemetryService(config, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

	ctx := t.Context()
	device := models.Device{ID: deviceID}

	// Drain the status results channel
	go func() {
		select {
		case <-service.statusResults:
		case <-time.After(1 * time.Second):
		}
	}()

	// Act
	service.processStatusOnly(ctx, device)

	// Assert - mock expectations verify AddDevices was NOT called
}

func TestProcessStatusOnly_ConnectionError_SetsStatusOffline(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Arrange
	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
	mockMiner := minerMocks.NewMockMiner(ctrl)

	deviceID := models.DeviceIdentifier("offline-device-123")

	mockMinerGetter.EXPECT().
		GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).
		Return(mockMiner, nil)

	mockMiner.EXPECT().
		GetDeviceStatus(gomock.Any()).
		Return(mm.MinerStatusUnknown, fleeterror.NewConnectionError(string(deviceID), errors.New("connection refused")))

	// Note: IsFailedDevice is NOT called because offline devices skip recovery.
	// This prevents re-adding unreachable devices to the scheduler where they'd just fail again.

	config := Config{
		StalenessThreshold: 1 * time.Minute,
		FetchInterval:      10 * time.Second,
		ConcurrencyLimit:   5,
	}

	service := NewTelemetryService(config, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

	ctx := t.Context()
	device := models.Device{ID: deviceID}

	var receivedResult statusResult
	go func() {
		select {
		case receivedResult = <-service.statusResults:
		case <-time.After(1 * time.Second):
		}
	}()

	// Act
	service.processStatusOnly(ctx, device)
	time.Sleep(50 * time.Millisecond)

	// Assert - status is still written to DB for UI visibility
	assert.Equal(t, deviceID, receivedResult.deviceIdentifier)
	assert.Equal(t, mm.MinerStatusOffline, receivedResult.status)
}

// Tests for non-blocking channel sends

func TestProcessDevice_NonBlockingSend_DropsUpdateWhenChannelFull(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Arrange
	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
	mockMiner := minerMocks.NewMockMiner(ctrl)
	mockErrorPoller := mock.NewMockErrorPoller(ctrl)

	deviceID := models.DeviceIdentifier("test-device")
	device := models.Device{ID: deviceID, LastUpdatedAt: time.Now().Add(-1 * time.Minute)}

	mockMinerGetter.EXPECT().
		GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).
		Return(mockMiner, nil).
		Times(2) // Telemetry and error polling (status derived from metrics health, no extra RPC)

	mockMiner.EXPECT().
		GetDeviceMetrics(gomock.Any()).
		Return(modelsV2.DeviceMetrics{
			DeviceIdentifier: string(deviceID),
			Timestamp:        time.Now(),
			Health:           modelsV2.HealthHealthyActive,
		}, nil)

	mockScheduler.EXPECT().
		AddDevices(gomock.Any(), gomock.Any()).
		Return(nil)

	mockErrorPoller.EXPECT().
		PollErrors(gomock.Any(), mockMiner).
		Return(diagnostics.PollResult{})

	config := Config{
		StalenessThreshold: 1 * time.Minute,
		FetchInterval:      10 * time.Second,
		ConcurrencyLimit:   1,
		MetricTimeout:      5 * time.Second,
	}

	service := &TelemetryService{
		config:             config,
		telemetryDataStore: mockDataStore,
		minerManager:       mockMinerGetter,
		updateScheduler:    mockScheduler,
		deviceStore:        mockDeviceStore,
		errorPoller:        mockErrorPoller,
		tasks:              make(chan models.Device, 1),
		statusTasks:        make(chan models.Device, 1),
		statusResults:      make(chan statusResult, 1), // Small buffer to test non-blocking
		metricsResults:     make(chan metricsResult, 1),
		lookBackDuration:   -1 * (config.StalenessThreshold - config.FetchInterval),
	}

	// Fill the channel to force non-blocking send path
	service.statusResults <- statusResult{deviceIdentifier: "blocker", status: mm.MinerStatusActive}

	ctx := t.Context()

	done := make(chan struct{})
	go func() {
		service.processDevice(ctx, device)
		close(done)
	}()

	// Act & Assert - processDevice should complete without blocking
	select {
	case <-done:
		// Success - processDevice completed without blocking
	case <-time.After(2 * time.Second):
		t.Fatal("processDevice blocked on full channel - non-blocking send not working")
	}
}

// Tests for status derivation logic in processDevice

// TestProcessDevice_HealthHealthyInactive_CallsGetDeviceStatus verifies that when
// metrics succeed but Health == HealthHealthyInactive, processDevice falls back to
// GetDeviceStatus (because the V2 model collapses NeedsMiningPool into Inactive).
func TestProcessDevice_HealthHealthyInactive_CallsGetDeviceStatus(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Arrange
	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
	mockMiner := minerMocks.NewMockMiner(ctrl)
	mockErrorPoller := mock.NewMockErrorPoller(ctrl)

	deviceID := models.DeviceIdentifier("inactive-device")
	device := models.Device{ID: deviceID, LastUpdatedAt: time.Now().Add(-1 * time.Minute)}

	// Telemetry fetch (fetchTelemetryFromMiner) and error polling each call GetMinerFromDeviceIdentifier.
	// Status fetch (fetchStatusFromMiner) also calls it, so three calls total.
	mockMinerGetter.EXPECT().
		GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).
		Return(mockMiner, nil).
		Times(3)

	mockMiner.EXPECT().
		GetDeviceMetrics(gomock.Any()).
		Return(modelsV2.DeviceMetrics{
			DeviceIdentifier: string(deviceID),
			Timestamp:        time.Now(),
			Health:           modelsV2.HealthHealthyInactive,
		}, nil)

	mockScheduler.EXPECT().
		AddDevices(gomock.Any(), gomock.Any()).
		Return(nil)

	// GetDeviceStatus must be called because hasMetricsStatus == false for HealthHealthyInactive.
	mockMiner.EXPECT().
		GetDeviceStatus(gomock.Any()).
		Return(mm.MinerStatusNeedsMiningPool, nil)

	mockErrorPoller.EXPECT().
		PollErrors(gomock.Any(), mockMiner).
		Return(diagnostics.PollResult{})

	config := Config{
		StalenessThreshold: 1 * time.Minute,
		FetchInterval:      10 * time.Second,
		ConcurrencyLimit:   1,
		MetricTimeout:      5 * time.Second,
	}

	service := &TelemetryService{
		config:             config,
		telemetryDataStore: mockDataStore,
		minerManager:       mockMinerGetter,
		updateScheduler:    mockScheduler,
		deviceStore:        mockDeviceStore,
		errorPoller:        mockErrorPoller,
		tasks:              make(chan models.Device, 1),
		statusTasks:        make(chan models.Device, 1),
		statusResults:      make(chan statusResult, 1),
		metricsResults:     make(chan metricsResult, 1),
		lookBackDuration:   -1 * (config.StalenessThreshold - config.FetchInterval),
	}

	ctx := t.Context()
	go func() {
		select {
		case <-service.statusResults:
		case <-time.After(1 * time.Second):
		}
	}()

	// Act
	service.processDevice(ctx, device)

	// Assert — mock expectations verify GetDeviceStatus was called exactly once.
}

// TestProcessDevice_HealthHealthyActive_SkipsGetDeviceStatus verifies that when
// metrics succeed with HealthHealthyActive, processDevice derives status from health
// and does NOT make a redundant GetDeviceStatus RPC call.
func TestProcessDevice_HealthHealthyActive_SkipsGetDeviceStatus(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Arrange
	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
	mockMiner := minerMocks.NewMockMiner(ctrl)
	mockErrorPoller := mock.NewMockErrorPoller(ctrl)

	deviceID := models.DeviceIdentifier("active-device")
	device := models.Device{ID: deviceID, LastUpdatedAt: time.Now().Add(-1 * time.Minute)}

	// Telemetry fetch and error polling each call GetMinerFromDeviceIdentifier — two calls total.
	// GetDeviceStatus must NOT be called when hasMetricsStatus == true.
	mockMinerGetter.EXPECT().
		GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).
		Return(mockMiner, nil).
		Times(2)

	mockMiner.EXPECT().
		GetDeviceMetrics(gomock.Any()).
		Return(modelsV2.DeviceMetrics{
			DeviceIdentifier: string(deviceID),
			Timestamp:        time.Now(),
			Health:           modelsV2.HealthHealthyActive,
		}, nil)

	mockScheduler.EXPECT().
		AddDevices(gomock.Any(), gomock.Any()).
		Return(nil)

	// GetDeviceStatus must NOT be called (gomock will fail the test if it is).

	mockErrorPoller.EXPECT().
		PollErrors(gomock.Any(), mockMiner).
		Return(diagnostics.PollResult{})

	config := Config{
		StalenessThreshold: 1 * time.Minute,
		FetchInterval:      10 * time.Second,
		ConcurrencyLimit:   1,
		MetricTimeout:      5 * time.Second,
	}

	service := &TelemetryService{
		config:             config,
		telemetryDataStore: mockDataStore,
		minerManager:       mockMinerGetter,
		updateScheduler:    mockScheduler,
		deviceStore:        mockDeviceStore,
		errorPoller:        mockErrorPoller,
		tasks:              make(chan models.Device, 1),
		statusTasks:        make(chan models.Device, 1),
		statusResults:      make(chan statusResult, 1),
		metricsResults:     make(chan metricsResult, 1),
		lookBackDuration:   -1 * (config.StalenessThreshold - config.FetchInterval),
	}

	ctx := t.Context()

	var receivedResult statusResult
	go func() {
		select {
		case receivedResult = <-service.statusResults:
		case <-time.After(1 * time.Second):
		}
	}()

	// Act
	service.processDevice(ctx, device)
	time.Sleep(50 * time.Millisecond)

	// Assert — status was derived from metrics health, no GetDeviceStatus call.
	assert.Equal(t, deviceID, receivedResult.deviceIdentifier)
	assert.Equal(t, mm.MinerStatusActive, receivedResult.status)
}

// TestProcessDevice_MetricsFail_CallsGetDeviceStatus verifies that when metrics fetch
// fails, processDevice falls back to GetDeviceStatus to determine the device's status.
func TestProcessDevice_MetricsFail_CallsGetDeviceStatus(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Arrange
	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
	mockMiner := minerMocks.NewMockMiner(ctrl)
	mockErrorPoller := mock.NewMockErrorPoller(ctrl)

	deviceID := models.DeviceIdentifier("metrics-fail-device")
	device := models.Device{ID: deviceID, LastUpdatedAt: time.Now().Add(-1 * time.Minute)}

	// Telemetry fetch, status fetch, and error polling each call GetMinerFromDeviceIdentifier.
	mockMinerGetter.EXPECT().
		GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).
		Return(mockMiner, nil).
		Times(3)

	mockMiner.EXPECT().
		GetDeviceMetrics(gomock.Any()).
		Return(modelsV2.DeviceMetrics{}, errors.New("metrics unavailable"))

	mockScheduler.EXPECT().
		AddDevices(gomock.Any(), gomock.Any()).
		Return(nil)

	// GetDeviceStatus must be called because metricsErr != nil (hasMetricsStatus == false).
	mockMiner.EXPECT().
		GetDeviceStatus(gomock.Any()).
		Return(mm.MinerStatusActive, nil)

	mockErrorPoller.EXPECT().
		PollErrors(gomock.Any(), mockMiner).
		Return(diagnostics.PollResult{})

	config := Config{
		StalenessThreshold: 1 * time.Minute,
		FetchInterval:      10 * time.Second,
		ConcurrencyLimit:   1,
		MetricTimeout:      5 * time.Second,
	}

	service := &TelemetryService{
		config:             config,
		telemetryDataStore: mockDataStore,
		minerManager:       mockMinerGetter,
		updateScheduler:    mockScheduler,
		deviceStore:        mockDeviceStore,
		errorPoller:        mockErrorPoller,
		tasks:              make(chan models.Device, 1),
		statusTasks:        make(chan models.Device, 1),
		statusResults:      make(chan statusResult, 1),
		lookBackDuration:   -1 * (config.StalenessThreshold - config.FetchInterval),
	}

	ctx := t.Context()
	go func() {
		select {
		case <-service.statusResults:
		case <-time.After(1 * time.Second):
		}
	}()

	// Act
	service.processDevice(ctx, device)

	// Assert — mock expectations verify GetDeviceStatus was called exactly once.
}

// Unit conversion test constants - raw storage values
// These tests verify that the service layer returns RAW values (H/s, W, J/H)
// and does NOT apply unit conversion. Conversion should happen in the handler layer.
const (
	// Raw hashrate: 100 TH/s = 100e12 H/s (storage unit)
	testRawHashrateHS = 100e12
	// Raw power: 3 kW = 3000 W (storage unit)
	testRawPowerW = 3000.0
	// Raw efficiency: 30 J/TH = 30e-12 J/H (storage unit)
	testRawEfficiencyJH = 30e-12
)

// TestService_GetCombinedMetrics_ReturnsRawValues verifies that GetCombinedMetrics
// returns values in raw storage units (H/s, W, J/H) WITHOUT applying conversion.
func TestService_GetCombinedMetrics_ReturnsRawValues(t *testing.T) {
	tests := []struct {
		name            string
		measurementType models.MeasurementType
		storeValue      float64
		expectedValue   float64
	}{
		{
			name:            "hashrate returns raw H/s (no conversion to TH/s)",
			measurementType: models.MeasurementTypeHashrate,
			storeValue:      testRawHashrateHS,
			expectedValue:   testRawHashrateHS,
		},
		{
			name:            "power returns raw W (no conversion to kW)",
			measurementType: models.MeasurementTypePower,
			storeValue:      testRawPowerW,
			expectedValue:   testRawPowerW,
		},
		{
			name:            "efficiency returns raw J/H (no conversion to J/TH)",
			measurementType: models.MeasurementTypeEfficiency,
			storeValue:      testRawEfficiencyJH,
			expectedValue:   testRawEfficiencyJH,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
			mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
			mockScheduler := mock.NewMockUpdateScheduler(ctrl)
			mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

			// Store returns raw values
			mockDataStore.EXPECT().GetCombinedMetrics(gomock.Any(), gomock.Any()).
				Return(models.CombinedMetric{
					Metrics: []models.Metric{
						{
							MeasurementType: tt.measurementType,
							AggregatedValues: []models.AggregatedValue{
								{Type: models.AggregationTypeSum, Value: tt.storeValue},
							},
							OpenTime: time.Now(),
						},
					},
				}, nil)

			service := NewTelemetryService(Config{}, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

			query := models.CombinedMetricsQuery{
				DeviceIDs:        []models.DeviceIdentifier{"device1"},
				MeasurementTypes: []models.MeasurementType{tt.measurementType},
				AggregationTypes: []models.AggregationType{models.AggregationTypeSum},
			}

			result, err := service.GetCombinedMetrics(t.Context(), query)

			require.NoError(t, err)
			require.Len(t, result.Metrics, 1)
			require.Len(t, result.Metrics[0].AggregatedValues, 1)
			assert.InDelta(t, tt.expectedValue, result.Metrics[0].AggregatedValues[0].Value, 1e-20,
				"Service should return raw value %v, but got %v (conversion should happen in handler)",
				tt.expectedValue, result.Metrics[0].AggregatedValues[0].Value)
		})
	}
}

func TestPersistFirmwareVersionIfChanged(t *testing.T) {
	const deviceID = models.DeviceIdentifier("device-1")
	const firmwareV1 = "1.2.3"
	const firmwareV2 = "1.2.4"

	t.Run("skips ambiguous empty firmware version from telemetry", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
		service := NewTelemetryService(Config{ConcurrencyLimit: 1}, nil, nil, nil, mockDeviceStore, nil)

		service.persistFirmwareVersionIfChanged(t.Context(), deviceID, "")
	})

	t.Run("persists new firmware version", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
		mockDeviceStore.EXPECT().
			UpdateFirmwareVersion(gomock.Any(), deviceID, firmwareV1).
			Return(nil)

		service := NewTelemetryService(Config{ConcurrencyLimit: 1}, nil, nil, nil, mockDeviceStore, nil)

		service.persistFirmwareVersionIfChanged(t.Context(), deviceID, firmwareV1)
	})

	t.Run("skips when firmware version unchanged", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
		mockDeviceStore.EXPECT().
			UpdateFirmwareVersion(gomock.Any(), deviceID, firmwareV1).
			Return(nil)

		service := NewTelemetryService(Config{ConcurrencyLimit: 1}, nil, nil, nil, mockDeviceStore, nil)

		// First call persists
		service.persistFirmwareVersionIfChanged(t.Context(), deviceID, firmwareV1)
		// Second call with same version should not call UpdateFirmwareVersion again
		service.persistFirmwareVersionIfChanged(t.Context(), deviceID, firmwareV1)
	})

	t.Run("persists when firmware version changes", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
		mockDeviceStore.EXPECT().
			UpdateFirmwareVersion(gomock.Any(), deviceID, firmwareV1).
			Return(nil)
		mockDeviceStore.EXPECT().
			UpdateFirmwareVersion(gomock.Any(), deviceID, firmwareV2).
			Return(nil)

		service := NewTelemetryService(Config{ConcurrencyLimit: 1}, nil, nil, nil, mockDeviceStore, nil)

		service.persistFirmwareVersionIfChanged(t.Context(), deviceID, firmwareV1)
		service.persistFirmwareVersionIfChanged(t.Context(), deviceID, firmwareV2)
	})

	t.Run("does not cache on store error", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
		mockDeviceStore.EXPECT().
			UpdateFirmwareVersion(gomock.Any(), deviceID, firmwareV1).
			Return(fmt.Errorf("db error"))
		// Retry should call UpdateFirmwareVersion again since previous failed
		mockDeviceStore.EXPECT().
			UpdateFirmwareVersion(gomock.Any(), deviceID, firmwareV1).
			Return(nil)

		service := NewTelemetryService(Config{ConcurrencyLimit: 1}, nil, nil, nil, mockDeviceStore, nil)

		service.persistFirmwareVersionIfChanged(t.Context(), deviceID, firmwareV1)
		service.persistFirmwareVersionIfChanged(t.Context(), deviceID, firmwareV1)
	})
}

func TestSendCombinedMetricUpdate_DeviceScopedMinerStateCounts(t *testing.T) {
	t.Run("non-empty DeviceIDs passes MinerFilter with those identifiers", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
		mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

		service := NewTelemetryService(Config{
			StalenessThreshold: 1 * time.Minute,
			FetchInterval:      10 * time.Second,
			ConcurrencyLimit:   5,
		}, mockDataStore, nil, nil, mockDeviceStore, nil)

		deviceIDs := []models.DeviceIdentifier{"device-a", "device-b"}

		query := models.StreamCombinedMetricsQuery{
			DeviceIDs:      deviceIDs,
			Granularity:    5 * time.Minute,
			UpdateInterval: 5 * time.Minute,
			OrganizationID: 42,
		}

		// GetCombinedMetrics returns empty metrics
		mockDataStore.EXPECT().
			GetCombinedMetrics(gomock.Any(), gomock.Any()).
			Return(models.CombinedMetric{Metrics: []models.Metric{}}, nil)

		// Expect GetMinerStateCounts called with a MinerFilter containing exactly those device IDs
		expectedFilter := &stores.MinerFilter{
			DeviceIdentifiers: []string{"device-a", "device-b"},
		}
		mockDeviceStore.EXPECT().
			GetMinerStateCounts(gomock.Any(), int64(42), expectedFilter).
			Return(&telemetryv1.MinerStateCounts{
				HashingCount: 1,
				BrokenCount:  1,
			}, nil)

		updateChan := make(chan models.CombinedMetric, 1)
		err := service.sendCombinedMetricUpdate(t.Context(), updateChan, query, 5*time.Minute)
		require.NoError(t, err)

		result := <-updateChan
		require.NotNil(t, result.MinerStateCounts)
		assert.Equal(t, int32(1), result.MinerStateCounts.Hashing)
		assert.Equal(t, int32(1), result.MinerStateCounts.Broken)
	})

	t.Run("empty DeviceIDs passes nil MinerFilter", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
		mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

		service := NewTelemetryService(Config{
			StalenessThreshold: 1 * time.Minute,
			FetchInterval:      10 * time.Second,
			ConcurrencyLimit:   5,
		}, mockDataStore, nil, nil, mockDeviceStore, nil)

		query := models.StreamCombinedMetricsQuery{
			DeviceIDs:      nil,
			Granularity:    5 * time.Minute,
			UpdateInterval: 5 * time.Minute,
			OrganizationID: 42,
		}

		mockDataStore.EXPECT().
			GetCombinedMetrics(gomock.Any(), gomock.Any()).
			Return(models.CombinedMetric{Metrics: []models.Metric{}}, nil)

		// Expect nil filter when no device IDs provided
		mockDeviceStore.EXPECT().
			GetMinerStateCounts(gomock.Any(), int64(42), nil).
			Return(&telemetryv1.MinerStateCounts{
				HashingCount:  5,
				BrokenCount:   2,
				OfflineCount:  1,
				SleepingCount: 3,
			}, nil)

		updateChan := make(chan models.CombinedMetric, 1)
		err := service.sendCombinedMetricUpdate(t.Context(), updateChan, query, 5*time.Minute)
		require.NoError(t, err)

		result := <-updateChan
		require.NotNil(t, result.MinerStateCounts)
		assert.Equal(t, int32(5), result.MinerStateCounts.Hashing)
		assert.Equal(t, int32(2), result.MinerStateCounts.Broken)
		assert.Equal(t, int32(1), result.MinerStateCounts.Offline)
		assert.Equal(t, int32(3), result.MinerStateCounts.Sleeping)
	})
}

// runStatusPollingOnce runs statusPollingRoutine until it fires the ticker once,
// then cancels. Returns the device IDs that were enqueued into statusTasks.
func runStatusPollingOnce(t *testing.T, service *TelemetryService) []models.DeviceIdentifier {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	// Short interval so the ticker fires immediately.
	service.config.DeviceStatusPollInterval = 5 * time.Millisecond

	done := make(chan struct{})
	go func() {
		defer close(done)
		service.statusPollingRoutine(ctx)
	}()

	// Drain statusTasks until the ticker has had time to fire and the goroutine exits.
	var enqueued []models.DeviceIdentifier
	timer := time.NewTimer(200 * time.Millisecond)
	defer timer.Stop()
	for {
		select {
		case device, ok := <-service.statusTasks:
			if !ok {
				return enqueued
			}
			enqueued = append(enqueued, device.ID)
		case <-timer.C:
			cancel()
			<-done
			// Drain any remaining items buffered before cancel.
			for {
				select {
				case device := <-service.statusTasks:
					enqueued = append(enqueued, device.ID)
				default:
					return enqueued
				}
			}
		}
	}
}

func newStatusPollingService(t *testing.T, ctrl *gomock.Controller, scheduler *mock.MockUpdateScheduler) *TelemetryService {
	t.Helper()
	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
	config := Config{
		StalenessThreshold: 1 * time.Minute,
		FetchInterval:      10 * time.Second,
		ConcurrencyLimit:   10,
	}
	svc := NewTelemetryService(config, mockDataStore, mockMinerGetter, scheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))
	return svc
}

func TestStatusPollingRoutine_SkipsActiveNonFailedDevice(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Arrange
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	deviceID := models.DeviceIdentifier("active-device")

	mockScheduler.EXPECT().
		IsFailedDevice(gomock.Any(), deviceID).
		Return(false, time.Time{}, nil).
		AnyTimes()

	service := newStatusPollingService(t, ctrl, mockScheduler)
	service.devicesForStatusPolling.Store(deviceID, struct{}{})
	service.lastKnownStatuses.Store(deviceID, mm.MinerStatusActive)

	// Act
	enqueued := runStatusPollingOnce(t, service)

	// Assert — active, non-failed device must not be polled
	assert.NotContains(t, enqueued, deviceID)
}

func TestStatusPollingRoutine_EnqueuesOfflineDevice(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Arrange
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	deviceID := models.DeviceIdentifier("offline-device")

	service := newStatusPollingService(t, ctrl, mockScheduler)
	service.devicesForStatusPolling.Store(deviceID, struct{}{})
	service.lastKnownStatuses.Store(deviceID, mm.MinerStatusOffline)

	// Act
	enqueued := runStatusPollingOnce(t, service)

	// Assert — offline device must be enqueued for recovery polling
	assert.Contains(t, enqueued, deviceID)
}

func TestStatusPollingRoutine_EnqueuesDeviceWithNoKnownStatus(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Arrange
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	deviceID := models.DeviceIdentifier("unseen-device")

	service := newStatusPollingService(t, ctrl, mockScheduler)
	service.devicesForStatusPolling.Store(deviceID, struct{}{})
	// no entry in lastKnownStatuses

	// Act
	enqueued := runStatusPollingOnce(t, service)

	// Assert — device never seen by the main loop must be polled
	assert.Contains(t, enqueued, deviceID)
}

func TestStatusPollingRoutine_EnqueuesFailedDeviceEvenIfCachedActive(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Arrange
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	deviceID := models.DeviceIdentifier("failed-but-cached-active")

	// Device is in the scheduler's failed set despite showing ACTIVE in the cache.
	mockScheduler.EXPECT().
		IsFailedDevice(gomock.Any(), deviceID).
		Return(true, time.Now().Add(-1*time.Minute), nil).
		AnyTimes()

	service := newStatusPollingService(t, ctrl, mockScheduler)
	service.devicesForStatusPolling.Store(deviceID, struct{}{})
	service.lastKnownStatuses.Store(deviceID, mm.MinerStatusActive)

	// Act
	enqueued := runStatusPollingOnce(t, service)

	// Assert — failed device must not be skipped, even with a cached ACTIVE status
	assert.Contains(t, enqueued, deviceID)
}

func TestStatusPollingRoutine_SkipsInFlightDevice(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Arrange
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	deviceID := models.DeviceIdentifier("inflight-device")

	service := newStatusPollingService(t, ctrl, mockScheduler)
	service.devicesForStatusPolling.Store(deviceID, struct{}{})
	// No cached status (would normally be enqueued), but already claimed in inFlight.
	service.inFlight.Store(deviceID, struct{}{})

	// Act
	enqueued := runStatusPollingOnce(t, service)

	// Assert — device already being processed by a worker must not be double-enqueued
	assert.NotContains(t, enqueued, deviceID)
}

func TestStatusPollingRoutine_MixedDevices(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Arrange
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)

	activeDevice := models.DeviceIdentifier("active")
	offlineDevice := models.DeviceIdentifier("offline")
	failedCachedActive := models.DeviceIdentifier("failed-cached-active")
	inFlightDevice := models.DeviceIdentifier("inflight")
	unseenDevice := models.DeviceIdentifier("unseen")

	mockScheduler.EXPECT().
		IsFailedDevice(gomock.Any(), activeDevice).
		Return(false, time.Time{}, nil).AnyTimes()
	mockScheduler.EXPECT().
		IsFailedDevice(gomock.Any(), failedCachedActive).
		Return(true, time.Now().Add(-1*time.Minute), nil).AnyTimes()

	service := newStatusPollingService(t, ctrl, mockScheduler)
	for _, id := range []models.DeviceIdentifier{activeDevice, offlineDevice, failedCachedActive, inFlightDevice, unseenDevice} {
		service.devicesForStatusPolling.Store(id, struct{}{})
	}
	service.lastKnownStatuses.Store(activeDevice, mm.MinerStatusActive)
	service.lastKnownStatuses.Store(offlineDevice, mm.MinerStatusOffline)
	service.lastKnownStatuses.Store(failedCachedActive, mm.MinerStatusActive)
	service.lastKnownStatuses.Store(inFlightDevice, mm.MinerStatusOffline)
	service.inFlight.Store(inFlightDevice, struct{}{})

	// Act
	enqueued := runStatusPollingOnce(t, service)

	// Assert
	assert.NotContains(t, enqueued, activeDevice, "healthy active device should be skipped")
	assert.Contains(t, enqueued, offlineDevice, "offline device should be polled")
	assert.Contains(t, enqueued, failedCachedActive, "failed device must not be skipped even if cached ACTIVE")
	assert.NotContains(t, enqueued, inFlightDevice, "in-flight device should be skipped")
	assert.Contains(t, enqueued, unseenDevice, "unseen device should be polled")
}

// Tests for fetchStatusFromMiner auth error → InvalidateMiner

func TestFetchStatusFromMiner_AuthErrorFromGetMinerFromDeviceIdentifier_InvalidatesMinerCache(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Arrange
	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

	deviceID := models.DeviceIdentifier("device-bad-creds")
	authErr := fleeterror.NewUnauthenticatedErrorf("invalid credentials for device %s", deviceID)

	mockMinerGetter.EXPECT().
		GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).
		Return(nil, authErr)

	// fetchStatusFromMiner calls InvalidateMiner on auth error
	mockMinerGetter.EXPECT().
		InvalidateMiner(deviceID)

	// processStatusOnly calls handleAuthenticationFailure on auth error
	mockDeviceStore.EXPECT().
		UpdateDevicePairingStatusByIdentifier(gomock.Any(), string(deviceID), gomock.Any()).
		Return(nil)

	service := NewTelemetryService(Config{
		StalenessThreshold: 1 * time.Minute,
		FetchInterval:      10 * time.Second,
		ConcurrencyLimit:   5,
	}, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

	ctx := t.Context()
	device := models.Device{ID: deviceID}

	// Act
	service.processStatusOnly(ctx, device)

	// Assert — mock expectations verify InvalidateMiner was called exactly once
}

func TestFetchStatusFromMiner_AuthErrorFromGetDeviceStatus_InvalidatesMinerCache(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Arrange
	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
	mockMiner := minerMocks.NewMockMiner(ctrl)

	deviceID := models.DeviceIdentifier("device-expired-token")
	authErr := fleeterror.NewUnauthenticatedErrorf("token expired for device %s", deviceID)

	// GetMinerFromDeviceIdentifier succeeds (miner in cache)
	mockMinerGetter.EXPECT().
		GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).
		Return(mockMiner, nil)

	// GetDeviceStatus returns an auth error (e.g., token rotated)
	mockMiner.EXPECT().
		GetDeviceStatus(gomock.Any()).
		Return(mm.MinerStatusUnknown, authErr)

	// fetchStatusFromMiner calls InvalidateMiner on auth error from GetDeviceStatus
	mockMinerGetter.EXPECT().
		InvalidateMiner(deviceID)

	// processStatusOnly calls handleAuthenticationFailure on auth error
	mockDeviceStore.EXPECT().
		UpdateDevicePairingStatusByIdentifier(gomock.Any(), string(deviceID), gomock.Any()).
		Return(nil)

	service := NewTelemetryService(Config{
		StalenessThreshold: 1 * time.Minute,
		FetchInterval:      10 * time.Second,
		ConcurrencyLimit:   5,
	}, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

	ctx := t.Context()
	device := models.Device{ID: deviceID}

	// Act
	service.processStatusOnly(ctx, device)

	// Assert — mock expectations verify InvalidateMiner was called exactly once
}

func TestProcessStatusOnly_ForbiddenError_UpdatesPairingStatus(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
	mockMiner := minerMocks.NewMockMiner(ctrl)

	deviceID := models.DeviceIdentifier("device-default-password-active")
	forbiddenErr := fleeterror.NewForbiddenErrorf("default password must be changed for device %s", deviceID)

	mockMinerGetter.EXPECT().
		GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).
		Return(mockMiner, nil)

	mockMiner.EXPECT().
		GetDeviceStatus(gomock.Any()).
		Return(mm.MinerStatusUnknown, forbiddenErr)

	mockDeviceStore.EXPECT().
		UpdateDevicePairingStatusByIdentifier(gomock.Any(), string(deviceID), gomock.Any()).
		Return(nil)

	service := NewTelemetryService(Config{
		StalenessThreshold: 1 * time.Minute,
		FetchInterval:      10 * time.Second,
		ConcurrencyLimit:   5,
	}, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

	ctx := t.Context()
	device := models.Device{ID: deviceID}

	service.processStatusOnly(ctx, device)
}

func TestProcessStatusOnly_GenericForbiddenDoesNotUpdatePairingStatus(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
	mockMiner := minerMocks.NewMockMiner(ctrl)

	deviceID := models.DeviceIdentifier("device-access-denied")
	forbiddenErr := fleeterror.NewForbiddenErrorf("permission denied while reading telemetry for device %s", deviceID)

	mockMinerGetter.EXPECT().
		GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).
		Return(mockMiner, nil)

	mockMiner.EXPECT().
		GetDeviceStatus(gomock.Any()).
		Return(mm.MinerStatusUnknown, forbiddenErr)

	service := NewTelemetryService(Config{
		StalenessThreshold: 1 * time.Minute,
		FetchInterval:      10 * time.Second,
		ConcurrencyLimit:   5,
	}, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

	ctx := t.Context()
	device := models.Device{ID: deviceID}

	service.processStatusOnly(ctx, device)
}

func TestWriteFleetStateSnapshot(t *testing.T) {
	tickTime := time.Now().Truncate(time.Second)

	t.Run("issues one insert per tick", func(t *testing.T) {
		// Arrange
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
		mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
		mockDataStore.EXPECT().
			InsertMinerStateSnapshot(gomock.Any(), tickTime).
			Return(nil)

		service := NewTelemetryService(Config{ConcurrencyLimit: 1}, mockDataStore, nil, nil, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

		// Act
		service.writeFleetStateSnapshot(t.Context(), tickTime)
	})

	t.Run("logs and returns on insert error", func(t *testing.T) {
		// Arrange
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
		mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
		mockDataStore.EXPECT().
			InsertMinerStateSnapshot(gomock.Any(), tickTime).
			Return(errors.New("db down"))

		service := NewTelemetryService(Config{ConcurrencyLimit: 1}, mockDataStore, nil, nil, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

		// Act
		service.writeFleetStateSnapshot(t.Context(), tickTime)
	})
}
