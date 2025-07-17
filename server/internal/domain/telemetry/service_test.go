package telemetry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/brianvoe/gofakeit/v7"
	gomock "github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	minerMocks "github.com/btc-mining/proto-fleet/server/internal/domain/miner/interfaces/mocks"
	storesMocks "github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	mock "github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/mocks"
	"github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"
)

func TestNewTelemetryService(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

	config := Config{
		StalenessThreshold: 1 * time.Minute,
		FetchInterval:      10 * time.Second,
		ConcurrencyLimit:   5,
	}

	service := NewTelemetryService(config, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore)

	// Test that the service was created successfully
	assert.NotNil(t, service)
}

func TestTelemetryService_AddDevices(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockMinerGetter(ctrl)
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
			}, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore)

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
	mockMinerGetter := mock.NewMockMinerGetter(ctrl)
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
			}, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore)

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
	mockMinerGetter := mock.NewMockMinerGetter(ctrl)
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

	config := Config{
		StalenessThreshold: 1 * time.Minute,
		FetchInterval:      100 * time.Millisecond, // Short interval for test
		ConcurrencyLimit:   5,
		DevicePollInterval: 100 * time.Millisecond, // Short interval for test
	}

	service := NewTelemetryService(config, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore)

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
	mockMinerGetter := mock.NewMockMinerGetter(ctrl)
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

	config := Config{
		StalenessThreshold: 1 * time.Minute,
		FetchInterval:      100 * time.Millisecond, // Short interval for test
		ConcurrencyLimit:   5,
		DevicePollInterval: 100 * time.Millisecond, // Short interval for test
	}

	service := NewTelemetryService(config, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore)

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

func FakeTelemetryData(deviceID models.DeviceIdentifier) models.Telemetry {
	data := models.Telemetry{}
	data.Measurement = gofakeit.RandomString([]string{"temperature", "hashrate", "fan_speed", "power_usage"})
	data.Fields = map[string]any{
		"value": gofakeit.Float64Range(0, 100),
	}
	data.Tags = map[string]string{
		"device_id": deviceID.String(),
		"location":  gofakeit.City(),
	}
	data.Timestamp = gofakeit.DateRange(time.Now().Add(-24*time.Hour), time.Now())
	return data
}

func TestTelemetryService_DataStoreInteraction(t *testing.T) {
	type deviceScenario struct {
		device            models.Device
		telemetry         []models.Telemetry
		hasStoreError     bool
		hasSchedulerError bool
		hasMinerError     bool
		hasDiscoveryError bool
	}

	tests := []struct {
		name            string
		devicesScenario []deviceScenario
	}{
		{
			name: "validates telemetry data is stored correctly for one device one  telemetry record",
			devicesScenario: []deviceScenario{
				{
					device: models.Device{
						ID:            "123",
						LastUpdatedAt: time.Now().Add(-5 * time.Minute),
					},
					telemetry: []models.Telemetry{FakeTelemetryData("123")},
				},
			},
		},
		{
			name: "validates telemetry data is stored correctly for one device and multiple telemetry records",
			devicesScenario: []deviceScenario{
				{
					device: models.Device{
						ID:            "124",
						LastUpdatedAt: time.Now().Add(-5 * time.Minute),
					},
					telemetry: []models.Telemetry{
						FakeTelemetryData("124"),
						FakeTelemetryData("124"),
						FakeTelemetryData("124"),
						FakeTelemetryData("124"),
						FakeTelemetryData("124"),
					},
				},
			},
		},
		{
			name: "validates telemetry data is stored correctly for multiple devices with one telemetry record each",
			devicesScenario: []deviceScenario{
				{
					device: models.Device{
						ID:            "125",
						LastUpdatedAt: time.Now().Add(-5 * time.Minute),
					},
					telemetry: []models.Telemetry{FakeTelemetryData("125")},
				},
				{
					device: models.Device{
						ID:            "305",
						LastUpdatedAt: time.Now().Add(-2 * time.Minute),
					},
					telemetry: []models.Telemetry{FakeTelemetryData("305")},
				},
				{
					device: models.Device{
						ID:            "10010",
						LastUpdatedAt: time.Now().Add(-1 * time.Minute),
					},
					telemetry: []models.Telemetry{FakeTelemetryData("10010")},
				},
			},
		},
		{
			name: "validates telemetry data is stored correctly for multiple devices with multiple telemetry records each",
			devicesScenario: []deviceScenario{
				{
					device: models.Device{
						ID:            "125",
						LastUpdatedAt: time.Now().Add(-5 * time.Minute),
					},
					telemetry: []models.Telemetry{FakeTelemetryData("125"), FakeTelemetryData("125"), FakeTelemetryData("125"), FakeTelemetryData("125"), FakeTelemetryData("125")},
				},
				{
					device: models.Device{
						ID:            "305",
						LastUpdatedAt: time.Now().Add(-2 * time.Minute),
					},
					telemetry: []models.Telemetry{FakeTelemetryData("305"), FakeTelemetryData("305"), FakeTelemetryData("305")},
				},
				{
					device: models.Device{
						ID:            "10010",
						LastUpdatedAt: time.Now().Add(-1 * time.Minute),
					},
					telemetry: []models.Telemetry{FakeTelemetryData("10010"), FakeTelemetryData("10010"), FakeTelemetryData("10010"), FakeTelemetryData("10010"), FakeTelemetryData("10010")},
				},
			},
		},
		{
			name: "gets error when device discovery fails of just one device of many",
			devicesScenario: []deviceScenario{
				{
					device: models.Device{
						ID:            "125",
						LastUpdatedAt: time.Now().Add(-5 * time.Minute),
					},
					hasDiscoveryError: true,
				},
				{
					device: models.Device{
						ID:            "305",
						LastUpdatedAt: time.Now().Add(-2 * time.Minute),
					},
					telemetry: []models.Telemetry{FakeTelemetryData("305"), FakeTelemetryData("305"), FakeTelemetryData("305")},
				},
				{
					device: models.Device{
						ID:            "10010",
						LastUpdatedAt: time.Now().Add(-1 * time.Minute),
					},
					telemetry: []models.Telemetry{FakeTelemetryData("10010"), FakeTelemetryData("10010"), FakeTelemetryData("10010"), FakeTelemetryData("10010"), FakeTelemetryData("10010")},
				},
			},
		},
		{
			name: "gets error when miner errors of just one device of many",
			devicesScenario: []deviceScenario{
				{
					device: models.Device{
						ID:            "125",
						LastUpdatedAt: time.Now().Add(-5 * time.Minute),
					},
					hasMinerError: true,
				},
				{
					device: models.Device{
						ID:            "10010",
						LastUpdatedAt: time.Now().Add(-1 * time.Minute),
					},
					telemetry: []models.Telemetry{FakeTelemetryData("10010"), FakeTelemetryData("10010"), FakeTelemetryData("10010"), FakeTelemetryData("10010"), FakeTelemetryData("10010")},
				},
			},
		},
		{
			name: "gets error when store has an error for just one device of many",
			devicesScenario: []deviceScenario{
				{
					device: models.Device{
						ID:            "125",
						LastUpdatedAt: time.Now().Add(-5 * time.Minute),
					},
					telemetry:     []models.Telemetry{FakeTelemetryData("125"), FakeTelemetryData("125")},
					hasStoreError: true,
				},
				{
					device: models.Device{
						ID:            "10010",
						LastUpdatedAt: time.Now().Add(-1 * time.Minute),
					},
					telemetry: []models.Telemetry{FakeTelemetryData("10010"), FakeTelemetryData("10010"), FakeTelemetryData("10010"), FakeTelemetryData("10010"), FakeTelemetryData("10010")},
				},
			},
		},
		{
			name: "validates telemetry data with one device and no returned data",
			devicesScenario: []deviceScenario{
				{
					device: models.Device{
						ID:            "125",
						LastUpdatedAt: time.Now().Add(-5 * time.Minute),
					},
					telemetry:     []models.Telemetry{},
					hasStoreError: true,
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
			mockMinerGetter := mock.NewMockMinerGetter(ctrl)
			mockScheduler := mock.NewMockUpdateScheduler(ctrl)
			mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

			//nolint:revive
			addFailedDevice := func(device models.Device, withErr bool) {
				// TODO(briano-block): Migrate this into a larger test on the workers
				// if withErr {
				// 	mockScheduler.EXPECT().
				// 		AddFailedDevices(gomock.Any(), device).
				// 		Return(errors.New("failed to add device")).Times(1)
				// 	return
				// }
				// mockScheduler.EXPECT().
				// 	AddFailedDevices(gomock.Any(), device).
				// 	Return(nil).Times(1)
			}

			for _, scenario := range test.devicesScenario {
				if scenario.hasDiscoveryError {
					mockMinerGetter.EXPECT().
						GetMinerFromDeviceIdentifier(gomock.Any(), scenario.device.ID).
						Return(nil, errors.New("discovery error"))
					addFailedDevice(scenario.device, scenario.hasSchedulerError)
					continue
				}
				mockMiner := minerMocks.NewMockMiner(ctrl)
				mockMinerGetter.EXPECT().
					GetMinerFromDeviceIdentifier(gomock.Any(), scenario.device.ID).
					Return(mockMiner, nil)

				if scenario.hasMinerError {
					mockMiner.EXPECT().
						GetTelemetry(gomock.Any(), scenario.device.LastUpdatedAt).
						Return(nil, errors.New("miner error"))
					addFailedDevice(scenario.device, scenario.hasSchedulerError)
					continue
				}
				mockMiner.EXPECT().
					GetTelemetry(gomock.Any(), scenario.device.LastUpdatedAt).
					Return(scenario.telemetry, nil)
				if scenario.hasStoreError {
					mockDataStore.EXPECT().
						Store(gomock.Any(), scenario.telemetry).
						Return(errors.New("store error"))
					addFailedDevice(scenario.device, scenario.hasSchedulerError)
					continue
				}
				mockDataStore.EXPECT().
					Store(gomock.Any(), scenario.telemetry).
					Return(nil)
				mockScheduler.EXPECT().
					AddDevices(gomock.Any(), gomock.Any()).
					Do(func(ctx context.Context, devices ...models.Device) {
						require.Len(t, devices, 1)
						assert.Equal(t, scenario.device.ID, devices[0].ID)
					}).Return(nil).Times(1)
			}

			service := NewTelemetryService(Config{
				StalenessThreshold: 1 * time.Minute,
				FetchInterval:      10 * time.Second,
				ConcurrencyLimit:   5,
			}, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore)

			for _, scenario := range test.devicesScenario {
				err := service.GetTelemetryFromDevice(t.Context(), scenario.device)
				if scenario.hasMinerError || scenario.hasDiscoveryError || scenario.hasStoreError || scenario.hasSchedulerError {
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
		mockMinerGetter := mock.NewMockMinerGetter(ctrl)
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
		}, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore)

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
		mockMinerGetter := mock.NewMockMinerGetter(ctrl)
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
		}, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore)

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
		mockMinerGetter := mock.NewMockMinerGetter(ctrl)
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

		service := NewTelemetryService(Config{
			StalenessThreshold: 1 * time.Minute,
			FetchInterval:      100 * time.Millisecond, // Short interval for test
			ConcurrencyLimit:   5,
			MetricTimeout:      5 * time.Second,
			DevicePollInterval: 100 * time.Millisecond, // Short interval for test
		}, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore)

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
}

// TestTelemetryService_ComponentInteraction validates that all components work together
func TestTelemetryService_ComponentInteraction(t *testing.T) {
	t.Run("validates all dependencies are properly configured", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
		mockMinerGetter := mock.NewMockMinerGetter(ctrl)
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

		config := Config{
			StalenessThreshold: 1 * time.Minute,
			FetchInterval:      100 * time.Millisecond, // Short interval for test
			ConcurrencyLimit:   5,
			MetricTimeout:      5 * time.Second,
			DevicePollInterval: 100 * time.Millisecond, // Short interval for test
		}

		service := NewTelemetryService(config, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore)

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
		mockMinerGetter := mock.NewMockMinerGetter(ctrl)
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
		}, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore)

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
		mockMinerGetter := mock.NewMockMinerGetter(ctrl)
		mockScheduler := mock.NewMockUpdateScheduler(ctrl)
		mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

		// Test that component interactions maintain consistent state
		deviceIDs := []models.DeviceIdentifier{"700", "701", "702"}

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

		service := NewTelemetryService(Config{
			StalenessThreshold: 1 * time.Minute,
			FetchInterval:      100 * time.Millisecond, // Short interval for test
			ConcurrencyLimit:   5,
			MetricTimeout:      5 * time.Second,
			DevicePollInterval: 100 * time.Millisecond, // Short interval for test
		}, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore)

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
