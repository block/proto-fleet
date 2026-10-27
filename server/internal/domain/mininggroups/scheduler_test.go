package mininggroups

import (
	"context"
	"database/sql"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

// Exercise the real ready-work query. Only the reconciliation callback is
// controlled, so tests can hold tasks open without issuing controller writes.
func newSchedulerFixture(t *testing.T, count int) (*Service, []int64, *sql.DB) {
	t.Helper()
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	ids := make([]int64, 0, count)
	for range count {
		device := database.CreateDevice(user.OrganizationID, "proto")
		_, err := database.DB.ExecContext(t.Context(), `INSERT INTO container_mining_group_state(device_id,organization_id,status) VALUES($1,$2,'pending')`, device.DatabaseID, user.OrganizationID)
		require.NoError(t, err)
		ids = append(ids, device.DatabaseID)
	}
	return &Service{queries: sqlc.New(database.DB)}, ids, database.DB
}

func startScheduler(t *testing.T, s *Service, reconcile func(context.Context, sqlc.ListMiningGroupControllersRow) error) (context.CancelFunc, <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	ticker := time.NewTicker(10 * time.Millisecond)
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.run(ctx, ticker.C, reconcile)
	}()
	t.Cleanup(func() {
		cancel()
		ticker.Stop()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("scheduler did not stop")
		}
	})
	return cancel, done
}

func TestSchedulerAdvancesHealthyContainerWhileAnotherIsSlow(t *testing.T) {
	s, ids, _ := newSchedulerFixture(t, 2)
	var slowCalls, healthyCalls atomic.Int32
	var slowRunning atomic.Bool
	startScheduler(t, s, func(ctx context.Context, row sqlc.ListMiningGroupControllersRow) error {
		if row.DeviceID == ids[0] {
			slowCalls.Add(1)
			slowRunning.Store(true)
			defer slowRunning.Store(false)
			<-ctx.Done()
			return ctx.Err()
		}
		healthyCalls.Add(1)
		return nil
	})
	require.Eventually(t, func() bool { return healthyCalls.Load() >= 3 }, 2*time.Second, 10*time.Millisecond,
		"a slow container must not delay the next steps of a healthy container")
	require.True(t, slowRunning.Load())
	require.EqualValues(t, 1, slowCalls.Load(), "a container already running must be skipped on subsequent ticks")
}

func TestSchedulerLimitsConcurrencyAndDrainsOnShutdown(t *testing.T) {
	s, _, conn := newSchedulerFixture(t, 9)
	started := make(chan int64, 32)
	release := make(chan int64)
	canceled := make(chan int64, 32)
	allowExit := make(chan struct{})
	// Unblock canceled callbacks even if an assertion fails.
	defer close(allowExit)
	cancel, done := startScheduler(t, s, func(ctx context.Context, row sqlc.ListMiningGroupControllersRow) error {
		started <- row.DeviceID
		select {
		case <-ctx.Done():
			canceled <- row.DeviceID
			<-allowExit
			return ctx.Err()
		case release <- row.DeviceID:
			_, err := conn.ExecContext(ctx, `UPDATE container_mining_group_state SET status='complete' WHERE device_id=$1`, row.DeviceID)
			if err != nil {
				return fmt.Errorf("complete scheduled test operation: %w", err)
			}
			return nil
		}
	})
	running := map[int64]bool{}
	for range 8 {
		select {
		case id := <-started:
			require.False(t, running[id], "one task per container")
			running[id] = true
		case <-time.After(2 * time.Second):
			t.Fatal("expected eight concurrent tasks")
		}
	}
	select {
	case <-started:
		t.Fatal("started more than eight tasks")
	case <-time.After(100 * time.Millisecond): // Several scheduler ticks with all slots occupied.
	}
	// Completing any task must let the ninth container use the freed slot.
	finished := <-release
	delete(running, finished)
	select {
	case id := <-started:
		require.False(t, running[id], "running containers must not be scheduled twice")
		require.NotEqual(t, finished, id, "completed containers must leave the ready query")
	case <-time.After(2 * time.Second):
		t.Fatal("the ninth container did not use the freed slot")
	}
	cancel()
	for range 8 {
		select {
		case <-canceled:
		case <-time.After(2 * time.Second):
			t.Fatal("shutdown did not cancel every running task")
		}
	}
	select {
	case <-done:
		t.Fatal("scheduler returned before its running tasks exited")
	default:
	}
}
