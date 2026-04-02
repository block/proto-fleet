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
	// Sorted slice of devices, sorted by LastUpdatedAt. New devices are added at the end.
	devices []models.Device
	// Map of devices that have failed according to consumers of the scheduler.
	// After a device has failed a certain number of times, it is removed from the scheduler.
	failedDevices sync.Map
	// This is used to prevent duplicate devices from being added to the scheduler.
	// If value is true, the device is in the scheduler queue, false means it is checked out.
	managedDevices sync.Map
	mu             sync.Mutex

	config Config
}

// This used to search for and insert new devices into the scheduler.
// We are looking to add devices with the most recent timestamp at the end of the slice
func deviceTimeEqual(a, b models.Device) int {
	if a.LastUpdatedAt.Before(b.LastUpdatedAt) {
		return -1
	}
	if a.LastUpdatedAt.After(b.LastUpdatedAt) {
		return 1
	}
	return 0
}

//nolint:revive // It is okay and preferred to return a private type here, it forces the use of the constructor, and the scheduler should be managed and stored with interfaces client side.
func NewScheduler(config Config) *scheduler {
	return &scheduler{
		devices:        make([]models.Device, 0, 100),
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
		// Insert the new device at the end of the slice
		// This is a simple way to ensure that the most recent devices are at the end of the slice.
		s.mu.Lock()
		s.devices = append(s.devices, models.Device{
			ID:            id,
			LastUpdatedAt: time.Now(),
		})
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
			s.failedDevices.Delete(device.ID) // Remove from failed devices if it exists
		}
	}
	return s.addDevices(ctx, devices...)
}

func (s *scheduler) addDevices(ctx context.Context, devices ...models.Device) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, device := range devices {
		inQueue, exists := s.managedDevices.Load(device.ID)
		if !exists {
			return DeviceNotManagedErr{
				DeviceID: device.ID,
			}
		}
		if value, ok := inQueue.(bool); ok && value {
			slog.Warn("Device already scheduled", "device_id", device.ID)
			continue
		}
		s.managedDevices.Store(device.ID, true)

		insertPos, _ := slices.BinarySearchFunc(s.devices, device, deviceTimeEqual)
		s.devices = slices.Insert(s.devices, insertPos, device)
	}
	return nil
}

func (s *scheduler) AddFailedDevices(ctx context.Context, devices ...models.Device) error {

	for _, device := range devices {
		count, _ := s.failedDevices.LoadOrStore(device.ID, 0)

		// Check if the device is already marked as permanently failed (stored as time.Time)
		if _, isTime := count.(time.Time); isTime {
			slog.Debug("Device is already marked as failed, skipping", "device_id", device.ID)
			continue // Device is already permanently failed, skip it
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
			continue // Do not add back to the scheduler
		}
		if err := s.addDevices(ctx, device); err != nil {
			return err
		}
	}
	return nil
}

// Removes a device from scheduler management
func (s *scheduler) RemoveDevices(ctx context.Context, deviceID ...models.DeviceIdentifier) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, id := range deviceID {
		if _, exists := s.managedDevices.LoadAndDelete(id); !exists {
			return DeviceNotManagedErr{
				DeviceID: id,
			}
		}

		// Remove the device from the slice
		s.devices = slices.DeleteFunc(s.devices, func(d models.Device) bool { return d.ID == id })
	}
	return nil
}

// Fetches a slice of devices that where last updated before the given time.
// The devices fetched will not be provided by the scheduler until they are added back to the scheduler.
// Or until a general timeout is reached.
func (s *scheduler) FetchDevices(ctx context.Context, before time.Time) ([]models.Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Find the first device that is NOT older than threshold
	cutoffIndex, _ := slices.BinarySearchFunc(s.devices, models.Device{LastUpdatedAt: before}, deviceTimeEqual)

	if cutoffIndex == 0 {
		// No old devices
		return []models.Device{}, nil
	}

	// Extract old devices
	oldDevices := make([]models.Device, cutoffIndex)
	for i, device := range s.devices[:cutoffIndex] {
		oldDevices[i] = device
		s.managedDevices.Store(device.ID, false) // Mark as not in queue
	}

	// Remove old devices from the array
	s.devices = s.devices[cutoffIndex:]

	return oldDevices, nil
}

func (s *scheduler) GetAllDevices(ctx context.Context) ([]models.Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Return a copy of the devices slice to avoid external modifications
	devicesCopy := make([]models.Device, len(s.devices))
	copy(devicesCopy, s.devices)
	return devicesCopy, nil
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
		// if the value is a time.Time, it means the device is failed
		return true, failedAt, nil
	}
	return false, time.Time{}, nil
}
