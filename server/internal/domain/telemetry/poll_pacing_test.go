package telemetry

import (
	"context"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	"github.com/block/proto-fleet/server/internal/domain/telemetry/scheduler"
)

func TestGatherMetricsRoutineSpreadsRecurringPolls(t *testing.T) {
	for _, tc := range []struct {
		name            string
		deviceCount     int
		workerCount     int
		minTicks        int
		maxPollsPerTick int
	}{
		{name: "large_fleet", deviceCount: 50_000, workerCount: 1500, minTicks: 75, maxPollsPerTick: 1000},
		{name: "two_workers", deviceCount: 200, workerCount: 2, minTicks: 40, maxPollsPerTick: 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				const (
					pollPeriod    = 20 * time.Second
					fetchInterval = 5 * time.Second
					cycles        = 3
				)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()

				queue := scheduler.NewScheduler(scheduler.Config{}, pollPeriod, fetchInterval)
				ids := make([]models.DeviceIdentifier, tc.deviceCount)
				for i := range ids {
					ids[i] = models.DeviceIdentifier(fmt.Sprintf("miner-%05d", i))
				}
				require.NoError(t, queue.AddNewDevices(ctx, ids...))

				service := NewTelemetryService(Config{FetchInterval: fetchInterval, ConcurrencyLimit: tc.workerCount},
					nil, nil, queue, nil, nil)
				tasks := make(chan models.Device)
				go service.gatherMetricsRoutine(ctx, tasks)
				deadline := time.After(cycles*pollPeriod + time.Second)

				for cycle := range cycles {
					perTick := make(map[time.Time]int)
					for range tc.deviceCount {
						var device models.Device
						select {
						case device = <-tasks:
						case <-deadline:
							t.Fatal("gather did not dispatch all polls")
						}
						now := time.Now()
						if cycle > 0 && now.Sub(device.LastUpdatedAt) != pollPeriod {
							t.Fatalf("device %s had a poll gap of %s", device.ID, now.Sub(device.LastUpdatedAt))
						}
						perTick[now]++
						device.LastUpdatedAt = now
						require.NoError(t, queue.AddDevices(ctx, device))
					}
					require.GreaterOrEqual(t, len(perTick), tc.minTicks, "polls must use enough ticks to spread across the period")
					peak := 0
					for _, count := range perTick {
						peak = max(peak, count)
					}
					t.Logf("cycle %d used %d ticks, with at most %d polls per tick", cycle+1, len(perTick), peak)
					require.Less(t, peak, tc.maxPollsPerTick, "healthy dispatch must stay spread across ticks")
				}
			})
		})
	}
}

func TestGatherMetricsRoutineBoundsAndRestoresOverdueCheckout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const (
			pollPeriod    = 20 * time.Second
			fetchInterval = 5 * time.Second
			extraDevices  = 5
		)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		queue := scheduler.NewScheduler(scheduler.Config{}, pollPeriod, fetchInterval)
		ids := make([]models.DeviceIdentifier, maxPollCheckoutSize+extraDevices)
		for i := range ids {
			ids[i] = models.DeviceIdentifier(fmt.Sprintf("miner-%05d", i))
		}
		require.NoError(t, queue.AddNewDevices(ctx, ids...))
		time.Sleep(pollPeriod)

		service := &TelemetryService{config: Config{FetchInterval: fetchInterval}, updateScheduler: queue}
		go service.gatherMetricsRoutine(ctx, make(chan models.Device))

		time.Sleep(pollDispatchInterval)
		synctest.Wait()
		require.Equal(t, extraDevices, queue.GetDeviceCount(), "overdue devices outside the current batch must stay queued")
		cancel()
		synctest.Wait()
		require.Equal(t, len(ids), queue.GetDeviceCount(), "cancellation must restore the unsent batch")
	})
}
