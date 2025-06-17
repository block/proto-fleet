package telemetry

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"
)

//go:generate mockgen -source=service.go -destination=mocks/mock_service.go -package=mock UpdateScheduler
type UpdateScheduler interface {
	AddNewDevices(ctx context.Context, deviceID ...models.DeviceID) error
	AddDevices(ctx context.Context, device ...models.Device) error
	FetchDevices(ctx context.Context, after time.Time) ([]models.Device, error)
	RemoveDevices(ctx context.Context, deviceID ...models.DeviceID) error
}

type TelemetryDataStore interface {
	Store(ctx context.Context, data ...models.Telemetry) error
}

type TelemetryService struct {
	config             Config
	updateScheduler    UpdateScheduler
	telemetryDataStore TelemetryDataStore
	minerManager       MinerManager
	mux                sync.Mutex
	tasks              chan models.Device
	cancelFunc         context.CancelFunc
	lookBackDuration   time.Duration
}

type MinerManager interface {
	GetMinerFromDeviceID(ctx context.Context, deviceID models.DeviceID) (models.Miner, error)
}

func NewTelemetryService(config Config, telemetryDataStore TelemetryDataStore, minerManager MinerManager, scheduler UpdateScheduler) *TelemetryService {
	return &TelemetryService{
		config:             config,
		telemetryDataStore: telemetryDataStore,
		minerManager:       minerManager,
		updateScheduler:    scheduler,
		// channel for tasks to process telemetry data, it is set so that there is at least 1 queued task for every worker.
		tasks:            make(chan models.Device, config.ConcurrencyLimit),
		lookBackDuration: -1 * (config.StalenessThreshold - config.FetchInterval),
	}
}

func (s *TelemetryService) AddDevices(ctx context.Context, deviceID ...models.DeviceID) error {
	if len(deviceID) == 0 {
		return nil
	}
	return s.updateScheduler.AddNewDevices(ctx, deviceID...)
}

func (s *TelemetryService) RemoveDevices(ctx context.Context, deviceID ...models.DeviceID) error {
	if len(deviceID) == 0 {
		return nil
	}
	return s.updateScheduler.RemoveDevices(ctx, deviceID...)
}

func (s *TelemetryService) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	s.cancelFunc = cancel

	go s.gatherMetricsRoutine(ctx)
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

func (s *TelemetryService) worker(ctx context.Context) {
	tryAddDevice := func(devices ...models.Device) {
		err := s.updateScheduler.AddDevices(ctx, devices...)
		if err != nil {
			slog.Warn("failed to re-add device to update scheduler", "devices", devices, "error", err)
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case device := <-s.tasks:
			func(ctx context.Context, device models.Device) {
				ctx, cancel := context.WithTimeout(ctx, s.config.MetricTimeout)
				defer cancel()
				miner, err := s.minerManager.GetMinerFromDeviceID(ctx, device.ID)
				// TODO(DASH-446): update to handle dor unique miner discovery errors.
				if err != nil {
					slog.Error("failed to get miner from device ID", "deviceID", device.ID, "error", err)
					tryAddDevice(device)
					return
				}
				telemetry, err := miner.GetTelemetryMeasurements(ctx, device.LastUpdatedAt)
				if err != nil {
					slog.Error("failed to get telemetry measurements", "deviceID", device.ID, "error", err)
					tryAddDevice(device)
					return
				}
				err = s.telemetryDataStore.Store(ctx, telemetry...)
				if err != nil {
					slog.Error("failed to store telemetry data", "deviceID", device.ID, "error", err)
					tryAddDevice(device)
					return
				}
				err = s.updateScheduler.AddDevices(ctx, models.Device{
					ID:            device.ID,
					LastUpdatedAt: time.Now(),
				})
				if err != nil {
					slog.Error("failed to update device last updated time", "deviceID", device.ID, "error", err)
					return
				}

			}(ctx, device)
		}
	}
}
