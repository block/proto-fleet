package scheduler

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
)

type scheduler struct {
	// devices maps each managed device to its last-updated timestamp.
	devices map[models.DeviceIdentifier]time.Time
	// failedDevices tracks consecutive failure counts per device (int), transitioning to
	// the device's LastUpdatedAt (time.Time) once MaxConsecutiveFailures is reached.
	failedDevices sync.Map
	// managedDevices tracks queue state per device: true = queued, false = checked out.
	managedDevices sync.Map
	mu             sync.Mutex

	config Config
}

//nolint:revive // It is okay and preferred to return a private type here, it forces the use of the constructor, and the scheduler should be managed and stored with interfaces client side.
func NewScheduler(config Config) *scheduler {
	return &scheduler{
		devices:        make(map[models.DeviceIdentifier]time.Time),
		managedDevices: sync.Map{},
		mu:             sync.Mutex{},
		failedDevices:  sync.Map{},
		config:         config,
	}
}

// AddNewDevices adds new devices to the scheduler.
func (s *scheduler) AddNewDevices(ctx context.Context, deviceID ...models.DeviceIdentifier) error {

	for _, id := range deviceID {
		if _, exists := s.managedDevices.LoadOrStore(id, true); exists {
			slog.Warn("Device already managed", "device_id", id)
			continue
		}
		s.mu.Lock()
		s.devices[id] = time.Time{} // Zero time ensures device is immediately eligible for scheduling
		s.mu.Unlock()
		slog.Debug("Added new device to scheduler", "device_id", id)
		// TODO(Briano-block): do we want to fetch historical telemetry data for the new device?
		// where does our responsibility for telemetry data start and end? at pairing?
	}
	return nil
}

// AddDevices adds a device back into the scheduler.
func (s *scheduler) AddDevices(ctx context.Context, devices ...models.Device) error {
	for _, device := range devices {
		if _, ok := s.failedDevices.Load(device.ID); ok {
			s.failedDevices.Delete(device.ID)
		}
	}
	return s.addDevices(ctx, devices...)
}

func (s *scheduler) addDevices(ctx context.Context, devices ...models.Device) error {
	var alreadyScheduled []models.DeviceIdentifier

	s.mu.Lock()
	for _, device := range devices {
		inQueue, exists := s.managedDevices.Load(device.ID)
		if !exists {
			s.mu.Unlock()
			for _, id := range alreadyScheduled {
				slog.Warn("Device already scheduled", "device_id", id)
			}
			return DeviceNotManagedErr{DeviceID: device.ID}
		}
		if value, ok := inQueue.(bool); ok && value {
			alreadyScheduled = append(alreadyScheduled, device.ID)
			continue
		}
		s.managedDevices.Store(device.ID, true)
		s.devices[device.ID] = device.LastUpdatedAt
	}
	s.mu.Unlock()

	for _, id := range alreadyScheduled {
		slog.Warn("Device already scheduled", "device_id", id)
	}
	return nil
}

func (s *scheduler) AddFailedDevices(ctx context.Context, devices ...models.Device) error {

	for _, device := range devices {
		count, _ := s.failedDevices.LoadOrStore(device.ID, 0)

		// A time.Time value means the device has already reached MaxConsecutiveFailures.
		if _, isTime := count.(time.Time); isTime {
			slog.Debug("Device is already marked as failed, skipping", "device_id", device.ID)
			continue
		}

		failedCount, ok := count.(int)
		if !ok {
			slog.Error("Failed to convert failed count to int", "device_id", device.ID, "failed_count", count)
			return DeviceNotManagedErr{
				DeviceID: device.ID,
			}
		}

		failedCount++
		s.failedDevices.Store(device.ID, failedCount)

		if failedCount >= s.config.MaxConsecutiveFailures {
			slog.Warn("Device failed too many times, removing from scheduler", "device_id", device.ID, "failed_count", failedCount)
			s.failedDevices.Store(device.ID, device.LastUpdatedAt)
			continue
		}
		if err := s.addDevices(ctx, device); err != nil {
			return err
		}
	}
	return nil
}

// RemoveDevices removes a device from scheduler management.
func (s *scheduler) RemoveDevices(ctx context.Context, deviceID ...models.DeviceIdentifier) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, id := range deviceID {
		if _, exists := s.managedDevices.LoadAndDelete(id); !exists {
			return DeviceNotManagedErr{
				DeviceID: id,
			}
		}
		delete(s.devices, id)
	}
	return nil
}

// FetchDevices returns all devices last updated before the given time, sorted oldest-first.
// Returned devices are marked checked out and excluded from future fetches until re-added.
func (s *scheduler) FetchDevices(ctx context.Context, before time.Time) ([]models.Device, error) {
	s.mu.Lock()
	var stale []models.Device
	for id, lastUpdated := range s.devices {
		if lastUpdated.Before(before) {
			stale = append(stale, models.Device{ID: id, LastUpdatedAt: lastUpdated})
			s.managedDevices.Store(id, false) // Mark as checked out
			delete(s.devices, id)
		}
	}
	s.mu.Unlock()

	if stale == nil {
		return []models.Device{}, nil
	}
	slices.SortFunc(stale, func(a, b models.Device) int {
		return a.LastUpdatedAt.Compare(b.LastUpdatedAt)
	})
	return stale, nil
}

func (s *scheduler) GetAllDevices(ctx context.Context) ([]models.Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make([]models.Device, 0, len(s.devices))
	for id, lastUpdated := range s.devices {
		result = append(result, models.Device{ID: id, LastUpdatedAt: lastUpdated})
	}
	return result, nil
}

func (s *scheduler) GetDeviceCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.devices)
}

func (s *scheduler) GetManagedDeviceCount(ctx context.Context) (int, error) {
	count := 0
	s.managedDevices.Range(func(_, value any) bool {
		if val, ok := value.(bool); ok && val {
			count++
		}
		return true // continue iteration
	})
	return count, nil
}

func (s *scheduler) IsFailedDevice(ctx context.Context, deviceID models.DeviceIdentifier) (bool, time.Time, error) {
	lastUpdatedAt, exists := s.failedDevices.Load(deviceID)
	if !exists {
		return false, time.Time{}, nil
	}
	if failedAt, ok := lastUpdatedAt.(time.Time); ok {
		return true, failedAt, nil
	}
	return false, time.Time{}, nil
}
