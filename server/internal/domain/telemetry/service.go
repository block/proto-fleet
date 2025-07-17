package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/interfaces"
	stores "github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"
)

//go:generate mockgen -source=service.go -destination=mocks/mock_service.go -package=mock UpdateScheduler,TelemetryDataStore,MinerGetter
type UpdateScheduler interface {
	AddNewDevices(ctx context.Context, deviceID ...models.DeviceIdentifier) error
	AddDevices(ctx context.Context, devices ...models.Device) error
	AddFailedDevices(ctx context.Context, devices ...models.Device) error
	FetchDevices(ctx context.Context, after time.Time) ([]models.Device, error)
	RemoveDevices(ctx context.Context, deviceID ...models.DeviceIdentifier) error
}

type TelemetryDataStore interface {
	Store(ctx context.Context, data ...models.Telemetry) error
	GetLatestTelemetry(ctx context.Context, query models.LatestTelemetryQuery) ([]models.Telemetry, error)
	GetTimeSeriesTelemetry(ctx context.Context, query models.TimeSeriesTelemetryQuery) ([]models.Telemetry, error)
	GetTelemetryMetadata(ctx context.Context, query models.MetadataQuery) ([]models.DeviceMetadata, error)
	StreamTelemetryUpdates(ctx context.Context, query models.StreamQuery) (<-chan models.TelemetryUpdate, error)
	GetAggregatedTelemetry(ctx context.Context, query models.AggregationQuery) ([]models.AggregatedTelemetry, error)
	Ping(ctx context.Context) error
	Close() error
}
type MinerGetter interface {
	GetMinerFromDeviceIdentifier(ctx context.Context, deviceIdentifier models.DeviceIdentifier) (interfaces.Miner, error)
}

type TelemetryService struct {
	config             Config
	updateScheduler    UpdateScheduler
	telemetryDataStore TelemetryDataStore
	minerManager       MinerGetter
	deviceStore        stores.DeviceStore
	mux                sync.Mutex
	tasks              chan models.Device
	cancelFunc         context.CancelFunc
	lookBackDuration   time.Duration
}

func NewTelemetryService(config Config, telemetryDataStore TelemetryDataStore, minerManager MinerGetter, scheduler UpdateScheduler, deviceStore stores.DeviceStore) *TelemetryService {
	return &TelemetryService{
		config:             config,
		telemetryDataStore: telemetryDataStore,
		minerManager:       minerManager,
		updateScheduler:    scheduler,
		deviceStore:        deviceStore,
		// channel for tasks to process telemetry data, it is set so that there is at least 1 queued task for every worker.
		tasks:            make(chan models.Device, config.ConcurrencyLimit),
		lookBackDuration: -1 * (config.StalenessThreshold - config.FetchInterval),
	}
}

func (s *TelemetryService) AddDevices(ctx context.Context, deviceID ...models.DeviceIdentifier) error {
	if len(deviceID) == 0 {
		return nil
	}
	return s.updateScheduler.AddNewDevices(ctx, deviceID...)
}

func (s *TelemetryService) RemoveDevices(ctx context.Context, deviceID ...models.DeviceIdentifier) error {
	if len(deviceID) == 0 {
		return nil
	}
	return s.updateScheduler.RemoveDevices(ctx, deviceID...)
}

func (s *TelemetryService) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	s.cancelFunc = cancel

	go s.gatherMetricsRoutine(ctx)
	go s.devicePollingRoutine(ctx)
	return nil
}

func (s *TelemetryService) Stop(ctx context.Context) error {
	s.cancelFunc()
	defer close(s.tasks)
	return nil
}

func (s *TelemetryService) gatherMetricsRoutine(ctx context.Context) {
	if !s.mux.TryLock() {
		slog.Info("Telemetry gathering routine is already running")
		return // Another routine is already running
	}
	defer s.mux.Unlock()

	// Spin up workers to fetch telemetry data
	for range s.config.ConcurrencyLimit {
		go s.worker(ctx)
	}

	// Periodically fetch devices that need telemetry data
	ticker := time.NewTicker(s.config.FetchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Fetch devices that need telemetry data, considering the staleness threshold and fetch interval
			lookback := time.Now().Add(s.lookBackDuration)
			devices, err := s.updateScheduler.FetchDevices(ctx, lookback)
			if err != nil {
				slog.Error("failed to fetch devices for telemetry", "error", err)
				continue
			}
			for _, device := range devices {
				s.tasks <- device
			}
		}
	}
}

func (s *TelemetryService) devicePollingRoutine(ctx context.Context) {
	ticker := time.NewTicker(s.config.DevicePollInterval)
	defer ticker.Stop()

	// Run once immediately on startup
	if err := s.loadPairedDevices(ctx); err != nil {
		slog.Error("failed to load paired devices on startup", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.loadPairedDevices(ctx); err != nil {
				slog.Error("failed to load paired devices", "error", err)
			}
		}
	}
}

func (s *TelemetryService) loadPairedDevices(ctx context.Context) error {
	deviceIDs, err := s.deviceStore.GetAllPairedDeviceIdentifiers(ctx)
	if err != nil {
		return fmt.Errorf("failed to get paired device identifiers: %w", err)
	}

	if len(deviceIDs) == 0 {
		slog.Debug("no paired devices found to add to telemetry service")
		return nil
	}

	if err := s.AddDevices(ctx, deviceIDs...); err != nil {
		// failed to add devices is expected to happen from time to time.
		slog.Debug("failed to add paired devices to telemetry service", "error", err)
		return nil
	}

	slog.Debug("loaded paired devices into telemetry service", "count", len(deviceIDs))
	return nil
}

func (s *TelemetryService) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case device := <-s.tasks:
			if err := s.GetTelemetryFromDevice(ctx, device); err != nil {
				slog.Warn("failed to get telemetry from device", "deviceID", device.ID, "error", err)
				if err := s.updateScheduler.AddFailedDevices(ctx, device); err != nil {
					slog.Warn("failed to add failed telemetry device back into scheduler", "deviceID", device.ID, "error", err)
				}
			}
		}
	}
}

// GetTelemetryFromDevice fetches telemetry data from a specific device,
// and stores it in the telemetry data store
func (s *TelemetryService) GetTelemetryFromDevice(ctx context.Context, device models.Device) error {
	ctx, cancel := context.WithTimeout(ctx, s.config.MetricTimeout)
	defer cancel()
	miner, err := s.minerManager.GetMinerFromDeviceIdentifier(ctx, device.ID)
	// TODO(DASH-446): update to handle dor unique miner discovery errors.
	if err != nil {
		return fmt.Errorf("failed to get miner from device ID %s: %w", device.ID, err)
	}
	telemetry, err := miner.GetTelemetry(ctx, device.LastUpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to get telemetry measurements for device %s: %w", device.ID, err)
	}
	err = s.telemetryDataStore.Store(ctx, telemetry...)
	if err != nil {
		slog.Error("failed to store telemetry data", "deviceID", device.ID, "error", err)
		return fmt.Errorf("failed to store telemetry data for device %s: %w", device.ID, err)
	}
	err = s.updateScheduler.AddDevices(ctx, models.Device{
		ID:            device.ID,
		LastUpdatedAt: time.Now(),
	})
	if err != nil {
		return fmt.Errorf("failed to update device last updated time for device %s: %w", device.ID, err)
	}
	return nil
}

// GetLatestTelemetry delegates to the datastore to retrieve latest telemetry data
func (s *TelemetryService) GetLatestTelemetry(ctx context.Context, query models.LatestTelemetryQuery) ([]models.Telemetry, error) {
	return s.telemetryDataStore.GetLatestTelemetry(ctx, query)
}

func (s *TelemetryService) GetTimeSeriesTelemetry(ctx context.Context, query models.TimeSeriesTelemetryQuery) ([]models.Telemetry, error) {
	return s.telemetryDataStore.GetTimeSeriesTelemetry(ctx, query)
}

func (s *TelemetryService) GetTelemetryMetadata(ctx context.Context, query models.MetadataQuery) ([]models.DeviceMetadata, error) {
	return s.telemetryDataStore.GetTelemetryMetadata(ctx, query)
}

func (s *TelemetryService) StreamTelemetryUpdates(ctx context.Context, query models.StreamQuery) (<-chan models.TelemetryUpdate, error) {
	return s.telemetryDataStore.StreamTelemetryUpdates(ctx, query)
}

func (s *TelemetryService) GetAggregatedTelemetry(ctx context.Context, query models.AggregationQuery) ([]models.AggregatedTelemetry, error) {
	return s.telemetryDataStore.GetAggregatedTelemetry(ctx, query)
}

// Ping checks the health of the telemetry datastore
func (s *TelemetryService) Ping(ctx context.Context) error {
	return s.telemetryDataStore.Ping(ctx)
}
