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
	"github.com/block/proto-fleet/server/internal/domain/pairing"
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

func TestTelemetryService_AddDevicesRegistersWithSchedulerOnly(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
	deviceID := models.DeviceIdentifier("device-1")

	mockScheduler.EXPECT().
		AddNewDevices(gomock.Any(), deviceID).
		Return(nil)

	service := NewTelemetryService(Config{
		StalenessThreshold: 1 * time.Minute,
		FetchInterval:      10 * time.Second,
		ConcurrencyLimit:   1,
	}, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

	err := service.AddDevices(t.Context(), deviceID)

	require.NoError(t, err)
	_, tracked := service.devicesForStatusPolling.Load(deviceID)
	assert.True(t, tracked)
	select {
	case task := <-service.tasks:
		t.Fatalf("AddDevices should let the scheduler dispatch telemetry work, got immediate task %s", task.ID)
	default:
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
					mockDeviceStore.EXPECT().
						GetDeviceOrgDriverAndSite(gomock.Any(), scenario.device.ID).
						Return(int64(0), "", int64(0), nil).
						AnyTimes()
					continue
				}
				mockMiner := minerMocks.NewMockMiner(ctrl)
				mockMinerGetter.EXPECT().
					GetMinerFromDeviceIdentifier(gomock.Any(), scenario.device.ID).
					Return(mockMiner, nil)
				mockMiner.EXPECT().GetOrgID().Return(int64(0)).AnyTimes()
				mockMiner.EXPECT().GetSiteID().Return(int64(0)).AnyTimes()
				mockMiner.EXPECT().GetDriverName().Return("").AnyTimes()

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
				_, _, _, _, _, _, err := service.GetTelemetryFromDevice(t.Context(), scenario.device)
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

// A plugin that returns a DeviceMetrics whose DeviceIdentifier does not match
// the trusted device ID supplied to the poll is the same trust boundary the
// rest of this file enforces around health, hashrate, and temperature.
func TestGetTelemetryFromDevice_DropsMismatchedDeviceIdentifier(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
	mockMiner := minerMocks.NewMockMiner(ctrl)

	trustedID := models.DeviceIdentifier("trusted-device-1")
	device := models.Device{ID: trustedID, LastUpdatedAt: time.Now().Add(-5 * time.Minute)}

	mockMinerGetter.EXPECT().
		GetMinerFromDeviceIdentifier(gomock.Any(), trustedID).
		Return(mockMiner, nil)
	mockMiner.EXPECT().GetOrgID().Return(int64(42)).AnyTimes()
	mockMiner.EXPECT().GetSiteID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetDriverName().Return("virtual").AnyTimes()

	// Plugin returns a sample stamped with another device's identifier.
	mockMiner.EXPECT().
		GetDeviceMetrics(gomock.Any()).
		Return(modelsV2.DeviceMetrics{
			DeviceIdentifier: "victim-device",
			Health:           modelsV2.HealthHealthyActive,
			Timestamp:        time.Now(),
		}, nil)

	// AddDevices and StoreDeviceMetrics MUST NOT be called on this path.
	// We register no expectation; the gomock controller fails the test if
	// either method is invoked because the mocks are strict.

	service := NewTelemetryService(Config{
		StalenessThreshold: 1 * time.Minute,
		FetchInterval:      10 * time.Second,
		ConcurrencyLimit:   5,
	}, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

	status, hasStatus, orgID, driverName, _, pollSuccess, err := service.GetTelemetryFromDevice(t.Context(), device)

	require.Error(t, err, "mismatched plugin identifier must surface as a telemetryErr so processDevice triggers AddFailedDevices")
	assert.Contains(t, err.Error(), "mismatched device identifier")
	assert.Equal(t, mm.MinerStatusUnknown, status, "tainted health-derived status must not be returned")
	assert.False(t, hasStatus, "tainted health-derived status must not be returned")
	assert.False(t, pollSuccess, "the poll must not be counted as successful")
	assert.Equal(t, int64(42), orgID, "the trusted miner-resolved orgID must still be returned for poll-failure accounting")
	assert.Equal(t, "virtual", driverName)

	// metricsResults must not have received the tainted sample. The channel
	// is buffered, so a non-blocking receive proves nothing was enqueued.
	select {
	case got := <-service.metricsResults:
		t.Fatalf("forged telemetry sample was enqueued for persistence: %+v", got)
	default:
	}
}

// A plugin that returns a DeviceMetrics with an empty DeviceIdentifier — i.e.
// non-authoritative rather than forged — must have the trusted poll target
// stamped onto the sample before it leaves GetTelemetryFromDevice.
func TestGetTelemetryFromDevice_NormalizesEmptyDeviceIdentifier(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
	mockMiner := minerMocks.NewMockMiner(ctrl)

	trustedID := models.DeviceIdentifier("trusted-device-7")
	device := models.Device{ID: trustedID, LastUpdatedAt: time.Now().Add(-5 * time.Minute)}

	mockMinerGetter.EXPECT().
		GetMinerFromDeviceIdentifier(gomock.Any(), trustedID).
		Return(mockMiner, nil)
	mockMiner.EXPECT().GetOrgID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetSiteID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetDriverName().Return("").AnyTimes()

	// Plugin reports metrics but leaves DeviceIdentifier blank.
	mockMiner.EXPECT().
		GetDeviceMetrics(gomock.Any()).
		Return(modelsV2.DeviceMetrics{
			Health:    modelsV2.HealthHealthyActive,
			Timestamp: time.Now(),
		}, nil)

	mockScheduler.EXPECT().
		AddDevices(gomock.Any(), gomock.Any()).
		Do(func(_ context.Context, devices ...models.Device) {
			require.Len(t, devices, 1)
			assert.Equal(t, trustedID, devices[0].ID)
		}).Return(nil).Times(1)

	service := NewTelemetryService(Config{
		StalenessThreshold: 1 * time.Minute,
		FetchInterval:      10 * time.Second,
		ConcurrencyLimit:   5,
	}, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

	_, _, _, _, _, pollSuccess, err := service.GetTelemetryFromDevice(t.Context(), device)
	require.NoError(t, err, "empty plugin identifier is non-authoritative and must be normalized, not rejected")
	assert.True(t, pollSuccess)

	// Drain the enqueued metricsResult and verify the trusted ID was stamped on.
	select {
	case got := <-service.metricsResults:
		assert.Equal(t, trustedID, got.deviceID)
		assert.Equal(t, string(trustedID), got.metrics.DeviceIdentifier,
			"empty plugin identifier must be overwritten with the trusted poll target so persistence and OTel agree on the device")
	case <-time.After(time.Second):
		t.Fatal("expected a metricsResult to be enqueued for the normalized sample")
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

		// New devices become scheduler-eligible immediately and are dispatched by
		// the gather loop instead of bypassing scheduler ownership.

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

	mockMinerGetter.EXPECT().
		GetMinerFromDeviceIdentifier(gomock.Any(), gomock.Any()).
		Return(nil, errors.New("metrics observer stub")).
		AnyTimes()

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

	mockMinerGetter.EXPECT().
		GetMinerFromDeviceIdentifier(gomock.Any(), gomock.Any()).
		Return(nil, errors.New("metrics observer stub")).
		AnyTimes()

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

	mockMinerGetter.EXPECT().
		GetMinerFromDeviceIdentifier(gomock.Any(), gomock.Any()).
		Return(nil, errors.New("metrics observer stub")).
		AnyTimes()

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

// the writer must emit fleet_device_online using the org/driver labels the worker attached to statusResult,
// and it must NOT call back into the miner manager from the flush loop.
func TestStatusWriterRoutine_FlushUsesWorkerSuppliedLabels(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

	mockDeviceStore.EXPECT().
		GetDeviceStatusForDeviceIdentifiers(gomock.Any(), gomock.Any()).
		Return(map[models.DeviceIdentifier]mm.MinerStatus{}, nil).
		AnyTimes()
	mockDeviceStore.EXPECT().
		UpsertDeviceStatuses(gomock.Any(), gomock.Any()).
		Return(nil).
		AnyTimes()

	// Crucial assertion: the flush loop must NEVER consult the miner manager
	// for org/driver labels. The .Times(0) fails the test if a regression
	// brings back the per-device miner lookups.
	mockMinerGetter.EXPECT().
		GetMinerFromDeviceIdentifier(gomock.Any(), gomock.Any()).
		Times(0)

	rec := &recordingEmitter{}

	service := NewTelemetryService(Config{
		StalenessThreshold:  1 * time.Minute,
		FetchInterval:       10 * time.Second,
		ConcurrencyLimit:    5,
		StatusFlushInterval: 50 * time.Millisecond,
	}, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl)).WithMetricsEmitter(rec)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go service.statusWriterRoutine(ctx)

	service.statusResults <- statusResult{
		deviceIdentifier: models.DeviceIdentifier("dev-A"),
		status:           mm.MinerStatusActive,
		orgID:            42,
		driverName:       "proto",
	}
	service.statusResults <- statusResult{
		deviceIdentifier: models.DeviceIdentifier("dev-B"),
		status:           mm.MinerStatusOffline,
		orgID:            99,
		driverName:       "antminer",
	}

	require.Eventually(t, func() bool {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		return len(rec.online) >= 2
	}, time.Second, 10*time.Millisecond, "writer routine should emit fleet_device_online for both devices")

	cancel()
	time.Sleep(50 * time.Millisecond)

	rec.mu.Lock()
	defer rec.mu.Unlock()

	byDevice := map[string]onlineEvent{}
	for _, ev := range rec.online {
		byDevice[ev.labels.DeviceID] = ev
	}

	devA, ok := byDevice["dev-A"]
	require.True(t, ok, "expected an onDeviceStatus event for dev-A")
	require.Equal(t, "42", devA.labels.OrganizationID,
		"flush must use the worker-supplied org id label")
	require.Equal(t, "proto", devA.labels.Driver,
		"flush must use the worker-supplied driver label")
	require.True(t, devA.online, "MinerStatusActive should map to online=true")

	devB, ok := byDevice["dev-B"]
	require.True(t, ok, "expected an onDeviceStatus event for dev-B")
	require.Equal(t, "99", devB.labels.OrganizationID)
	require.Equal(t, "antminer", devB.labels.Driver)
	require.False(t, devB.online, "MinerStatusOffline should map to online=false")
}

func TestStatusWriterRoutine_FlushRequestChunksDrainedStatuses(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

	statuses := make([]statusResult, maxStatusBatchSize+1)
	for i := range statuses {
		statuses[i] = statusResult{
			deviceIdentifier: models.DeviceIdentifier(fmt.Sprintf("test-device-%d", i)),
			status:           mm.MinerStatusActive,
		}
	}

	mockMinerGetter.EXPECT().
		GetMinerFromDeviceIdentifier(gomock.Any(), gomock.Any()).
		Return(nil, errors.New("metrics observer stub")).
		AnyTimes()

	statusLookupSizes := make(chan int, 2)
	mockDeviceStore.EXPECT().
		GetDeviceStatusForDeviceIdentifiers(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, deviceIDs []models.DeviceIdentifier) (map[models.DeviceIdentifier]mm.MinerStatus, error) {
			statusLookupSizes <- len(deviceIDs)
			return map[models.DeviceIdentifier]mm.MinerStatus{}, nil
		}).
		Times(2)

	upsertSizes := make(chan int, 2)
	mockDeviceStore.EXPECT().
		UpsertDeviceStatuses(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, updates []stores.DeviceStatusUpdate) error {
			upsertSizes <- len(updates)
			return nil
		}).
		Times(2)

	service := NewTelemetryService(
		Config{StalenessThreshold: time.Minute, FetchInterval: 10 * time.Second, ConcurrencyLimit: 1, StatusFlushInterval: 10 * time.Second},
		mockDataStore,
		mockMinerGetter,
		mockScheduler,
		mockDeviceStore,
		mock.NewMockErrorPoller(ctrl),
	)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	for _, status := range statuses {
		service.statusResults <- status
	}

	go service.statusWriterRoutine(ctx)

	require.NoError(t, service.FlushStatusNow(ctx))
	assert.ElementsMatch(t, []int{maxStatusBatchSize, 1}, []int{<-statusLookupSizes, <-statusLookupSizes})
	assert.ElementsMatch(t, []int{maxStatusBatchSize, 1}, []int{<-upsertSizes, <-upsertSizes})
}

// When the current DB status is UPDATING / REBOOT_REQUIRED, the writer must
// still call onDeviceStatus for the pending sample so that an unreachable
// miner keeps emitting fleet_device_online=0 through a stuck firmware update
// and the default offline alert is not silenced.
func TestStatusWriterRoutine_FirmwareUpdateGuardDoesNotSuppressOnlineMetric(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

	updatingDevice := models.DeviceIdentifier("dev-updating")
	rebootDevice := models.DeviceIdentifier("dev-reboot-required")
	healthyDevice := models.DeviceIdentifier("dev-healthy")

	// Each pending device has a different DB-side current status. UPDATING and
	// REBOOT_REQUIRED are guarded — they must NOT appear in the upsert batch.
	mockDeviceStore.EXPECT().
		GetDeviceStatusForDeviceIdentifiers(gomock.Any(), gomock.Any()).
		Return(map[models.DeviceIdentifier]mm.MinerStatus{
			updatingDevice: mm.MinerStatusUpdating,
			rebootDevice:   mm.MinerStatusRebootRequired,
			healthyDevice:  mm.MinerStatusActive,
		}, nil).
		AnyTimes()

	// The upsert must receive ONLY the healthy device. Capturing the batch
	// here verifies the firmware-update guard still suppresses the DB write
	// (we only want to fix the metric emission, not weaken the guard).
	mockDeviceStore.EXPECT().
		UpsertDeviceStatuses(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, updates []stores.DeviceStatusUpdate) error {
			require.Len(t, updates, 1, "firmware-guarded devices must not land in the DB upsert")
			assert.Equal(t, healthyDevice, updates[0].DeviceIdentifier)
			return nil
		}).
		AnyTimes()

	rec := &recordingEmitter{}

	// Long StatusFlushInterval so we control flushing via context cancel —
	// otherwise a ticker firing mid-send would split the three samples across
	// multiple flushes and the upsert-batch assertion above would race.
	service := NewTelemetryService(Config{
		StalenessThreshold:  1 * time.Minute,
		FetchInterval:       10 * time.Second,
		ConcurrencyLimit:    5,
		StatusFlushInterval: 10 * time.Second,
	}, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl)).WithMetricsEmitter(rec)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		service.statusWriterRoutine(ctx)
		close(done)
	}()

	// The unreachable miner whose DB row is UPDATING — the case the default
	// offline alert exists to catch.
	service.statusResults <- statusResult{
		deviceIdentifier: updatingDevice,
		status:           mm.MinerStatusOffline,
		orgID:            42,
		driverName:       "proto",
	}
	// A device whose DB row is REBOOT_REQUIRED but is now back online —
	// fleet_device_online must follow the polled value, not the stuck DB row.
	service.statusResults <- statusResult{
		deviceIdentifier: rebootDevice,
		status:           mm.MinerStatusActive,
		orgID:            42,
		driverName:       "proto",
	}
	// Control device that is not firmware-guarded; verifies the unguarded
	// path still works alongside the guarded ones in the same flush.
	service.statusResults <- statusResult{
		deviceIdentifier: healthyDevice,
		status:           mm.MinerStatusActive,
		orgID:            42,
		driverName:       "antminer",
	}

	// Give the writer goroutine time to drain statusResults into pendingUpdates
	// before we trigger the final flush via cancel().
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("statusWriterRoutine did not finish after context cancel")
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()

	byDevice := map[string]onlineEvent{}
	for _, ev := range rec.online {
		byDevice[ev.labels.DeviceID] = ev
	}

	updatingEv, ok := byDevice[string(updatingDevice)]
	require.True(t, ok, "fleet_device_online must be emitted for an UPDATING device that is now Offline")
	assert.False(t, updatingEv.online,
		"MinerStatusOffline must surface as online=false even when DB row is UPDATING")
	assert.Equal(t, "42", updatingEv.labels.OrganizationID)
	assert.Equal(t, "proto", updatingEv.labels.Driver)

	rebootEv, ok := byDevice[string(rebootDevice)]
	require.True(t, ok, "fleet_device_online must be emitted for a REBOOT_REQUIRED device whose poll succeeded")
	assert.True(t, rebootEv.online,
		"MinerStatusActive must surface as online=true even when DB row is REBOOT_REQUIRED")

	healthyEv, ok := byDevice[string(healthyDevice)]
	require.True(t, ok, "non-firmware-guarded device must still emit")
	assert.True(t, healthyEv.online)
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

	mockMinerGetter.EXPECT().
		GetMinerFromDeviceIdentifier(gomock.Any(), gomock.Any()).
		Return(nil, errors.New("metrics observer stub")).
		AnyTimes()

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

func TestMetricsWriterRoutine_FlushesOnRequest(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMiner := minerMocks.NewMockMiner(ctrl)
	mockCachedMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

	metric := modelsV2.DeviceMetrics{DeviceIdentifier: "test-device-1"}

	mockCachedMinerGetter.EXPECT().
		GetMinerFromDeviceIdentifier(gomock.Any(), gomock.Any()).
		Return(mockMiner, nil).
		AnyTimes()

	mockDataStore.EXPECT().
		StoreDeviceMetrics(gomock.Any(), metric).
		Return(nil).
		Times(1)

	config := Config{
		StalenessThreshold:  1 * time.Minute,
		FetchInterval:       10 * time.Second,
		ConcurrencyLimit:    5,
		StatusFlushInterval: 10 * time.Second,
	}

	service := NewTelemetryService(config, mockDataStore, mockCachedMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go service.metricsWriterRoutine(ctx)
	service.metricsResults <- metricsResult{metrics: metric}

	require.NoError(t, service.FlushMetricsNow(ctx))
}

func TestMetricsWriterRoutine_FlushRequestChunksDrainedMetrics(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

	metrics := make([]modelsV2.DeviceMetrics, maxMetricsBatchSize+1)
	for i := range metrics {
		metrics[i] = modelsV2.DeviceMetrics{DeviceIdentifier: fmt.Sprintf("test-device-%d", i)}
	}

	mockMinerGetter.EXPECT().
		GetMinerFromDeviceIdentifier(gomock.Any(), gomock.Any()).
		Return(nil, errors.New("metrics observer stub")).
		AnyTimes()

	firstBatchArgs := make([]any, 0, maxMetricsBatchSize)
	for _, metric := range metrics[:maxMetricsBatchSize] {
		firstBatchArgs = append(firstBatchArgs, metric)
	}

	mockDataStore.EXPECT().
		StoreDeviceMetrics(gomock.Any(), firstBatchArgs...).
		Return(nil).
		Times(1)
	mockDataStore.EXPECT().
		StoreDeviceMetrics(gomock.Any(), metrics[maxMetricsBatchSize]).
		Return(nil).
		Times(1)

	service := NewTelemetryService(
		Config{StalenessThreshold: time.Minute, FetchInterval: 10 * time.Second, ConcurrencyLimit: 1, StatusFlushInterval: 10 * time.Second},
		mockDataStore,
		mockMinerGetter,
		mockScheduler,
		mockDeviceStore,
		mock.NewMockErrorPoller(ctrl),
	)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	for _, metric := range metrics {
		service.metricsResults <- metricsResult{metrics: metric}
	}

	go service.metricsWriterRoutine(ctx)

	require.NoError(t, service.FlushMetricsNow(ctx))
}

func TestFlushStatusNow_ReturnsContextErrorWhenCanceledBeforeQueue(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	service := NewTelemetryService(
		Config{StalenessThreshold: time.Minute, FetchInterval: 10 * time.Second, ConcurrencyLimit: 1},
		mock.NewMockTelemetryDataStore(ctrl),
		mock.NewMockCachedMinerGetter(ctrl),
		mock.NewMockUpdateScheduler(ctrl),
		storesMocks.NewMockDeviceStore(ctrl),
		mock.NewMockErrorPoller(ctrl),
	)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := service.FlushStatusNow(ctx)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "context cancelled before status flush request was queued")
}

func TestFlushMetricsNow_ReturnsContextErrorWhenCanceledBeforeQueue(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	service := NewTelemetryService(
		Config{StalenessThreshold: time.Minute, FetchInterval: 10 * time.Second, ConcurrencyLimit: 1},
		mock.NewMockTelemetryDataStore(ctrl),
		mock.NewMockCachedMinerGetter(ctrl),
		mock.NewMockUpdateScheduler(ctrl),
		storesMocks.NewMockDeviceStore(ctrl),
		mock.NewMockErrorPoller(ctrl),
	)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := service.FlushMetricsNow(ctx)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "context cancelled before metrics flush request was queued")
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

	mockMinerGetter.EXPECT().
		GetMinerFromDeviceIdentifier(gomock.Any(), gomock.Any()).
		Return(nil, errors.New("metrics observer stub")).
		AnyTimes()

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

	mockMinerGetter.EXPECT().
		GetMinerFromDeviceIdentifier(gomock.Any(), gomock.Any()).
		Return(nil, errors.New("metrics observer stub")).
		AnyTimes()

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

	mockMinerGetter.EXPECT().
		GetMinerFromDeviceIdentifier(gomock.Any(), gomock.Any()).
		Return(nil, errors.New("metrics observer stub")).
		AnyTimes()

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

func TestRefreshDevice_WaitsForExistingInFlightCollectionAndFlushesWriters(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
	mockMiner := minerMocks.NewMockMiner(ctrl)
	mockErrorPoller := mock.NewMockErrorPoller(ctrl)

	deviceID := models.DeviceIdentifier("test-device-1")
	metric := modelsV2.DeviceMetrics{
		DeviceIdentifier: string(deviceID),
		Health:           modelsV2.HealthHealthyActive,
	}
	status := mm.MinerStatusActive

	mockMinerGetter.EXPECT().
		GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).
		Return(mockMiner, nil).
		Times(2)

	mockMiner.EXPECT().GetOrgID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetSiteID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetDriverName().Return("").AnyTimes()
	mockMiner.EXPECT().
		GetDeviceMetrics(gomock.Any()).
		Return(metric, nil)

	mockDeviceStore.EXPECT().
		GetDeviceStatusForDeviceIdentifiers(gomock.Any(), []models.DeviceIdentifier{deviceID}).
		Return(map[models.DeviceIdentifier]mm.MinerStatus{}, nil).
		Times(1)
	mockDeviceStore.EXPECT().
		UpsertDeviceStatuses(gomock.Any(), gomock.AssignableToTypeOf([]stores.DeviceStatusUpdate{})).
		DoAndReturn(func(_ context.Context, updates []stores.DeviceStatusUpdate) error {
			require.Len(t, updates, 1)
			assert.Equal(t, deviceID, updates[0].DeviceIdentifier)
			assert.Equal(t, status, updates[0].Status)
			return nil
		}).
		Times(1)
	mockDataStore.EXPECT().
		StoreDeviceMetrics(gomock.Any(), metric).
		Return(nil).
		Times(1)
	mockScheduler.EXPECT().
		AddDevices(gomock.Any(), gomock.Any()).
		Return(nil).
		Times(1)
	mockErrorPoller.EXPECT().
		PollErrors(gomock.Any(), mockMiner).
		Return(diagnostics.PollResult{}).
		Times(1)

	config := Config{
		StalenessThreshold:  1 * time.Minute,
		FetchInterval:       10 * time.Second,
		ConcurrencyLimit:    5,
		StatusFlushInterval: 10 * time.Second,
		MetricTimeout:       5 * time.Second,
	}

	service := NewTelemetryService(config, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mockErrorPoller)
	service.inFlight.Store(deviceID, struct{}{})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go service.statusWriterRoutine(ctx)
	go service.metricsWriterRoutine(ctx)

	go func() {
		time.Sleep(20 * time.Millisecond)
		service.inFlight.Delete(deviceID)
	}()

	require.NoError(t, service.RefreshDevice(ctx, models.Device{ID: deviceID}))
}

func TestRefreshDevice_ConnectionErrorFlushesOfflineStatusAndSucceeds(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
	mockErrorPoller := mock.NewMockErrorPoller(ctrl)

	deviceID := models.DeviceIdentifier("offline-refresh-device")
	device := models.Device{ID: deviceID}
	connErr := fleeterror.NewConnectionError(string(deviceID), errors.New("connection refused"))

	mockMinerGetter.EXPECT().
		GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).
		Return(nil, connErr).
		Times(3)
	mockDeviceStore.EXPECT().
		GetDeviceOrgDriverAndSite(gomock.Any(), deviceID).
		Return(int64(42), "antminer", int64(7), nil).
		Times(2)
	mockScheduler.EXPECT().
		AddFailedDevices(gomock.Any(), device).
		Return(nil).
		Times(1)
	mockDeviceStore.EXPECT().
		GetDeviceStatusForDeviceIdentifiers(gomock.Any(), []models.DeviceIdentifier{deviceID}).
		Return(map[models.DeviceIdentifier]mm.MinerStatus{}, nil).
		Times(1)
	mockDeviceStore.EXPECT().
		UpsertDeviceStatuses(gomock.Any(), gomock.AssignableToTypeOf([]stores.DeviceStatusUpdate{})).
		DoAndReturn(func(_ context.Context, updates []stores.DeviceStatusUpdate) error {
			require.Len(t, updates, 1)
			assert.Equal(t, deviceID, updates[0].DeviceIdentifier)
			assert.Equal(t, mm.MinerStatusOffline, updates[0].Status)
			return nil
		}).
		Times(1)

	service := NewTelemetryService(Config{
		StalenessThreshold:  1 * time.Minute,
		FetchInterval:       10 * time.Second,
		ConcurrencyLimit:    5,
		StatusFlushInterval: 10 * time.Second,
		MetricTimeout:       5 * time.Second,
	}, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mockErrorPoller)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go service.statusWriterRoutine(ctx)
	go service.metricsWriterRoutine(ctx)

	require.NoError(t, service.RefreshDevice(ctx, device))
}

func TestRefreshDevice_IgnoresUnrelatedMetricFlushError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
	mockMiner := minerMocks.NewMockMiner(ctrl)
	mockErrorPoller := mock.NewMockErrorPoller(ctrl)

	deviceID := models.DeviceIdentifier("refresh-device")
	device := models.Device{ID: deviceID}
	requestedMetric := modelsV2.DeviceMetrics{
		DeviceIdentifier: string(deviceID),
		Health:           modelsV2.HealthHealthyActive,
	}
	unrelatedMetric := modelsV2.DeviceMetrics{DeviceIdentifier: "unrelated-device"}
	unrelatedErr := errors.New("unrelated metric insert failed")

	mockMinerGetter.EXPECT().
		GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).
		Return(mockMiner, nil).
		Times(2)
	mockMiner.EXPECT().GetOrgID().Return(int64(42)).AnyTimes()
	mockMiner.EXPECT().GetSiteID().Return(int64(7)).AnyTimes()
	mockMiner.EXPECT().GetDriverName().Return("antminer").AnyTimes()
	mockMiner.EXPECT().
		GetDeviceMetrics(gomock.Any()).
		Return(requestedMetric, nil)
	mockScheduler.EXPECT().
		AddDevices(gomock.Any(), gomock.AssignableToTypeOf(models.Device{})).
		Return(nil).
		Times(1)
	mockDeviceStore.EXPECT().
		GetDeviceStatusForDeviceIdentifiers(gomock.Any(), []models.DeviceIdentifier{deviceID}).
		Return(map[models.DeviceIdentifier]mm.MinerStatus{}, nil).
		Times(1)
	mockDeviceStore.EXPECT().
		UpsertDeviceStatuses(gomock.Any(), gomock.AssignableToTypeOf([]stores.DeviceStatusUpdate{})).
		DoAndReturn(func(_ context.Context, updates []stores.DeviceStatusUpdate) error {
			require.Len(t, updates, 1)
			assert.Equal(t, deviceID, updates[0].DeviceIdentifier)
			assert.Equal(t, mm.MinerStatusActive, updates[0].Status)
			return nil
		}).
		Times(1)
	mockErrorPoller.EXPECT().
		PollErrors(gomock.Any(), mockMiner).
		Return(diagnostics.PollResult{}).
		Times(1)
	mockDataStore.EXPECT().
		StoreDeviceMetrics(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(errors.New("batch insert failed")).
		Times(1)
	mockDataStore.EXPECT().
		StoreDeviceMetrics(gomock.Any(), gomock.AssignableToTypeOf(modelsV2.DeviceMetrics{})).
		DoAndReturn(func(_ context.Context, metric modelsV2.DeviceMetrics) error {
			if metric.DeviceIdentifier == unrelatedMetric.DeviceIdentifier {
				return unrelatedErr
			}
			assert.Equal(t, requestedMetric.DeviceIdentifier, metric.DeviceIdentifier)
			return nil
		}).
		Times(2)

	service := NewTelemetryService(Config{
		StalenessThreshold:  1 * time.Minute,
		FetchInterval:       10 * time.Second,
		ConcurrencyLimit:    5,
		StatusFlushInterval: 10 * time.Second,
		MetricTimeout:       5 * time.Second,
	}, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mockErrorPoller)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go service.statusWriterRoutine(ctx)
	go service.metricsWriterRoutine(ctx)
	service.metricsResults <- metricsResult{metrics: unrelatedMetric}

	require.NoError(t, service.RefreshDevice(ctx, device))
}

func TestRefreshDevice_ReturnsRequestedMetricFlushError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
	mockMiner := minerMocks.NewMockMiner(ctrl)
	mockErrorPoller := mock.NewMockErrorPoller(ctrl)

	deviceID := models.DeviceIdentifier("refresh-device")
	device := models.Device{ID: deviceID}
	requestedMetric := modelsV2.DeviceMetrics{
		DeviceIdentifier: string(deviceID),
		Health:           modelsV2.HealthHealthyActive,
	}
	requestedErr := errors.New("requested metric insert failed")

	mockMinerGetter.EXPECT().
		GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).
		Return(mockMiner, nil).
		Times(2)
	mockMiner.EXPECT().GetOrgID().Return(int64(42)).AnyTimes()
	mockMiner.EXPECT().GetSiteID().Return(int64(7)).AnyTimes()
	mockMiner.EXPECT().GetDriverName().Return("antminer").AnyTimes()
	mockMiner.EXPECT().
		GetDeviceMetrics(gomock.Any()).
		Return(requestedMetric, nil)
	mockScheduler.EXPECT().
		AddDevices(gomock.Any(), gomock.AssignableToTypeOf(models.Device{})).
		Return(nil).
		Times(1)
	mockDeviceStore.EXPECT().
		GetDeviceStatusForDeviceIdentifiers(gomock.Any(), []models.DeviceIdentifier{deviceID}).
		Return(map[models.DeviceIdentifier]mm.MinerStatus{}, nil).
		Times(1)
	mockDeviceStore.EXPECT().
		UpsertDeviceStatuses(gomock.Any(), gomock.AssignableToTypeOf([]stores.DeviceStatusUpdate{})).
		Return(nil).
		Times(1)
	mockErrorPoller.EXPECT().
		PollErrors(gomock.Any(), mockMiner).
		Return(diagnostics.PollResult{}).
		Times(1)
	mockDataStore.EXPECT().
		StoreDeviceMetrics(gomock.Any(), requestedMetric).
		Return(requestedErr).
		Times(1)
	mockDataStore.EXPECT().
		StoreDeviceMetrics(gomock.Any(), requestedMetric).
		Return(requestedErr).
		Times(1)

	service := NewTelemetryService(Config{
		StalenessThreshold:  1 * time.Minute,
		FetchInterval:       10 * time.Second,
		ConcurrencyLimit:    5,
		StatusFlushInterval: 10 * time.Second,
		MetricTimeout:       5 * time.Second,
	}, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mockErrorPoller)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go service.statusWriterRoutine(ctx)
	go service.metricsWriterRoutine(ctx)

	err := service.RefreshDevice(ctx, device)

	require.Error(t, err)
	assert.ErrorIs(t, err, requestedErr)
}

func TestRefreshDevice_RunsCollectionAndReturnsErrorAfterFullTelemetryInFlightClears(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

	deviceID := models.DeviceIdentifier("test-device-1")
	collectionErr := errors.New("collection failed")

	mockMinerGetter.EXPECT().
		GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).
		Return(nil, collectionErr).
		Times(2)
	mockDeviceStore.EXPECT().
		GetDeviceOrgDriverAndSite(gomock.Any(), deviceID).
		Return(int64(0), "", int64(0), nil).
		Times(1)
	mockScheduler.EXPECT().
		AddFailedDevices(gomock.Any(), gomock.Any()).
		Return(nil).
		Times(1)

	service := NewTelemetryService(
		Config{StalenessThreshold: time.Minute, FetchInterval: 10 * time.Second, ConcurrencyLimit: 1},
		mockDataStore,
		mockMinerGetter,
		mockScheduler,
		mockDeviceStore,
		mock.NewMockErrorPoller(ctrl),
	)
	service.inFlight.Store(deviceID, inFlightKindFullTelemetry)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go service.statusWriterRoutine(ctx)
	go service.metricsWriterRoutine(ctx)

	go func() {
		time.Sleep(20 * time.Millisecond)
		service.inFlight.Delete(deviceID)
	}()

	err := service.RefreshDevice(ctx, models.Device{ID: deviceID})

	require.Error(t, err)
	assert.Contains(t, err.Error(), collectionErr.Error())
}

func TestRefreshDevice_ReturnsContextErrorWhileWaitingForInFlightCollection(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	deviceID := models.DeviceIdentifier("test-device-1")
	service := NewTelemetryService(
		Config{StalenessThreshold: time.Minute, FetchInterval: 10 * time.Second, ConcurrencyLimit: 1},
		mock.NewMockTelemetryDataStore(ctrl),
		mock.NewMockCachedMinerGetter(ctrl),
		mock.NewMockUpdateScheduler(ctrl),
		storesMocks.NewMockDeviceStore(ctrl),
		mock.NewMockErrorPoller(ctrl),
	)
	service.inFlight.Store(deviceID, struct{}{})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := service.RefreshDevice(ctx, models.Device{ID: deviceID})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "context cancelled waiting for in-flight refresh")
}

func TestClaimDeviceForRefresh_ClaimsAfterStatusOnlyInFlightCollection(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	deviceID := models.DeviceIdentifier("test-device-1")
	service := NewTelemetryService(
		Config{StalenessThreshold: time.Minute, FetchInterval: 10 * time.Second, ConcurrencyLimit: 1},
		mock.NewMockTelemetryDataStore(ctrl),
		mock.NewMockCachedMinerGetter(ctrl),
		mock.NewMockUpdateScheduler(ctrl),
		storesMocks.NewMockDeviceStore(ctrl),
		mock.NewMockErrorPoller(ctrl),
	)
	service.inFlight.Store(deviceID, inFlightKindStatusOnly)

	done := make(chan bool, 1)
	go func() {
		claimed, err := service.claimDeviceForRefresh(t.Context(), deviceID)
		require.NoError(t, err)
		done <- claimed
	}()

	time.Sleep(20 * time.Millisecond)
	service.inFlight.Delete(deviceID)

	select {
	case claimed := <-done:
		assert.True(t, claimed)
	case <-time.After(time.Second):
		t.Fatal("claimDeviceForRefresh did not claim after status-only in-flight collection cleared")
	}

	value, ok := service.inFlight.Load(deviceID)
	require.True(t, ok)
	assert.Equal(t, inFlightKindFullTelemetry, value)
}

func TestWorker_RequeuesSkippedInFlightTelemetryTask(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

	device := models.Device{ID: "test-device-1", LastUpdatedAt: time.Now().Add(-time.Minute)}
	mockScheduler.EXPECT().
		AddDevices(gomock.Any(), device).
		Return(nil).
		Times(1)

	service := NewTelemetryService(
		Config{StalenessThreshold: time.Minute, FetchInterval: 10 * time.Second, ConcurrencyLimit: 1},
		mockDataStore,
		mockMinerGetter,
		mockScheduler,
		mockDeviceStore,
		mock.NewMockErrorPoller(ctrl),
	)
	service.inFlight.Store(device.ID, inFlightKindStatusOnly)
	service.tasks <- device
	close(service.tasks)

	done := make(chan struct{})
	go func() {
		service.worker(t.Context())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not exit after tasks channel closed")
	}
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

	mockMiner.EXPECT().GetOrgID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetSiteID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetDriverName().Return("").AnyTimes()

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

	mockMiner.EXPECT().GetOrgID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetSiteID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetDriverName().Return("").AnyTimes()

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

	mockMiner.EXPECT().GetOrgID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetSiteID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetDriverName().Return("").AnyTimes()

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
		Times(2) // Telemetry and error polling.

	mockMiner.EXPECT().GetOrgID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetSiteID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetDriverName().Return("").AnyTimes()

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

	done := make(chan error, 1)
	go func() {
		done <- service.processDevice(ctx, device)
	}()

	// Act & Assert - processDevice should complete without blocking
	select {
	case err := <-done:
		// Success - processDevice completed without blocking
		assert.ErrorContains(t, err, "status results channel full")
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

	// Telemetry fetch (fetchTelemetryFromMiner),
	// status fetch (fetchStatusFromMiner), and error polling each call GetMinerFromDeviceIdentifier.
	mockMinerGetter.EXPECT().
		GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).
		Return(mockMiner, nil).
		Times(3)

	mockMiner.EXPECT().GetOrgID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetSiteID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetDriverName().Return("").AnyTimes()

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
	require.NoError(t, service.processDevice(ctx, device))

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

	// Telemetry fetch and error polling
	// each call GetMinerFromDeviceIdentifier — two calls total.
	// GetDeviceStatus must NOT be called when hasMetricsStatus == true.
	mockMinerGetter.EXPECT().
		GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).
		Return(mockMiner, nil).
		Times(2)

	mockMiner.EXPECT().GetOrgID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetSiteID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetDriverName().Return("").AnyTimes()

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
	require.NoError(t, service.processDevice(ctx, device))
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

	// Telemetry fetch, status fetch, and error polling each
	// call GetMinerFromDeviceIdentifier — three calls total.
	mockMinerGetter.EXPECT().
		GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).
		Return(mockMiner, nil).
		Times(3)

	mockMiner.EXPECT().GetOrgID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetSiteID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetDriverName().Return("").AnyTimes()

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
	require.NoError(t, service.processDevice(ctx, device))

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

func TestService_GetCombinedMetrics_GatesLiveUptimeBar(t *testing.T) {
	t.Run("skips live state counts when uptime is not requested", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
		mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
		service := NewTelemetryService(Config{}, mockDataStore, nil, nil, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

		mockDataStore.EXPECT().
			GetCombinedMetrics(gomock.Any(), gomock.Any()).
			Return(models.CombinedMetric{Metrics: []models.Metric{}}, nil)
		mockDeviceStore.EXPECT().GetMinerStateCounts(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)

		result, err := service.GetCombinedMetrics(t.Context(), models.CombinedMetricsQuery{
			OrganizationID:   42,
			DeviceIDs:        []models.DeviceIdentifier{"device-a"},
			MeasurementTypes: []models.MeasurementType{models.MeasurementTypeHashrate},
			AggregationTypes: []models.AggregationType{models.AggregationTypeAverage},
		})

		require.NoError(t, err)
		assert.Nil(t, result.MinerStateCounts)
		assert.Empty(t, result.UptimeStatusCounts)
	})

	t.Run("appends live state counts when uptime is requested", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
		mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
		service := NewTelemetryService(Config{}, mockDataStore, nil, nil, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

		deviceIDs := []models.DeviceIdentifier{"device-a"}
		mockDataStore.EXPECT().
			GetCombinedMetrics(gomock.Any(), gomock.Any()).
			Return(models.CombinedMetric{Metrics: []models.Metric{}}, nil)
		mockDeviceStore.EXPECT().
			GetMinerStateCounts(gomock.Any(), int64(42), &stores.MinerFilter{DeviceIdentifiers: []string{"device-a"}}).
			Return(&telemetryv1.MinerStateCounts{
				HashingCount: 2,
				OfflineCount: 1,
			}, nil)

		result, err := service.GetCombinedMetrics(t.Context(), models.CombinedMetricsQuery{
			OrganizationID:   42,
			DeviceIDs:        deviceIDs,
			MeasurementTypes: []models.MeasurementType{models.MeasurementTypeHashrate, models.MeasurementTypeUptime},
			AggregationTypes: []models.AggregationType{models.AggregationTypeAverage},
		})

		require.NoError(t, err)
		require.NotNil(t, result.MinerStateCounts)
		assert.Equal(t, int32(2), result.MinerStateCounts.Hashing)
		require.Len(t, result.UptimeStatusCounts, 1)
		assert.Equal(t, int32(2), result.UptimeStatusCounts[0].HashingCount)
		assert.Equal(t, int32(1), result.UptimeStatusCounts[0].NotHashingCount)
	})
}

func TestPersistFirmwareVersionIfChanged(t *testing.T) {
	const deviceID = models.DeviceIdentifier("device-1")
	const firmwareV1 = "1.2.3"
	const firmwareV2 = "1.2.4"

	t.Run("skips ambiguous empty firmware version from telemetry", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
		service := NewTelemetryService(Config{ConcurrencyLimit: 1}, nil, nil, nil, mockDeviceStore, nil)

		service.persistFirmwareVersionIfChanged(t.Context(), 1, deviceID, "")
	})

	t.Run("persists new firmware version", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
		mockDeviceStore.EXPECT().
			UpdateFirmwareVersion(gomock.Any(), deviceID, firmwareV1).
			Return(nil)

		service := NewTelemetryService(Config{ConcurrencyLimit: 1}, nil, nil, nil, mockDeviceStore, nil)

		service.persistFirmwareVersionIfChanged(t.Context(), 1, deviceID, firmwareV1)
	})

	t.Run("skips when firmware version unchanged", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
		mockDeviceStore.EXPECT().
			UpdateFirmwareVersion(gomock.Any(), deviceID, firmwareV1).
			Return(nil)

		service := NewTelemetryService(Config{ConcurrencyLimit: 1}, nil, nil, nil, mockDeviceStore, nil)

		// First call persists
		service.persistFirmwareVersionIfChanged(t.Context(), 1, deviceID, firmwareV1)
		// Second call with same version should not call UpdateFirmwareVersion again
		service.persistFirmwareVersionIfChanged(t.Context(), 1, deviceID, firmwareV1)
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

		service.persistFirmwareVersionIfChanged(t.Context(), 1, deviceID, firmwareV1)
		service.persistFirmwareVersionIfChanged(t.Context(), 1, deviceID, firmwareV2)
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

		service.persistFirmwareVersionIfChanged(t.Context(), 1, deviceID, firmwareV1)
		service.persistFirmwareVersionIfChanged(t.Context(), 1, deviceID, firmwareV1)
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

	t.Run("explicit non-uptime measurements skip MinerStateCounts", func(t *testing.T) {
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
			DeviceIDs:        []models.DeviceIdentifier{"device-a"},
			MeasurementTypes: []models.MeasurementType{models.MeasurementTypeHashrate},
			Granularity:      5 * time.Minute,
			UpdateInterval:   5 * time.Minute,
			OrganizationID:   42,
		}

		mockDataStore.EXPECT().
			GetCombinedMetrics(gomock.Any(), gomock.Any()).
			Return(models.CombinedMetric{Metrics: []models.Metric{}}, nil)
		mockDeviceStore.EXPECT().GetMinerStateCounts(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)

		updateChan := make(chan models.CombinedMetric, 1)
		err := service.sendCombinedMetricUpdate(t.Context(), updateChan, query, 5*time.Minute)
		require.NoError(t, err)

		result := <-updateChan
		assert.Nil(t, result.MinerStateCounts)
		assert.Empty(t, result.UptimeStatusCounts)
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

// TestFetchStatusFromMiner_ConnectionErrorResolvesOrgFromDeviceStore confirms
// that when miner construction itself fails with a connection error (no handle
// returned), the offline status carries the trusted (org_id, driver_name)
// resolved from the device store. Without this fallback, fleet_device_online
// for unreachable devices would be emitted without organization_id and miss
// org-scoped alert routing.
func TestFetchStatusFromMiner_ConnectionErrorResolvesOrgFromDeviceStore(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

	deviceID := models.DeviceIdentifier("offline-constructor-fail")
	connErr := fleeterror.NewConnectionError(string(deviceID), errors.New("dial tcp: i/o timeout"))

	mockMinerGetter.EXPECT().
		GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).
		Return(nil, connErr)

	mockDeviceStore.EXPECT().
		GetDeviceOrgDriverAndSite(gomock.Any(), deviceID).
		Return(int64(42), "antminer", int64(0), nil)

	service := NewTelemetryService(Config{
		StalenessThreshold: 1 * time.Minute,
		FetchInterval:      10 * time.Second,
		ConcurrencyLimit:   5,
	}, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

	status, orgID, driverName, _, err := service.fetchStatusFromMiner(t.Context(), deviceID)

	require.NoError(t, err)
	assert.Equal(t, mm.MinerStatusOffline, status)
	assert.Equal(t, int64(42), orgID, "trusted org_id from device store must label the offline sample")
	assert.Equal(t, "antminer", driverName, "trusted driver_name from device store must label the offline sample")
}

// TestFetchStatusFromMiner_ConnectionErrorWithMissingDeviceRowDowngradesGracefully
// covers the rare case where the trusted device store also can't resolve the
// device (e.g., row was deleted concurrently). The offline status is still
// emitted; the metric just ends up unscoped at org=0/"" — which matches the
// pre-fix behavior for every connection-error device.
func TestFetchStatusFromMiner_ConnectionErrorWithMissingDeviceRowDowngradesGracefully(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	mockScheduler := mock.NewMockUpdateScheduler(ctrl)
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)

	deviceID := models.DeviceIdentifier("offline-gone")
	connErr := fleeterror.NewConnectionError(string(deviceID), errors.New("connection refused"))

	mockMinerGetter.EXPECT().
		GetMinerFromDeviceIdentifier(gomock.Any(), deviceID).
		Return(nil, connErr)

	mockDeviceStore.EXPECT().
		GetDeviceOrgDriverAndSite(gomock.Any(), deviceID).
		Return(int64(0), "", int64(0), fleeterror.NewNotFoundErrorf("device not found: %s", deviceID))

	service := NewTelemetryService(Config{
		StalenessThreshold: 1 * time.Minute,
		FetchInterval:      10 * time.Second,
		ConcurrencyLimit:   5,
	}, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

	status, orgID, driverName, _, err := service.fetchStatusFromMiner(t.Context(), deviceID)

	require.NoError(t, err)
	assert.Equal(t, mm.MinerStatusOffline, status)
	assert.Zero(t, orgID)
	assert.Empty(t, driverName)
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

	// fetchStatusFromMiner invalidates on auth error; guarded remediation
	// invalidates again when it changes pairing state.
	mockMinerGetter.EXPECT().
		InvalidateMiner(deviceID).
		Times(2)

	// processStatusOnly routes auth remediation through a guarded transition.
	mockDeviceStore.EXPECT().
		ReconcileAuthenticationNeededPairingStatusByIdentifier(gomock.Any(), string(deviceID)).
		Return(true, true, nil)

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

	mockMiner.EXPECT().GetOrgID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetSiteID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetDriverName().Return("").AnyTimes()

	// GetDeviceStatus returns an auth error (e.g., token rotated)
	mockMiner.EXPECT().
		GetDeviceStatus(gomock.Any()).
		Return(mm.MinerStatusUnknown, authErr)

	// fetchStatusFromMiner invalidates on auth error; guarded remediation
	// invalidates again when it changes pairing state.
	mockMinerGetter.EXPECT().
		InvalidateMiner(deviceID).
		Times(2)

	// An auth error moves the device into AUTHENTICATION_NEEDED only through
	// the guarded remediation transition.
	mockDeviceStore.EXPECT().
		ReconcileAuthenticationNeededPairingStatusByIdentifier(gomock.Any(), string(deviceID)).
		Return(true, true, nil)

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

	mockMiner.EXPECT().GetOrgID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetSiteID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetDriverName().Return("").AnyTimes()

	mockMiner.EXPECT().
		GetDeviceStatus(gomock.Any()).
		Return(mm.MinerStatusUnknown, forbiddenErr)

	// A default-password forbidden error moves the device into the distinct
	// DEFAULT_PASSWORD remediation state (not AUTHENTICATION_NEEDED).
	mockDeviceStore.EXPECT().
		ReconcileDefaultPasswordPairingStatusByIdentifier(gomock.Any(), string(deviceID), pairing.StatusDefaultPassword).
		Return(true, true, nil)
	mockMinerGetter.EXPECT().
		InvalidateMiner(deviceID)

	service := NewTelemetryService(Config{
		StalenessThreshold: 1 * time.Minute,
		FetchInterval:      10 * time.Second,
		ConcurrencyLimit:   5,
	}, mockDataStore, mockMinerGetter, mockScheduler, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

	ctx := t.Context()
	device := models.Device{ID: deviceID}

	service.processStatusOnly(ctx, device)
}

func TestReconcileDefaultPasswordState(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Arrange
	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	deviceID := models.DeviceIdentifier("device-dp")
	service := NewTelemetryService(Config{},
		mock.NewMockTelemetryDataStore(ctrl), mockMinerGetter,
		mock.NewMockUpdateScheduler(ctrl), mockDeviceStore, mock.NewMockErrorPoller(ctrl))
	ctx := t.Context()
	ptr := func(b bool) *bool { return &b }
	mockMinerGetter.EXPECT().InvalidateMiner(deviceID).Times(3)

	// An undetermined reading (nil) never writes — keeps the current status.
	service.reconcileDefaultPasswordState(ctx, deviceID, nil)

	// First determined reading writes the matching status (so a device whose
	// password changed while the server was down is corrected on the next poll)...
	mockDeviceStore.EXPECT().
		ReconcileDefaultPasswordPairingStatusByIdentifier(gomock.Any(), string(deviceID), pairing.StatusPaired).
		Return(true, true, nil)
	service.reconcileDefaultPasswordState(ctx, deviceID, ptr(false))
	// ...and is not rewritten while unchanged.
	service.reconcileDefaultPasswordState(ctx, deviceID, ptr(false))

	// Becoming default-password writes DEFAULT_PASSWORD once, and an undetermined
	// read afterward must not demote it.
	mockDeviceStore.EXPECT().
		ReconcileDefaultPasswordPairingStatusByIdentifier(gomock.Any(), string(deviceID), pairing.StatusDefaultPassword).
		Return(true, true, nil)
	service.reconcileDefaultPasswordState(ctx, deviceID, ptr(true))
	service.reconcileDefaultPasswordState(ctx, deviceID, ptr(true))
	service.reconcileDefaultPasswordState(ctx, deviceID, nil)

	// Clearing the default password demotes back to PAIRED.
	mockDeviceStore.EXPECT().
		ReconcileDefaultPasswordPairingStatusByIdentifier(gomock.Any(), string(deviceID), pairing.StatusPaired).
		Return(true, true, nil)
	service.reconcileDefaultPasswordState(ctx, deviceID, ptr(false))

	// Assert — gomock verifies each ReconcileDefaultPasswordPairingStatusByIdentifier ran exactly once.
}

func TestReconcileDefaultPasswordState_EligibleNoopDoesNotInvalidateMiner(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
	mockMinerGetter := mock.NewMockCachedMinerGetter(ctrl)
	deviceID := models.DeviceIdentifier("device-dp-noop")
	service := NewTelemetryService(Config{},
		mock.NewMockTelemetryDataStore(ctrl), mockMinerGetter,
		mock.NewMockUpdateScheduler(ctrl), mockDeviceStore, mock.NewMockErrorPoller(ctrl))
	ctx := t.Context()
	active := false

	mockDeviceStore.EXPECT().
		ReconcileDefaultPasswordPairingStatusByIdentifier(gomock.Any(), string(deviceID), pairing.StatusPaired).
		Return(true, false, nil)

	service.reconcileDefaultPasswordState(ctx, deviceID, &active)
	service.reconcileDefaultPasswordState(ctx, deviceID, &active)
}

func TestReconcileDefaultPasswordState_IneligibleRowsAreCached(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
	deviceID := models.DeviceIdentifier("device-dp-unpaired")
	service := NewTelemetryService(Config{},
		mock.NewMockTelemetryDataStore(ctrl), mock.NewMockCachedMinerGetter(ctrl),
		mock.NewMockUpdateScheduler(ctrl), mockDeviceStore, mock.NewMockErrorPoller(ctrl))
	ctx := t.Context()
	active := false
	service.lastDefaultPwActive.Store(deviceID, true)

	mockDeviceStore.EXPECT().
		ReconcileDefaultPasswordPairingStatusByIdentifier(gomock.Any(), string(deviceID), pairing.StatusPaired).
		Return(false, false, nil).
		Times(1)

	service.reconcileDefaultPasswordState(ctx, deviceID, &active)
	service.reconcileDefaultPasswordState(ctx, deviceID, &active)
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

	mockMiner.EXPECT().GetOrgID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetSiteID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetDriverName().Return("").AnyTimes()

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

func TestWriteFleetMetricRollups(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 5, 17, 0, time.UTC)
	end := models.TruncateToFleetRollupBucket(now).Add(-time.Duration(models.FleetMetricRollupRawTailBuckets) * models.FleetMetricRollupBucketDuration)
	start := end.Add(-fleetRollupBackfillFloor)

	t.Run("upserts the next bounded window", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
		mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
		mockDataStore.EXPECT().
			GetLatestFleetMetricRollupBucket(gomock.Any()).
			Return(start.Add(-models.FleetMetricRollupBucketDuration), nil)
		mockDataStore.EXPECT().
			UpsertFleetMetricRollups(gomock.Any(), start, start.Add(time.Duration(fleetRollupMaxBucketsPerTick)*models.FleetMetricRollupBucketDuration)).
			Return(nil)

		service := NewTelemetryService(Config{ConcurrencyLimit: 1}, mockDataStore, nil, nil, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

		service.writeFleetMetricRollups(t.Context(), now)
	})

	t.Run("skips when latest lookup fails", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockDataStore := mock.NewMockTelemetryDataStore(ctrl)
		mockDeviceStore := storesMocks.NewMockDeviceStore(ctrl)
		mockDataStore.EXPECT().
			GetLatestFleetMetricRollupBucket(gomock.Any()).
			Return(time.Time{}, errors.New("db down"))
		mockDataStore.EXPECT().
			UpsertFleetMetricRollups(gomock.Any(), gomock.Any(), gomock.Any()).
			Times(0)

		service := NewTelemetryService(Config{ConcurrencyLimit: 1}, mockDataStore, nil, nil, mockDeviceStore, mock.NewMockErrorPoller(ctrl))

		service.writeFleetMetricRollups(t.Context(), now)
	})
}

func TestFleetMetricRollupWriteWindow(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 5, 17, 0, time.UTC)
	end := models.TruncateToFleetRollupBucket(now).Add(-time.Duration(models.FleetMetricRollupRawTailBuckets) * models.FleetMetricRollupBucketDuration)
	floor := end.Add(-fleetRollupBackfillFloor)

	tests := []struct {
		name      string
		latest    time.Time
		wantStart time.Time
		wantEnd   time.Time
		wantOK    bool
	}{
		{
			name:      "empty table starts at the six hour floor and writes one tick of buckets",
			latest:    time.Unix(0, 0).UTC(),
			wantStart: floor,
			wantEnd:   floor.Add(time.Duration(fleetRollupMaxBucketsPerTick) * models.FleetMetricRollupBucketDuration),
			wantOK:    true,
		},
		{
			name:      "continues after latest bucket and rewrites recent buckets",
			latest:    floor.Add(10 * models.FleetMetricRollupBucketDuration),
			wantStart: floor.Add(9 * models.FleetMetricRollupBucketDuration),
			wantEnd:   floor.Add(49 * models.FleetMetricRollupBucketDuration),
			wantOK:    true,
		},
		{
			name:      "rewrites overlap plus a single remaining bucket",
			latest:    end.Add(-2 * models.FleetMetricRollupBucketDuration),
			wantStart: end.Add(-3 * models.FleetMetricRollupBucketDuration),
			wantEnd:   end,
			wantOK:    true,
		},
		{
			name:   "skips when the latest bucket reaches the safe end",
			latest: end.Add(-models.FleetMetricRollupBucketDuration),
			wantOK: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotStart, gotEnd, gotOK := fleetMetricRollupWriteWindow(now, tc.latest)

			assert.Equal(t, tc.wantOK, gotOK)
			if !tc.wantOK {
				return
			}
			assert.Equal(t, tc.wantStart, gotStart)
			assert.Equal(t, tc.wantEnd, gotEnd)
		})
	}
}

// combinedMetricsQueryAt builds a dashboard-like query (90s granularity)
// whose end time lands endOffset past a fixed quantum-aligned base, with a
// 24h duration. Offsets within the same 15s quantum must quantize (and
// therefore key) identically.
func combinedMetricsQueryAt(org int64, endOffset time.Duration) models.CombinedMetricsQuery {
	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	end := base.Add(endOffset)
	start := end.Add(-24 * time.Hour)
	slide := 90 * time.Second
	return models.CombinedMetricsQuery{
		OrganizationID:   org,
		MeasurementTypes: []models.MeasurementType{models.MeasurementTypeHashrate},
		TimeRange:        models.TimeRange{StartTime: &start, EndTime: &end},
		SlideInterval:    &slide,
	}
}

func newCombinedMetricsTestService(t *testing.T) (*TelemetryService, *mock.MockTelemetryDataStore) {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	dataStore := mock.NewMockTelemetryDataStore(ctrl)
	svc := NewTelemetryService(Config{
		StalenessThreshold: time.Minute,
		FetchInterval:      10 * time.Second,
		ConcurrencyLimit:   5,
	}, dataStore, nil, nil, storesMocks.NewMockDeviceStore(ctrl), mock.NewMockErrorPoller(ctrl))
	return svc, dataStore
}

// TestQuantizeCombinedMetricsWindow verifies coarsening applies only when the
// requested bucket interval makes a sub-quantum shift invisible; finer
// queries keep their exact bounds.
func TestQuantizeCombinedMetricsWindow(t *testing.T) {
	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	coarse := 90 * time.Second
	fine := 10 * time.Second
	end := base.Add(7 * time.Second)
	start := end.Add(-time.Hour)

	tests := []struct {
		name          string
		slideInterval *time.Duration
		wantStart     time.Time
		wantEnd       time.Time
	}{
		{"coarse interval shifts window onto the quantum grid", &coarse, base.Add(-time.Hour), base},
		{"fine interval keeps exact bounds", &fine, start, end},
		{"nil interval keeps exact bounds", nil, start, end},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			query := models.CombinedMetricsQuery{
				TimeRange:     models.TimeRange{StartTime: &start, EndTime: &end},
				SlideInterval: tc.slideInterval,
			}

			// Act
			got := quantizeCombinedMetricsWindow(query)

			// Assert
			assert.Equal(t, tc.wantStart, *got.TimeRange.StartTime)
			assert.Equal(t, tc.wantEnd, *got.TimeRange.EndTime)
		})
	}
}

// TestCombinedMetricsFlightKey verifies which query variations may share a
// flight: pure filter sets (DeviceIDs, SiteIDs) collapse regardless of
// order, order-sensitive slices (MeasurementTypes, AggregationTypes) do not
// because the store emits results in request slice order, and pagination
// fields the combined-metrics path ignores never split a key.
func TestCombinedMetricsFlightKey(t *testing.T) {
	base := func() models.CombinedMetricsQuery {
		q := combinedMetricsQueryAt(42, 3*time.Second)
		q.MeasurementTypes = []models.MeasurementType{models.MeasurementTypeHashrate, models.MeasurementTypePower}
		q.AggregationTypes = []models.AggregationType{models.AggregationTypeAverage, models.AggregationTypeMax}
		q.DeviceIDs = []models.DeviceIdentifier{"device-a", "device-b"}
		q.SiteIDs = []int64{1, 2}
		return q
	}

	tests := []struct {
		name     string
		mutate   func(*models.CombinedMetricsQuery)
		wantSame bool
	}{
		{
			"pagination fields do not split the key",
			func(q *models.CombinedMetricsQuery) {
				q.PageSize = 500
				q.PaginationToken = "token"
			},
			true,
		},
		{
			"device ID order does not split the key",
			func(q *models.CombinedMetricsQuery) {
				q.DeviceIDs = []models.DeviceIdentifier{"device-b", "device-a"}
			},
			true,
		},
		{
			"site ID order does not split the key",
			func(q *models.CombinedMetricsQuery) {
				q.SiteIDs = []int64{2, 1}
			},
			true,
		},
		{
			"measurement type order splits the key",
			func(q *models.CombinedMetricsQuery) {
				q.MeasurementTypes = []models.MeasurementType{models.MeasurementTypePower, models.MeasurementTypeHashrate}
			},
			false,
		},
		{
			"aggregation type order splits the key",
			func(q *models.CombinedMetricsQuery) {
				q.AggregationTypes = []models.AggregationType{models.AggregationTypeMax, models.AggregationTypeAverage}
			},
			false,
		},
		{
			"site-derived device list splits the key",
			func(q *models.CombinedMetricsQuery) {
				q.DeviceListFromSiteScope = true
			},
			false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			query := base()
			tc.mutate(&query)

			// Act
			got := combinedMetricsFlightKey(query)

			// Assert
			if tc.wantSame {
				assert.Equal(t, combinedMetricsFlightKey(base()), got)
			} else {
				assert.NotEqual(t, combinedMetricsFlightKey(base()), got)
			}
		})
	}
}

// TestGetCombinedMetrics_Singleflight verifies identical concurrent queries
// collapse into one store execution, non-collapsing queries (distinct fields,
// fine granularity skew, nil bounds) do not, a canceled caller returns
// promptly without poisoning the shared flight, and the shared query is
// cancelled once no waiter remains.
func TestGetCombinedMetrics_Singleflight(t *testing.T) {
	quantumBase := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

	t.Run("identical concurrent queries share one store call", func(t *testing.T) {
		// Arrange
		svc, dataStore := newCombinedMetricsTestService(t)
		started := make(chan struct{})
		release := make(chan struct{})
		want := models.CombinedMetric{Metrics: []models.Metric{{MeasurementType: models.MeasurementTypeHashrate}}}
		dataStore.EXPECT().
			GetCombinedMetrics(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, q models.CombinedMetricsQuery) (models.CombinedMetric, error) {
				// The executed query must carry the quantized bounds so the
				// shared result matches what followers keyed on.
				assert.Equal(t, quantumBase, *q.TimeRange.EndTime)
				assert.Equal(t, quantumBase.Add(-24*time.Hour), *q.TimeRange.StartTime)
				close(started)
				<-release
				return want, nil
			})

		results := make(chan models.CombinedMetric, 2)
		errs := make(chan error, 2)
		run := func(q models.CombinedMetricsQuery) {
			res, err := svc.GetCombinedMetrics(t.Context(), q)
			results <- res
			errs <- err
		}

		// Act: end times 3s and 7s past the quantum boundary collapse to the
		// same key. The store blocks until released, guaranteeing overlap.
		go run(combinedMetricsQueryAt(42, 3*time.Second))
		<-started
		go run(combinedMetricsQueryAt(42, 7*time.Second))
		// Let the second caller join the pending flight before the leader is
		// released; if it misses and re-queries, the mock fails on call count.
		time.Sleep(50 * time.Millisecond)
		close(release)

		// Assert
		for range 2 {
			require.NoError(t, <-errs)
			assert.Equal(t, want, <-results)
		}
	})

	t.Run("non-collapsing queries do not share a flight", func(t *testing.T) {
		differentMeasurements := combinedMetricsQueryAt(1, 3*time.Second)
		differentMeasurements.MeasurementTypes = []models.MeasurementType{models.MeasurementTypePower}

		fineSlide := 10 * time.Second
		fineA := combinedMetricsQueryAt(1, 3*time.Second)
		fineA.SlideInterval = &fineSlide
		fineB := combinedMetricsQueryAt(1, 7*time.Second)
		fineB.SlideInterval = &fineSlide

		// Nil bounds resolve against time.Now() inside the store, so even
		// byte-identical nil-bound queries must bypass singleflight.
		nilBoundsA := combinedMetricsQueryAt(1, 3*time.Second)
		nilBoundsA.TimeRange = models.TimeRange{}
		nilBoundsB := combinedMetricsQueryAt(1, 3*time.Second)
		nilBoundsB.TimeRange = models.TimeRange{}

		tests := []struct {
			name   string
			queryA models.CombinedMetricsQuery
			queryB models.CombinedMetricsQuery
		}{
			{"different orgs", combinedMetricsQueryAt(1, 3*time.Second), combinedMetricsQueryAt(2, 3*time.Second)},
			{"different measurement sets", combinedMetricsQueryAt(1, 3*time.Second), differentMeasurements},
			{"fine granularity with skewed windows", fineA, fineB},
			{"identical nil time bounds", nilBoundsA, nilBoundsB},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				// Arrange
				svc, dataStore := newCombinedMetricsTestService(t)
				started := make(chan struct{}, 2)
				release := make(chan struct{})
				dataStore.EXPECT().
					GetCombinedMetrics(gomock.Any(), gomock.Any()).
					Times(2).
					DoAndReturn(func(_ context.Context, _ models.CombinedMetricsQuery) (models.CombinedMetric, error) {
						started <- struct{}{}
						<-release
						return models.CombinedMetric{}, nil
					})

				errs := make(chan error, 2)

				// Act
				go func() {
					_, err := svc.GetCombinedMetrics(t.Context(), tc.queryA)
					errs <- err
				}()
				go func() {
					_, err := svc.GetCombinedMetrics(t.Context(), tc.queryB)
					errs <- err
				}()

				// Assert: both queries reach the store concurrently, proving
				// they run as separate flights.
				for range 2 {
					select {
					case <-started:
					case <-time.After(5 * time.Second):
						t.Fatal("expected two concurrent store calls")
					}
				}
				close(release)
				require.NoError(t, <-errs)
				require.NoError(t, <-errs)
			})
		}
	})

	t.Run("canceled caller returns while sibling still gets the result", func(t *testing.T) {
		// Arrange
		svc, dataStore := newCombinedMetricsTestService(t)
		started := make(chan struct{})
		release := make(chan struct{})
		want := models.CombinedMetric{Metrics: []models.Metric{{MeasurementType: models.MeasurementTypeHashrate}}}
		dataStore.EXPECT().
			GetCombinedMetrics(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, _ models.CombinedMetricsQuery) (models.CombinedMetric, error) {
				close(started)
				// The flight context must be detached from the leader: if the
				// leader's cancellation propagated here, this would error and
				// poison the follower's result below.
				select {
				case <-release:
					return want, nil
				case <-ctx.Done():
					return models.CombinedMetric{}, ctx.Err()
				}
			})

		leaderCtx, cancelLeader := context.WithCancel(t.Context())
		leaderErrs := make(chan error, 1)
		go func() {
			_, err := svc.GetCombinedMetrics(leaderCtx, combinedMetricsQueryAt(42, 3*time.Second))
			leaderErrs <- err
		}()
		<-started

		followerResults := make(chan models.CombinedMetric, 1)
		followerErrs := make(chan error, 1)
		go func() {
			res, err := svc.GetCombinedMetrics(t.Context(), combinedMetricsQueryAt(42, 7*time.Second))
			followerResults <- res
			followerErrs <- err
		}()
		time.Sleep(50 * time.Millisecond) // let the follower join the pending flight

		// Act
		cancelLeader()

		// Assert: the canceled caller returns promptly even though the store
		// call is still blocked.
		select {
		case err := <-leaderErrs:
			require.ErrorIs(t, err, context.Canceled)
		case <-time.After(5 * time.Second):
			t.Fatal("canceled caller did not return promptly")
		}

		close(release)
		require.NoError(t, <-followerErrs)
		assert.Equal(t, want, <-followerResults)
	})

	t.Run("last waiter leaving cancels the shared query", func(t *testing.T) {
		// Arrange
		svc, dataStore := newCombinedMetricsTestService(t)
		started := make(chan struct{})
		storeCtxErr := make(chan error, 1)
		want := models.CombinedMetric{Metrics: []models.Metric{{MeasurementType: models.MeasurementTypeHashrate}}}
		gomock.InOrder(
			dataStore.EXPECT().
				GetCombinedMetrics(gomock.Any(), gomock.Any()).
				DoAndReturn(func(ctx context.Context, _ models.CombinedMetricsQuery) (models.CombinedMetric, error) {
					close(started)
					<-ctx.Done()
					storeCtxErr <- ctx.Err()
					return models.CombinedMetric{}, ctx.Err()
				}),
			dataStore.EXPECT().
				GetCombinedMetrics(gomock.Any(), gomock.Any()).
				Return(want, nil),
		)

		callerCtx, cancelCaller := context.WithCancel(t.Context())
		errs := make(chan error, 1)
		go func() {
			_, err := svc.GetCombinedMetrics(callerCtx, combinedMetricsQueryAt(42, 3*time.Second))
			errs <- err
		}()
		<-started

		// Act
		cancelCaller()

		// Assert: the detached store query is cancelled once no waiter remains,
		// instead of running out the flight timeout.
		select {
		case err := <-storeCtxErr:
			require.ErrorIs(t, err, context.Canceled)
		case <-time.After(5 * time.Second):
			t.Fatal("store query was not cancelled after the last waiter left")
		}
		require.ErrorIs(t, <-errs, context.Canceled)

		// A later identical query starts a fresh flight rather than joining
		// the cancelled one.
		res, err := svc.GetCombinedMetrics(t.Context(), combinedMetricsQueryAt(42, 3*time.Second))
		require.NoError(t, err)
		assert.Equal(t, want, res)
	})
}
