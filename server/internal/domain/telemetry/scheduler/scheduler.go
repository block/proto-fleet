package scheduler

import (
	"container/heap"
	"context"
	"hash/fnv"
	"log/slog"
	"sync"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
)

type scheduler struct {
	devices map[models.DeviceIdentifier]*scheduledDevice
	queue   pollQueue
	// Failure counts become the last update time when a device reaches the limit.
	failedDevices sync.Map
	mu            sync.Mutex

	config        Config
	pollPeriod    time.Duration
	retryInterval time.Duration
	minGap        time.Duration
	now           func() time.Time
}

//nolint:revive // Callers use the constructor and store the scheduler through an interface.
func NewScheduler(config Config, pollPeriod, retryInterval time.Duration) *scheduler {
	if pollPeriod <= 0 {
		pollPeriod = 20 * time.Second
	}
	if retryInterval <= 0 {
		retryInterval = 5 * time.Second
	}
	return &scheduler{
		devices:       make(map[models.DeviceIdentifier]*scheduledDevice),
		config:        config,
		pollPeriod:    pollPeriod,
		retryInterval: retryInterval,
		minGap:        max(0, pollPeriod-retryInterval),
		now:           time.Now,
	}
}

// AddNewDevices makes a single device due immediately and spreads bulk additions across one poll period.
func (s *scheduler) AddNewDevices(ctx context.Context, deviceIDs ...models.DeviceIdentifier) error {
	var alreadyManaged duplicateDeviceLog
	addedAt := s.now()

	s.mu.Lock()
	for _, id := range deviceIDs {
		if _, exists := s.devices[id]; exists {
			alreadyManaged.add(id)
			continue
		}
		due := addedAt
		if len(deviceIDs) > 1 {
			due = due.Add(initialPollOffset(id, s.pollPeriod))
		}
		entry := &scheduledDevice{device: models.Device{ID: id}, due: due}
		s.devices[id] = entry
		heap.Push(&s.queue, entry)
	}
	s.mu.Unlock()

	alreadyManaged.log("scheduler skipped already managed devices")
	return nil
}

func initialPollOffset(deviceID models.DeviceIdentifier, period time.Duration) time.Duration {
	if period <= 0 {
		return 0
	}
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(deviceID))
	return time.Duration(hasher.Sum64() % uint64(period)) // #nosec G115 -- The positive period bounds both conversions.
}

type returnReason uint8

const (
	pollCompleted returnReason = iota
	pollBusy
	pollUnsent
	pollDeferred
)

// AddDevices schedules the next poll and clears consecutive failures.
func (s *scheduler) AddDevices(ctx context.Context, devices ...models.Device) error {
	return s.returnDevices(devices, pollCompleted)
}

// RequeueDevices schedules the next poll after a capacity rejection and keeps the failure count.
func (s *scheduler) RequeueDevices(ctx context.Context, devices ...models.Device) error {
	return s.returnDevices(devices, pollBusy)
}

// RestoreDevices returns unprocessed devices with their previous due times and failure counts.
func (s *scheduler) RestoreDevices(ctx context.Context, devices ...models.Device) error {
	return s.returnDevices(devices, pollUnsent)
}

// DeferDevices queues a poll skipped during another device operation.
func (s *scheduler) DeferDevices(ctx context.Context, devices ...models.Device) error {
	return s.returnDevices(devices, pollDeferred)
}

func (s *scheduler) returnDevices(devices []models.Device, reason returnReason) error {
	var alreadyScheduled duplicateDeviceLog
	var err error

	s.mu.Lock()
	for _, device := range devices {
		entry, exists := s.devices[device.ID]
		if !exists {
			err = DeviceNotManagedErr{DeviceID: device.ID}
			break
		}
		var recovered bool
		if reason == pollCompleted {
			state, _ := s.failedDevices.LoadAndDelete(device.ID)
			_, recovered = state.(time.Time)
		}
		if entry.index >= 0 {
			previousUpdate := entry.device.LastUpdatedAt
			if reason != pollCompleted || previousUpdate.IsZero() || !device.LastUpdatedAt.After(previousUpdate) {
				alreadyScheduled.add(device.ID)
				continue
			}
			entry.device = device
			if earliest := device.LastUpdatedAt.Add(s.minGap); entry.due.Before(earliest) {
				entry.due = earliest
			}
			heap.Fix(&s.queue, entry.index)
			continue
		}
		switch reason {
		case pollUnsent:
		case pollDeferred:
			entry.due = s.now().Add(s.retryInterval)
		case pollCompleted, pollBusy:
			entry.device = device
			earliest := device.LastUpdatedAt.Add(s.minGap)
			entry.due = entry.due.Add(s.pollPeriod)
			if recovered || entry.due.Before(earliest) {
				entry.due = earliest
			}
		}
		heap.Push(&s.queue, entry)
	}
	s.mu.Unlock()

	alreadyScheduled.log("scheduler skipped already scheduled devices")
	return err
}

type duplicateDeviceLog struct {
	count  int
	sample [5]models.DeviceIdentifier
}

func (d *duplicateDeviceLog) add(id models.DeviceIdentifier) {
	if d.count < len(d.sample) {
		d.sample[d.count] = id
	}
	d.count++
}

func (d *duplicateDeviceLog) log(message string) {
	if d.count == 0 {
		return
	}

	attrs := []any{"count", d.count}
	samples := make([]string, min(d.count, len(d.sample)))
	for i := range samples {
		samples[i] = d.sample[i].String()
	}
	attrs = append(attrs, "sample_device_ids", samples)

	if d.count == 1 {
		slog.Debug(message, attrs...)
		return
	}
	slog.Info(message, attrs...)
}

func (s *scheduler) AddFailedDevices(ctx context.Context, devices ...models.Device) error {
	retryAt := s.now().Add(s.retryInterval)
	for _, device := range devices {
		terminalFailures, err := s.recordFailure(device, retryAt)
		if err != nil {
			return err
		}
		if terminalFailures > 0 {
			slog.Warn("Device failed too many times, removing from scheduler", "device_id", device.ID, "failed_count", terminalFailures)
		}
	}
	return nil
}

func (s *scheduler) recordFailure(device models.Device, retryAt time.Time) (terminalFailures int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, exists := s.devices[device.ID]
	if !exists {
		return 0, DeviceNotManagedErr{DeviceID: device.ID}
	}
	state, exists := s.failedDevices.Load(device.ID)
	if _, isFailed := state.(time.Time); isFailed {
		return 0, nil
	}
	failedCount, ok := state.(int)
	if exists && !ok {
		return 0, DeviceNotManagedErr{DeviceID: device.ID}
	}

	failedCount++
	if failedCount >= s.config.MaxConsecutiveFailures {
		s.failedDevices.Store(device.ID, device.LastUpdatedAt)
		return failedCount, nil
	}
	s.failedDevices.Store(device.ID, failedCount)
	if entry.index < 0 {
		entry.device = device
		entry.due = retryAt
		heap.Push(&s.queue, entry)
	}
	return 0, nil
}

func (s *scheduler) RemoveDevices(ctx context.Context, deviceIDs ...models.DeviceIdentifier) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, id := range deviceIDs {
		entry, exists := s.devices[id]
		if !exists {
			return DeviceNotManagedErr{DeviceID: id}
		}
		if entry.index >= 0 {
			heap.Remove(&s.queue, entry.index)
		}
		delete(s.devices, id)
		s.failedDevices.Delete(id)
	}
	return nil
}

// FetchDue checks out up to limit devices in due-time order.
func (s *scheduler) FetchDue(ctx context.Context, now time.Time, limit int) ([]models.Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	due := []models.Device{}
	for len(due) < limit && len(s.queue) > 0 && !s.queue[0].due.After(now) {
		entry := s.queue[0]
		heap.Pop(&s.queue)
		due = append(due, entry.device)
	}
	return due, nil
}

func (s *scheduler) GetAllDevices(ctx context.Context) ([]models.Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make([]models.Device, 0, len(s.queue))
	for _, entry := range s.queue {
		result = append(result, entry.device)
	}
	return result, nil
}

func (s *scheduler) GetDeviceCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.queue)
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
