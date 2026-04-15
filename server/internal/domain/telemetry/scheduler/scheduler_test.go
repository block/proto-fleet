package scheduler

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
)

func TestNewScheduler(t *testing.T) {
	t.Run("creates a new scheduler instance", func(t *testing.T) {
		config := Config{
			MaxConsecutiveFailures: 10,
		}
		s := NewScheduler(config)
		assert.NotNil(t, s)

		// Verify initial state through public interface
		assert.Equal(t, 0, s.GetDeviceCount())
		devices, err := s.GetAllDevices(t.Context())
		require.NoError(t, err)
		assert.Empty(t, devices)
	})
}

func TestScheduler_AddNewDevices(t *testing.T) {
	t.Run("adds single new device successfully", func(t *testing.T) {
		config := Config{
			MaxConsecutiveFailures: 10,
		}
		s := NewScheduler(config)
		ctx := t.Context()
		deviceID := models.DeviceIdentifier("123")

		err := s.AddNewDevices(ctx, deviceID)

		require.NoError(t, err)
		assert.Equal(t, 1, s.GetDeviceCount())

		devices, err := s.GetAllDevices(ctx)
		require.NoError(t, err)
		require.Len(t, devices, 1)
		assert.Equal(t, deviceID, devices[0].ID)

		// Verify device was added with zero timestamp so it is immediately eligible for scheduling
		assert.True(t, devices[0].LastUpdatedAt.IsZero(), "new devices should start with zero timestamp to be immediately eligible for scheduling")

		// Act
		stale, err := s.FetchDevices(ctx, time.Now())

		// Assert
		require.NoError(t, err)
		assert.Len(t, stale, 1, "new device should be immediately returned by FetchDevices")
	})

	t.Run("adds multiple new devices successfully", func(t *testing.T) {
		config := Config{
			MaxConsecutiveFailures: 10,
		}
		s := NewScheduler(config)
		ctx := t.Context()
		deviceIDs := []models.DeviceIdentifier{"123", "456", "789"}

		err := s.AddNewDevices(ctx, deviceIDs...)

		require.NoError(t, err)
		assert.Equal(t, 3, s.GetDeviceCount())

		devices, err := s.GetAllDevices(ctx)
		require.NoError(t, err)
		require.Len(t, devices, 3)

		// Verify all devices were added
		for _, expectedID := range deviceIDs {
			found := slices.ContainsFunc(devices, func(d models.Device) bool {
				return d.ID == expectedID
			})
			assert.True(t, found, "Device %d should be in scheduler", expectedID)
		}
	})

	t.Run("skips already managed devices", func(t *testing.T) {
		config := Config{
			MaxConsecutiveFailures: 10,
		}
		s := NewScheduler(config)
		ctx := t.Context()
		deviceID := models.DeviceIdentifier("123")

		// Add device first time
		err := s.AddNewDevices(ctx, deviceID)
		require.NoError(t, err)
		assert.Equal(t, 1, s.GetDeviceCount())

		// Try to add same device again
		err = s.AddNewDevices(ctx, deviceID)
		require.NoError(t, err)
		assert.Equal(t, 1, s.GetDeviceCount()) // Should still be 1
	})
}

func TestScheduler_AddDevices(t *testing.T) {
	t.Run("adds managed device back to scheduler", func(t *testing.T) {
		config := Config{
			MaxConsecutiveFailures: 10,
		}
		s := NewScheduler(config)
		ctx := t.Context()
		deviceID := models.DeviceIdentifier("123")

		// First add as new device
		err := s.AddNewDevices(ctx, deviceID)
		require.NoError(t, err)

		// Fetch the device to simulate it being checked out
		oldTime := time.Now().Add(-2 * time.Hour)
		oldDevices, err := s.FetchDevices(ctx, time.Now().Add(1*time.Hour))
		require.NoError(t, err)
		require.Len(t, oldDevices, 1)
		require.Equal(t, 0, s.GetDeviceCount())

		// Create device to add back
		device := models.Device{
			ID:            deviceID,
			LastUpdatedAt: oldTime,
		}

		err = s.AddDevices(ctx, device)
		require.NoError(t, err)

		// Verify device is back in scheduler
		devices, err := s.GetAllDevices(ctx)
		require.NoError(t, err)
		require.Len(t, devices, 1)
		found := slices.ContainsFunc(devices, func(d models.Device) bool {
			return d.ID == deviceID
		})
		assert.True(t, found)
	})

	t.Run("returns error for unmanaged device", func(t *testing.T) {
		config := Config{
			MaxConsecutiveFailures: 10,
		}
		s := NewScheduler(config)
		ctx := t.Context()
		device := models.Device{
			ID:            models.DeviceIdentifier("999"),
			LastUpdatedAt: time.Now(),
		}

		err := s.AddDevices(ctx, device)

		require.Error(t, err)
		var deviceNotManagedErr DeviceNotManagedErr
		require.ErrorAs(t, err, &deviceNotManagedErr)
		assert.Equal(t, device.ID, deviceNotManagedErr.DeviceID)
	})

	t.Run("handles already scheduled device", func(t *testing.T) {
		config := Config{
			MaxConsecutiveFailures: 10,
		}
		s := NewScheduler(config)
		ctx := t.Context()
		deviceID := models.DeviceIdentifier("123")

		// Add as new device (will be in queue)
		err := s.AddNewDevices(ctx, deviceID)
		require.NoError(t, err)
		assert.Equal(t, 1, s.GetDeviceCount())

		device := models.Device{
			ID:            deviceID,
			LastUpdatedAt: time.Now(),
		}

		err = s.AddDevices(ctx, device)
		require.NoError(t, err) // Should not return error, just log warning
		assert.Equal(t, 1, s.GetDeviceCount())
	})
}

func TestScheduler_RemoveDevices(t *testing.T) {
	t.Run("removes single managed device", func(t *testing.T) {
		config := Config{
			MaxConsecutiveFailures: 10,
		}
		s := NewScheduler(config)
		ctx := t.Context()
		deviceID := models.DeviceIdentifier("123")

		// Add device first
		err := s.AddNewDevices(ctx, deviceID)
		require.NoError(t, err)
		assert.Equal(t, 1, s.GetDeviceCount())

		// Remove device
		err = s.RemoveDevices(ctx, deviceID)

		require.NoError(t, err)
		assert.Equal(t, 0, s.GetDeviceCount())

		devices, err := s.GetAllDevices(ctx)
		require.NoError(t, err)
		assert.Empty(t, devices)
	})

	t.Run("removes multiple managed devices", func(t *testing.T) {
		config := Config{
			MaxConsecutiveFailures: 10,
		}
		s := NewScheduler(config)
		ctx := t.Context()
		deviceIDs := []models.DeviceIdentifier{"123", "456", "789", "101112", "131415"}

		// Add devices first
		err := s.AddNewDevices(ctx, deviceIDs...)
		require.NoError(t, err)
		assert.Equal(t, len(deviceIDs), s.GetDeviceCount())

		// Remove devices
		err = s.RemoveDevices(ctx, deviceIDs[:3]...)

		require.NoError(t, err)
		assert.Equal(t, len(deviceIDs)-3, s.GetDeviceCount())
	})

	t.Run("returns error for unmanaged device", func(t *testing.T) {
		config := Config{
			MaxConsecutiveFailures: 10,
		}
		s := NewScheduler(config)
		ctx := t.Context()
		deviceID := models.DeviceIdentifier("999")

		err := s.RemoveDevices(ctx, deviceID)

		require.Error(t, err)
		var deviceNotManagedErr DeviceNotManagedErr
		require.ErrorAs(t, err, &deviceNotManagedErr)
		assert.Equal(t, deviceID, deviceNotManagedErr.DeviceID)
	})

	t.Run("partial removal when some devices are unmanaged", func(t *testing.T) {
		config := Config{
			MaxConsecutiveFailures: 10,
		}
		s := NewScheduler(config)
		ctx := t.Context()
		managedID := models.DeviceIdentifier("123")
		unmanagedID := models.DeviceIdentifier("999")

		// Add only one device
		err := s.AddNewDevices(ctx, managedID)
		require.NoError(t, err)
		assert.Equal(t, 1, s.GetDeviceCount())

		// Try to remove both managed and unmanaged device
		err = s.RemoveDevices(ctx, managedID, unmanagedID)

		// Should return error for the unmanaged device
		require.Error(t, err)
		var deviceNotManagedErr DeviceNotManagedErr
		require.ErrorAs(t, err, &deviceNotManagedErr)
		assert.Equal(t, unmanagedID, deviceNotManagedErr.DeviceID)
	})
}

func TestScheduler_FetchDevices(t *testing.T) {
	t.Run("fetches devices older than threshold", func(t *testing.T) {
		config := Config{
			MaxConsecutiveFailures: 10,
		}
		s := NewScheduler(config)
		ctx := t.Context()
		now := time.Now()

		// Add devices with different timestamps by adding them as new devices first
		deviceIDs := []models.DeviceIdentifier{"1", "2", "3", "4"}
		for _, id := range deviceIDs {
			err := s.AddNewDevices(ctx, id)
			require.NoError(t, err)
		}

		// Fetch all devices to clear scheduler
		_, err := s.FetchDevices(ctx, time.Now().Add(time.Hour))
		require.NoError(t, err)

		// Add them back with specific timestamps
		oldDevices := []models.Device{
			{ID: "1", LastUpdatedAt: now.Add(-3 * time.Hour)},
			{ID: "2", LastUpdatedAt: now.Add(-2 * time.Hour)},
		}
		newDevices := []models.Device{
			{ID: "3", LastUpdatedAt: now.Add(-30 * time.Minute)},
			{ID: "4", LastUpdatedAt: now},
		}

		// Add all devices back
		allDevicesWithTimes := slices.Concat(oldDevices, newDevices)
		for _, device := range allDevicesWithTimes {
			err := s.AddDevices(ctx, device)
			require.NoError(t, err)
		}

		// Fetch devices older than 1 hour
		threshold := now.Add(-1 * time.Hour)
		fetchedDevices, err := s.FetchDevices(ctx, threshold)

		require.NoError(t, err)
		assert.Len(t, fetchedDevices, 2)

		// Verify correct devices were fetched (should be the old ones)
		fetchedIDs := make([]models.DeviceIdentifier, len(fetchedDevices))
		for i, device := range fetchedDevices {
			fetchedIDs[i] = device.ID
		}
		assert.Contains(t, fetchedIDs, models.DeviceIdentifier("1"))
		assert.Contains(t, fetchedIDs, models.DeviceIdentifier("2"))

		// Check that remaining devices are still in scheduler
		remainingDevices, err := s.GetAllDevices(ctx)
		require.NoError(t, err)
		assert.Len(t, remainingDevices, 2)

		remainingIDs := make([]models.DeviceIdentifier, len(remainingDevices))
		for i, device := range remainingDevices {
			remainingIDs[i] = device.ID
		}
		assert.Contains(t, remainingIDs, models.DeviceIdentifier("3"))
		assert.Contains(t, remainingIDs, models.DeviceIdentifier("4"))
	})

	t.Run("returns empty slice when no old devices", func(t *testing.T) {
		// Arrange: register devices, check them out, then re-add with time.Now() to simulate a fresh poll
		config := Config{
			MaxConsecutiveFailures: 10,
		}
		s := NewScheduler(config)
		ctx := t.Context()
		deviceIDs := []models.DeviceIdentifier{"1", "2"}
		for _, id := range deviceIDs {
			err := s.AddNewDevices(ctx, id)
			require.NoError(t, err)
		}
		// Check out all devices (simulating the scheduler picking them up)
		_, err := s.FetchDevices(ctx, time.Now())
		require.NoError(t, err)
		// Re-add with time.Now() to simulate a just-completed poll
		for _, id := range deviceIDs {
			err := s.AddDevices(ctx, models.Device{ID: id, LastUpdatedAt: time.Now()})
			require.NoError(t, err)
		}

		// Act: fetch with a threshold 1 hour ago — freshly polled devices should not be stale
		fetchedDevices, err := s.FetchDevices(ctx, time.Now().Add(-1*time.Hour))

		// Assert
		require.NoError(t, err)
		assert.Empty(t, fetchedDevices)
		assert.Equal(t, 2, s.GetDeviceCount()) // All devices should remain
	})

	t.Run("returns empty slice when scheduler is empty", func(t *testing.T) {
		config := Config{
			MaxConsecutiveFailures: 10,
		}
		s := NewScheduler(config)
		ctx := t.Context()
		threshold := time.Now().Add(-1 * time.Hour)

		fetchedDevices, err := s.FetchDevices(ctx, threshold)

		require.NoError(t, err)
		assert.Empty(t, fetchedDevices)
		assert.Equal(t, 0, s.GetDeviceCount())
	})

	t.Run("fetches all devices when all are old", func(t *testing.T) {
		config := Config{
			MaxConsecutiveFailures: 10,
		}
		s := NewScheduler(config)
		ctx := t.Context()
		now := time.Now()

		// Add devices as new first
		deviceIDs := []models.DeviceIdentifier{"1", "2"}
		for _, id := range deviceIDs {
			err := s.AddNewDevices(ctx, id)
			require.NoError(t, err)
		}

		// Fetch all to clear, then add back with old timestamps
		_, err := s.FetchDevices(ctx, time.Now().Add(time.Hour))
		require.NoError(t, err)

		oldDevices := []models.Device{
			{ID: "1", LastUpdatedAt: now.Add(-3 * time.Hour)},
			{ID: "2", LastUpdatedAt: now.Add(-2 * time.Hour)},
		}

		for _, device := range oldDevices {
			err := s.AddDevices(ctx, device)
			require.NoError(t, err)
		}

		// Fetch devices older than 1 hour (all should be fetched)
		threshold := now.Add(-1 * time.Hour)
		fetchedDevices, err := s.FetchDevices(ctx, threshold)

		require.NoError(t, err)
		assert.Len(t, fetchedDevices, 2)
		assert.Equal(t, 0, s.GetDeviceCount()) // All devices should be removed from scheduler
	})

	t.Run("fetched devices can be added back", func(t *testing.T) {
		config := Config{
			MaxConsecutiveFailures: 10,
		}
		s := NewScheduler(config)
		ctx := t.Context()
		deviceID := models.DeviceIdentifier("123")

		// Add device
		err := s.AddNewDevices(ctx, deviceID)
		require.NoError(t, err)

		// Fetch it (this should mark it as checked out)
		fetchedDevices, err := s.FetchDevices(ctx, time.Now().Add(time.Hour))
		require.NoError(t, err)
		require.Len(t, fetchedDevices, 1)
		assert.Equal(t, 0, s.GetDeviceCount())

		// Add it back
		err = s.AddDevices(ctx, fetchedDevices[0])
		require.NoError(t, err)
		assert.Equal(t, 1, s.GetDeviceCount())
	})
}

func TestScheduler_AddFailedDevices(t *testing.T) {
	t.Run("adds failed devices back to scheduler", func(t *testing.T) {
		config := Config{
			MaxConsecutiveFailures: 10,
		}

		s := NewScheduler(config)
		ctx := t.Context()
		deviceID := models.DeviceIdentifier("123")

		// Add device as new first
		err := s.AddNewDevices(ctx, deviceID)
		require.NoError(t, err)

		// Simulate fetching and failing
		fetchedDevices, err := s.FetchDevices(ctx, time.Now().Add(time.Hour))
		require.NoError(t, err)
		require.Len(t, fetchedDevices, 1)
		assert.Equal(t, 0, s.GetDeviceCount())

		// Add it back with failure
		failedDevice := fetchedDevices[0]
		err = s.AddFailedDevices(ctx, failedDevice)
		require.NoError(t, err)

		assert.Equal(t, 1, s.GetDeviceCount())
		devices, err := s.GetAllDevices(ctx)
		require.NoError(t, err)
		assert.Len(t, devices, 1)
		assert.Equal(t, deviceID, devices[0].ID)

		// Verify device is not marked as failed yet
		isFailed, _, err := s.IsFailedDevice(ctx, deviceID)
		require.NoError(t, err)
		assert.False(t, isFailed, "Device should not be marked as failed after first failure")
	})

	t.Run("device marked as failed after max consecutive failures", func(t *testing.T) {
		config := Config{
			MaxConsecutiveFailures: 3,
		}
		s := NewScheduler(config)
		ctx := t.Context()
		deviceID := models.DeviceIdentifier("123")

		// Add device as new first
		err := s.AddNewDevices(ctx, deviceID)
		require.NoError(t, err)

		// Simulate failing the device exactly MaxConsecutiveFailures times
		for i := range config.MaxConsecutiveFailures {
			// Fetch device
			fetchedDevices, err := s.FetchDevices(ctx, time.Now().Add(time.Hour))
			require.NoError(t, err)
			require.Len(t, fetchedDevices, 1)

			// Add back as failed
			failedDevice := fetchedDevices[0]
			failedDevice.LastUpdatedAt = time.Now()
			err = s.AddFailedDevices(ctx, failedDevice)
			require.NoError(t, err)

			if i < config.MaxConsecutiveFailures-1 {
				// Should still be in scheduler
				assert.Equal(t, 1, s.GetDeviceCount(), "Device should still be in scheduler after failure %d", i+1)

				// Should not be marked as failed yet
				isFailed, _, err := s.IsFailedDevice(ctx, deviceID)
				require.NoError(t, err)
				assert.False(t, isFailed, "Device should not be marked as failed after failure %d", i+1)
			}
		}

		// After max failures, device should not be in scheduler
		assert.Equal(t, 0, s.GetDeviceCount(), "Device should be removed from scheduler after max failures")

		// Device should be marked as failed
		isFailed, failedAt, err := s.IsFailedDevice(ctx, deviceID)
		require.NoError(t, err)
		assert.True(t, isFailed, "Device should be marked as failed after max consecutive failures")
		assert.False(t, failedAt.IsZero(), "Failed timestamp should be set")

		// Trying to add the failed device again should not add it back to scheduler
		failedDevice := models.Device{
			ID:            deviceID,
			LastUpdatedAt: time.Now(),
		}
		err = s.AddFailedDevices(ctx, failedDevice)
		require.NoError(t, err)
		assert.Equal(t, 0, s.GetDeviceCount(), "Failed device should not be added back to scheduler")
	})

	t.Run("failed device can be recovered with AddDevices", func(t *testing.T) {
		config := Config{
			MaxConsecutiveFailures: 2,
		}
		s := NewScheduler(config)
		ctx := t.Context()
		deviceID := models.DeviceIdentifier("123")

		// Add device and fail it until it's marked as failed
		err := s.AddNewDevices(ctx, deviceID)
		require.NoError(t, err)

		// Fail device max times
		for range config.MaxConsecutiveFailures {
			fetchedDevices, err := s.FetchDevices(ctx, time.Now().Add(time.Hour))
			require.NoError(t, err)
			require.Len(t, fetchedDevices, 1)

			failedDevice := fetchedDevices[0]
			failedDevice.LastUpdatedAt = time.Now()
			err = s.AddFailedDevices(ctx, failedDevice)
			require.NoError(t, err)
		}

		// Verify device is failed and not in scheduler
		isFailed, _, err := s.IsFailedDevice(ctx, deviceID)
		require.NoError(t, err)
		assert.True(t, isFailed)
		assert.Equal(t, 0, s.GetDeviceCount())

		// Use AddDevices to recover the device (simulating successful operation)
		recoveredDevice := models.Device{
			ID:            deviceID,
			LastUpdatedAt: time.Now(),
		}
		err = s.AddDevices(ctx, recoveredDevice)
		require.NoError(t, err)

		// Device should be back in scheduler and no longer marked as failed
		assert.Equal(t, 1, s.GetDeviceCount())
		isFailed, _, err = s.IsFailedDevice(ctx, deviceID)
		require.NoError(t, err)
		assert.False(t, isFailed, "Device should no longer be marked as failed after successful AddDevices")
	})

	t.Run("add failed devices to scheduler", func(t *testing.T) {
		type PassFailExpect struct {
			Pass          bool
			ExpectFetched bool
		}
		type scenario struct {
			PassFailQueue []PassFailExpect
		}
		tests := []struct {
			name                   string
			deviceScenario         map[models.DeviceIdentifier]scenario
			MaxConsecutiveFailures int
		}{
			{
				name:                   "single device with multiple failures",
				MaxConsecutiveFailures: 10,
				deviceScenario: map[models.DeviceIdentifier]scenario{
					"device1": {
						PassFailQueue: []PassFailExpect{
							{Pass: false, ExpectFetched: true},
							{Pass: false, ExpectFetched: true},
							{Pass: false, ExpectFetched: true},
						},
					},
				},
			},
			{
				name:                   "single device with max failures",
				MaxConsecutiveFailures: 3,
				deviceScenario: map[models.DeviceIdentifier]scenario{
					"device1": {
						PassFailQueue: []PassFailExpect{
							{Pass: false, ExpectFetched: true},
							{Pass: false, ExpectFetched: true},
							{Pass: false, ExpectFetched: true},
							{Pass: false, ExpectFetched: false}, // Should not be added back after max failures
							{Pass: false, ExpectFetched: false}, // Should not be added back after max failures
						},
					},
				},
			},
			{
				name:                   "multiple devices with mixed failures",
				MaxConsecutiveFailures: 3,
				deviceScenario: map[models.DeviceIdentifier]scenario{
					"device1": {
						PassFailQueue: []PassFailExpect{
							{Pass: false, ExpectFetched: true},
							{Pass: false, ExpectFetched: true},
							{Pass: false, ExpectFetched: true},
							{Pass: false, ExpectFetched: false}, // Should not be added back after max failures
							{Pass: false, ExpectFetched: false}, // Should not be added back after max failures
						},
					},
					"device2": {
						PassFailQueue: []PassFailExpect{
							{Pass: false, ExpectFetched: true},
							{Pass: false, ExpectFetched: true},
							{Pass: true, ExpectFetched: true},
							{Pass: false, ExpectFetched: true},
							{Pass: false, ExpectFetched: true},
							{Pass: false, ExpectFetched: true},
						},
					},
					"device3": {
						PassFailQueue: []PassFailExpect{
							{Pass: true, ExpectFetched: true},
							{Pass: false, ExpectFetched: true},
							{Pass: false, ExpectFetched: true},
							{Pass: false, ExpectFetched: true},
						},
					},
				},
			},
			{
				name:                   "single device fails till max limit and then succeeds should be in queue",
				MaxConsecutiveFailures: 3,
				deviceScenario: map[models.DeviceIdentifier]scenario{
					"device1": {
						PassFailQueue: []PassFailExpect{
							{Pass: false, ExpectFetched: true},
							{Pass: false, ExpectFetched: true},
							{Pass: true, ExpectFetched: true}, // Should be added back after max failures
							{Pass: true, ExpectFetched: true},
							{Pass: false, ExpectFetched: true},
							{Pass: false, ExpectFetched: true},
							{Pass: false, ExpectFetched: true},
							{Pass: false, ExpectFetched: false},
						},
					},
				},
			},
			{
				name:                   "single device fails multiple times, then passes, should be added back then fail until removed",
				MaxConsecutiveFailures: 3,
				deviceScenario: map[models.DeviceIdentifier]scenario{
					"device1": {
						PassFailQueue: []PassFailExpect{
							{Pass: false, ExpectFetched: true},
							{Pass: false, ExpectFetched: true},
							{Pass: true, ExpectFetched: true}, // Should be added back after max failures
							{Pass: true, ExpectFetched: true},
							{Pass: false, ExpectFetched: true},
							{Pass: false, ExpectFetched: true},
							{Pass: false, ExpectFetched: true},
							{Pass: false, ExpectFetched: false}, // Should not be added back after max failures
						},
					},
				},
			},
			{
				name:                   "failed till removed, than added back in after max failures should be added back in",
				MaxConsecutiveFailures: 3,
				deviceScenario: map[models.DeviceIdentifier]scenario{
					"device1": {
						PassFailQueue: []PassFailExpect{
							{Pass: false, ExpectFetched: true},
							{Pass: false, ExpectFetched: true},
							{Pass: false, ExpectFetched: true},
							{Pass: false, ExpectFetched: false},
							{Pass: false, ExpectFetched: false},
							{Pass: true, ExpectFetched: false},
							{Pass: true, ExpectFetched: true},
							{Pass: true, ExpectFetched: true},
						},
					},
				},
			},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				config := Config{
					MaxConsecutiveFailures: test.MaxConsecutiveFailures,
				}
				s := NewScheduler(config)
				ctx := t.Context()

				maxFetchCount := 0

				for deviceID := range test.deviceScenario {
					// Add device as new first
					err := s.AddNewDevices(ctx, deviceID)
					require.NoError(t, err, "Failed to add new device %s", deviceID)
					if maxFetchCount < len(test.deviceScenario[deviceID].PassFailQueue) {
						maxFetchCount = len(test.deviceScenario[deviceID].PassFailQueue)
					}
				}

				for i := range maxFetchCount {
					// Fetch devices to simulate work being done
					fetchedDevices, err := s.FetchDevices(ctx, time.Now().Add(time.Hour))
					require.NoError(t, err, "Failed to fetch devices on iteration %d", i)

					for deviceID, scenario := range test.deviceScenario {
						if i >= len(scenario.PassFailQueue) {
							continue // No more pass/fail for this device
						}

						pf := scenario.PassFailQueue[i]
						assert.Equal(t, pf.ExpectFetched, slices.ContainsFunc(
							fetchedDevices,
							func(a models.Device) bool {
								return a.ID == deviceID
							},
						))

						if pf.Pass {
							// Simulate adding back a device that passed
							err = s.AddDevices(ctx, models.Device{
								ID:            deviceID,
								LastUpdatedAt: time.Now(),
							})
						} else {
							// Simulate adding failed devices
							err = s.AddFailedDevices(ctx, models.Device{
								ID:            deviceID,
								LastUpdatedAt: time.Now(),
							})
						}
						require.NoError(t, err, "Failed to add device %s on iteration %d", deviceID, i)
					}
				}
			})
		}
	})

	t.Run("IsFailedDevice method tests", func(t *testing.T) {
		config := Config{
			MaxConsecutiveFailures: 2,
		}
		s := NewScheduler(config)
		ctx := t.Context()
		deviceID := models.DeviceIdentifier("test-device")

		// Test non-existent device
		isFailed, failedAt, err := s.IsFailedDevice(ctx, deviceID)
		require.NoError(t, err)
		assert.False(t, isFailed, "Non-existent device should not be marked as failed")
		assert.True(t, failedAt.IsZero(), "Non-existent device should have zero timestamp")

		// Add device and test before any failures
		err = s.AddNewDevices(ctx, deviceID)
		require.NoError(t, err)

		isFailed, failedAt, err = s.IsFailedDevice(ctx, deviceID)
		require.NoError(t, err)
		assert.False(t, isFailed, "New device should not be marked as failed")
		assert.True(t, failedAt.IsZero(), "New device should have zero timestamp")

		// Fail device once (should not be marked as failed yet)
		fetchedDevices, err := s.FetchDevices(ctx, time.Now().Add(time.Hour))
		require.NoError(t, err)
		require.Len(t, fetchedDevices, 1)

		failedDevice := fetchedDevices[0]
		failedDevice.LastUpdatedAt = time.Now()
		err = s.AddFailedDevices(ctx, failedDevice)
		require.NoError(t, err)

		isFailed, failedAt, err = s.IsFailedDevice(ctx, deviceID)
		require.NoError(t, err)
		assert.False(t, isFailed, "Device should not be marked as failed after first failure")
		assert.True(t, failedAt.IsZero(), "Device should have zero timestamp after first failure")

		// Fail device second time (should be marked as failed)
		fetchedDevices, err = s.FetchDevices(ctx, time.Now().Add(time.Hour))
		require.NoError(t, err)
		require.Len(t, fetchedDevices, 1)

		failedDevice = fetchedDevices[0]
		beforeFailTime := time.Now()
		failedDevice.LastUpdatedAt = beforeFailTime
		err = s.AddFailedDevices(ctx, failedDevice)
		require.NoError(t, err)

		isFailed, failedAt, err = s.IsFailedDevice(ctx, deviceID)
		require.NoError(t, err)
		assert.True(t, isFailed, "Device should be marked as failed after max consecutive failures")
		assert.False(t, failedAt.IsZero(), "Failed device should have non-zero timestamp")
		assert.Equal(t, beforeFailTime, failedAt, "Failed timestamp should match device's LastUpdatedAt")
	})
}

func TestScheduler_ConcurrentAccess(t *testing.T) {
	t.Run("concurrent add and remove operations", func(t *testing.T) {
		config := Config{
			MaxConsecutiveFailures: 10,
		}
		s := NewScheduler(config)
		ctx := t.Context()

		done := make(chan bool, 2)

		// Goroutine 1: Add devices
		go func() {
			defer func() { done <- true }()
			for i := range 10 {
				deviceID := models.DeviceIdentifier(fmt.Sprint(i))
				err := s.AddNewDevices(ctx, deviceID)
				assert.NoError(t, err, "Failed to add device %d", i)
			}
		}()

		// Goroutine 2: Remove devices (may fail for unmanaged devices)
		go func() {
			defer func() { done <- true }()
			for i := range 10 {
				deviceID := models.DeviceIdentifier(fmt.Sprint(i))
				//nolint:errcheck // Intentionally ignore errors for unmanaged devices
				s.RemoveDevices(ctx, deviceID) // This may error, which is expected
			}
		}()

		// Wait for both goroutines
		<-done
		<-done

		// Test passes if no race conditions occurred
		// We can verify the scheduler is in a consistent state
		count := s.GetDeviceCount()
		devices, err := s.GetAllDevices(ctx)
		require.NoError(t, err)
		assert.Len(t, devices, count)
	})

	t.Run("concurrent fetch operations", func(t *testing.T) {
		config := Config{
			MaxConsecutiveFailures: 10,
		}
		s := NewScheduler(config)
		ctx := t.Context()

		// Add some devices first
		for i := range 5 {
			err := s.AddNewDevices(ctx, models.DeviceIdentifier(fmt.Sprint(i)))
			require.NoError(t, err)
		}

		done := make(chan bool, 2)

		// Two goroutines trying to fetch devices concurrently
		go func() {
			defer func() { done <- true }()
			_, err := s.FetchDevices(ctx, time.Now().Add(time.Hour))
			assert.NoError(t, err, "Failed to fetch devices in goroutine 1")
		}()

		go func() {
			defer func() { done <- true }()
			_, err := s.FetchDevices(ctx, time.Now().Add(time.Hour))
			assert.NoError(t, err, "Failed to fetch devices in goroutine 2")
		}()

		<-done
		<-done

		// Verify scheduler is in consistent state
		count := s.GetDeviceCount()
		devices, err := s.GetAllDevices(ctx)
		require.NoError(t, err)
		assert.Len(t, devices, count)
	})
}

func TestScheduler_EdgeCases(t *testing.T) {
	t.Run("empty device ID slice", func(t *testing.T) {
		config := Config{
			MaxConsecutiveFailures: 10,
		}
		s := NewScheduler(config)
		ctx := t.Context()

		err := s.AddNewDevices(ctx)
		require.NoError(t, err)
		assert.Equal(t, 0, s.GetDeviceCount())

		err = s.RemoveDevices(ctx)
		require.NoError(t, err)
		assert.Equal(t, 0, s.GetDeviceCount())
	})

	t.Run("empty device slice", func(t *testing.T) {
		config := Config{
			MaxConsecutiveFailures: 10,
		}
		s := NewScheduler(config)
		ctx := t.Context()

		err := s.AddDevices(ctx)
		require.NoError(t, err)
		assert.Equal(t, 0, s.GetDeviceCount())
	})

	t.Run("fetch with exact timestamp boundary", func(t *testing.T) {
		config := Config{
			MaxConsecutiveFailures: 10,
		}
		s := NewScheduler(config)
		ctx := t.Context()
		exactTime := time.Now()

		// Add device as new first
		deviceID := models.DeviceIdentifier("1")
		err := s.AddNewDevices(ctx, deviceID)
		require.NoError(t, err)

		// Fetch all and add back with exact timestamp
		_, err = s.FetchDevices(ctx, time.Now().Add(time.Hour))
		require.NoError(t, err)

		device := models.Device{
			ID:            deviceID,
			LastUpdatedAt: exactTime,
		}
		err = s.AddDevices(ctx, device)
		require.NoError(t, err)

		// Fetch with exact same timestamp
		fetchedDevices, err := s.FetchDevices(ctx, exactTime)

		require.NoError(t, err)
		// The behavior here depends on the binary search implementation
		assert.Empty(t, fetchedDevices, "Should not fetch device with exact timestamp")
	})

	t.Run("large number of devices", func(t *testing.T) {
		config := Config{
			MaxConsecutiveFailures: 10,
		}
		s := NewScheduler(config)
		ctx := t.Context()

		// Add many devices
		deviceCount := 1000
		deviceIDs := make([]models.DeviceIdentifier, deviceCount)
		for i := range deviceCount {
			deviceIDs[i] = models.DeviceIdentifier(fmt.Sprint(i))
		}

		err := s.AddNewDevices(ctx, deviceIDs...)
		require.NoError(t, err)
		assert.Equal(t, deviceCount, s.GetDeviceCount())

		// Verify all devices are present
		devices, err := s.GetAllDevices(ctx)
		require.NoError(t, err)
		assert.Len(t, devices, deviceCount)
	})
}

func TestScheduler_IntegrationScenarios(t *testing.T) {
	t.Run("complete device lifecycle", func(t *testing.T) {
		config := Config{
			MaxConsecutiveFailures: 10,
		}
		s := NewScheduler(config)
		ctx := t.Context()
		deviceID := models.DeviceIdentifier("123")

		// 1. Add new device
		err := s.AddNewDevices(ctx, deviceID)
		require.NoError(t, err)
		assert.Equal(t, 1, s.GetDeviceCount())

		// 2. Fetch device (simulating work being done)
		fetchedDevices, err := s.FetchDevices(ctx, time.Now().Add(time.Hour))
		require.NoError(t, err)
		require.Len(t, fetchedDevices, 1)
		assert.Equal(t, 0, s.GetDeviceCount())

		// 3. Add device back after work is done
		updatedDevice := fetchedDevices[0]
		updatedDevice.LastUpdatedAt = time.Now()
		err = s.AddDevices(ctx, updatedDevice)
		require.NoError(t, err)
		assert.Equal(t, 1, s.GetDeviceCount())

		// 4. Remove device from management
		err = s.RemoveDevices(ctx, deviceID)
		require.NoError(t, err)
		assert.Equal(t, 0, s.GetDeviceCount())

		// 5. Verify device is no longer managed
		err = s.AddDevices(ctx, updatedDevice)
		require.Error(t, err)
		var deviceNotManagedErr DeviceNotManagedErr
		require.ErrorAs(t, err, &deviceNotManagedErr)
	})

	t.Run("multiple devices with different timestamps", func(t *testing.T) {
		config := Config{
			MaxConsecutiveFailures: 10,
		}
		s := NewScheduler(config)
		ctx := t.Context()
		now := time.Now()

		// Add devices as new first
		deviceIDs := []models.DeviceIdentifier{"1", "2", "3", "4", "5"}
		for _, id := range deviceIDs {
			err := s.AddNewDevices(ctx, id)
			require.NoError(t, err)
		}

		// Fetch all and add back with different timestamps
		_, err := s.FetchDevices(ctx, time.Now().Add(time.Hour))
		require.NoError(t, err)

		devices := []models.Device{
			{ID: "1", LastUpdatedAt: now.Add(-5 * time.Hour)}, // oldest
			{ID: "2", LastUpdatedAt: now.Add(-3 * time.Hour)},
			{ID: "3", LastUpdatedAt: now.Add(-1 * time.Hour)},
			{ID: "4", LastUpdatedAt: now.Add(-30 * time.Minute)},
			{ID: "5", LastUpdatedAt: now}, // newest
		}

		for _, device := range devices {
			err := s.AddDevices(ctx, device)
			require.NoError(t, err)
		}

		// Fetch devices older than 2 hours
		threshold := now.Add(-2 * time.Hour)
		fetchedDevices, err := s.FetchDevices(ctx, threshold)
		require.NoError(t, err)

		// Should fetch devices 1 and 2
		assert.Len(t, fetchedDevices, 2)
		fetchedIDs := []models.DeviceIdentifier{fetchedDevices[0].ID, fetchedDevices[1].ID}
		assert.Contains(t, fetchedIDs, models.DeviceIdentifier("1"))
		assert.Contains(t, fetchedIDs, models.DeviceIdentifier("2"))

		// Remaining devices should be 3, 4, 5
		remainingDevices, err := s.GetAllDevices(ctx)
		require.NoError(t, err)
		assert.Len(t, remainingDevices, 3)
	})
}
