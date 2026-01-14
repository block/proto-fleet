package telemetry

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/brianvoe/gofakeit/v7"
	gomock "github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/btc-mining/proto-fleet/server/internal/domain/diagnostics"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	minerMocks "github.com/btc-mining/proto-fleet/server/internal/domain/miner/interfaces/mocks"
	mm "github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	stores "github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"
	storesMocks "github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	mock "github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/mocks"
	"github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"
	modelsV2 "github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models/v2"
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

	service := NewTelemetryService(config, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

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
			}, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

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
		device                     models.Device
		telemetry                  []models.Telemetry
		deviceMetrics              *modelsV2.DeviceMetrics
		hasStoreError              bool
		hasSchedulerError          bool
		hasMinerError              bool
		hasDiscoveryError          bool
		hasDeviceMetricsError      bool
		hasDeviceMetricsStoreError bool
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
		{
			name: "validates GetDeviceMetrics succeeds and stores device metrics, GetTelemetry also succeeds",
			devicesScenario: []deviceScenario{
				{
					device: models.Device{
						ID:            "200",
						LastUpdatedAt: time.Now().Add(-5 * time.Minute),
					},
					deviceMetrics: &modelsV2.DeviceMetrics{
						DeviceID:  "200",
						Timestamp: time.Now(),
					},
					telemetry: []models.Telemetry{FakeTelemetryData("200")},
				},
			},
		},
		{
			name: "validates GetDeviceMetrics fails and falls back to GetTelemetry successfully",
			devicesScenario: []deviceScenario{
				{
					device: models.Device{
						ID:            "201",
						LastUpdatedAt: time.Now().Add(-5 * time.Minute),
					},
					hasDeviceMetricsError: true,
					telemetry:             []models.Telemetry{FakeTelemetryData("201")},
				},
			},
		},
		{
			name: "validates GetDeviceMetrics fails and GetTelemetry also fails",
			devicesScenario: []deviceScenario{
				{
					device: models.Device{
						ID:            "202",
						LastUpdatedAt: time.Now().Add(-5 * time.Minute),
					},
					hasDeviceMetricsError: true,
					hasMinerError:         true,
				},
			},
		},
		{
			name: "validates GetDeviceMetrics succeeds but StoreDeviceMetrics fails, GetTelemetry still succeeds",
			devicesScenario: []deviceScenario{
				{
					device: models.Device{
						ID:            "203",
						LastUpdatedAt: time.Now().Add(-5 * time.Minute),
					},
					deviceMetrics: &modelsV2.DeviceMetrics{
						DeviceID:  "203",
						Timestamp: time.Now(),
					},
					telemetry:                  []models.Telemetry{FakeTelemetryData("203")},
					hasDeviceMetricsStoreError: true,
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

				// Setup GetDeviceMetrics expectation (always called)
				if scenario.deviceMetrics != nil {
					mockMiner.EXPECT().
						GetDeviceMetrics(gomock.Any()).
						Return(*scenario.deviceMetrics, nil)
					if scenario.hasDeviceMetricsStoreError {
						mockDataStore.EXPECT().
							StoreDeviceMetrics(gomock.Any(), *scenario.deviceMetrics).
							Return(errors.New("device metrics store error"))
						// Don't continue - we still need to call GetTelemetry
					} else {
						mockDataStore.EXPECT().
							StoreDeviceMetrics(gomock.Any(), *scenario.deviceMetrics).
							Return(nil)
					}
				} else if scenario.hasDeviceMetricsError {
					mockMiner.EXPECT().
						GetDeviceMetrics(gomock.Any()).
						Return(modelsV2.DeviceMetrics{}, errors.New("device metrics error"))
				} else {
					// Default case - GetDeviceMetrics not implemented
					mockMiner.EXPECT().
						GetDeviceMetrics(gomock.Any()).
						Return(modelsV2.DeviceMetrics{}, errors.New("not implemented"))
				}

				// Setup GetTelemetry expectation (always called after GetDeviceMetrics)
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
			}, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

			for _, scenario := range test.devicesScenario {
				err := service.GetTelemetryFromDevice(t.Context(), scenario.device)
				// Only error if GetTelemetry fails, not if GetDeviceMetrics fails
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

		mockMinerGetter.EXPECT().
			GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).
			Return(nil, nil).AnyTimes()

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
		mockMinerGetter := mock.NewMockMinerGetter(ctrl)
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
		mockMinerGetter := mock.NewMockMinerGetter(ctrl)
		mockScheduler := mock.NewMockUpdateScheduler(ctrl)
		mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

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
		mockMinerGetter := mock.NewMockMinerGetter(ctrl)
		mockScheduler := mock.NewMockUpdateScheduler(ctrl)
		mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

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
		mockMinerGetter := mock.NewMockMinerGetter(ctrl)
		mockScheduler := mock.NewMockUpdateScheduler(ctrl)
		mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

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
		mockMinerGetter := mock.NewMockMinerGetter(ctrl)
		mockScheduler := mock.NewMockUpdateScheduler(ctrl)
		mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

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
		mockMinerGetter := mock.NewMockMinerGetter(ctrl)
		mockScheduler := mock.NewMockUpdateScheduler(ctrl)
		mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

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
	mockMinerGetter := mock.NewMockMinerGetter(ctrl)
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
	mockMinerGetter := mock.NewMockMinerGetter(ctrl)
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
	mockMinerGetter := mock.NewMockMinerGetter(ctrl)
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
	mockMinerGetter := mock.NewMockMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

	deviceID := models.DeviceIdentifier("test-device-1")

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
	mockMinerGetter := mock.NewMockMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

	deviceID := models.DeviceIdentifier("test-device-1")

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
	mockMinerGetter := mock.NewMockMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

	deviceID := models.DeviceIdentifier("test-device-1")

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

// Tests for processStatusOnly failed device recovery

func TestProcessStatusOnly_RecoversFailedDevice(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Arrange
	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockMinerGetter(ctrl)
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
	mockMinerGetter := mock.NewMockMinerGetter(ctrl)
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
	mockMinerGetter := mock.NewMockMinerGetter(ctrl)
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
	mockMinerGetter := mock.NewMockMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
	mockMiner := minerMocks.NewMockMiner(ctrl)
	mockErrorPoller := mock.NewMockErrorPoller(ctrl)

	deviceID := models.DeviceIdentifier("test-device")
	device := models.Device{ID: deviceID, LastUpdatedAt: time.Now().Add(-1 * time.Minute)}

	mockMinerGetter.EXPECT().
		GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).
		Return(mockMiner, nil).
		Times(3) // Telemetry, status, and error polling

	mockMiner.EXPECT().
		GetDeviceMetrics(gomock.Any()).
		Return(modelsV2.DeviceMetrics{}, errors.New("not implemented"))

	mockMiner.EXPECT().
		GetTelemetry(gomock.Any(), gomock.Any()).
		Return([]models.Telemetry{}, nil)

	mockDataStore.EXPECT().
		Store(gomock.Any(), gomock.Any()).
		Return(nil)

	mockScheduler.EXPECT().
		AddDevices(gomock.Any(), gomock.Any()).
		Return(nil)

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
		statusResults:      make(chan statusResult, 1), // Small buffer to test non-blocking
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
