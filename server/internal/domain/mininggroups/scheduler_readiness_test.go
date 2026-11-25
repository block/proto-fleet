package mininggroups

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/stretchr/testify/require"
)

func TestSchedulerReadinessCannotOccupyWorkflowSlots(t *testing.T) {
	s, ids, conn := newSchedulerFixture(t, 3)
	for _, id := range ids[:2] {
		_, err := conn.ExecContext(t.Context(), `UPDATE container_mining_group_state SET status='idle' WHERE device_id=$1`, id)
		require.NoError(t, err)
		_, err = conn.ExecContext(t.Context(), `UPDATE discovered_device SET device_class=2 WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)`, id)
		require.NoError(t, err)
		_, err = conn.ExecContext(t.Context(), `INSERT INTO device_pairing(device_id,pairing_status) VALUES($1,'PAIRED')`, id)
		require.NoError(t, err)
		var client, board int64
		require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities)
		 SELECT gen_random_uuid()::text,organization_id,id,1,'Client',31 FROM hosted_account
		 WHERE depth=0 AND organization_id=(SELECT org_id FROM device WHERE id=$1) RETURNING id`, id).Scan(&client))
		require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO hashboard(org_id,first_seen_at,last_seen_at)
		 SELECT org_id,now(),now() FROM device WHERE id=$1 RETURNING id`, id).Scan(&board))
		_, err = conn.ExecContext(t.Context(), `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,hashboard_id,device_id,client_id)
		 SELECT org_id,7,$2,$2,id,$3 FROM device WHERE id=$1`, id, board, client)
		require.NoError(t, err)
	}
	_, err := conn.ExecContext(t.Context(), `UPDATE container_mining_group_state SET status='complete' WHERE device_id=$1`, ids[2])
	require.NoError(t, err)
	slow := make(chan int64, 2)
	healthy := make(chan struct{}, 1)
	startScheduler(t, s, func(ctx context.Context, row sqlc.ListMiningGroupControllersRow) error {
		if row.DeviceID == ids[2] {
			healthy <- struct{}{}
			_, err := conn.ExecContext(ctx, `UPDATE container_mining_group_state SET status='complete' WHERE device_id=$1`, row.DeviceID)
			if err != nil {
				return fmt.Errorf("complete scheduled workflow: %w", err)
			}
			return nil
		}
		slow <- row.DeviceID
		<-ctx.Done()
		return ctx.Err()
	})
	for range 2 {
		select {
		case <-slow:
		case <-time.After(3 * time.Second):
			t.Fatal("idle readiness observations did not fill their slots")
		}
	}
	_, err = conn.ExecContext(t.Context(), `UPDATE container_mining_group_state SET status='pending' WHERE device_id=$1`, ids[2])
	require.NoError(t, err)
	select {
	case <-healthy:
	case <-time.After(2 * time.Second):
		t.Fatal("idle observations delayed a newly accepted workflow")
	}
}

func TestSchedulerDrainsReadinessWithoutWaitingForAnotherTick(t *testing.T) {
	const controllers = 100
	s, ids, conn := newSchedulerFixture(t, controllers)
	_, err := conn.ExecContext(t.Context(), `UPDATE container_mining_group_state SET status='idle' WHERE device_id=ANY($1)`, ids)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `UPDATE discovered_device SET device_class=2 WHERE id IN (SELECT discovered_device_id FROM device WHERE id=ANY($1))`, ids)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `INSERT INTO device_pairing(device_id,pairing_status) SELECT id,'PAIRED' FROM device WHERE id=ANY($1)`, ids)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	ticks := make(chan time.Time, 1)
	started := make(chan int64, controllers)
	completed := make(chan int64, controllers)
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.run(ctx, ticks, func(ctx context.Context, row sqlc.ListMiningGroupControllersRow) error {
			started <- row.DeviceID
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-release:
			}
			_, err := conn.ExecContext(ctx, `UPDATE container_mining_group_state SET observed_at=clock_timestamp(),next_reconcile_at=clock_timestamp()+INTERVAL '1 minute' WHERE device_id=$1`, row.DeviceID)
			if err != nil {
				return fmt.Errorf("save scheduled observation: %w", err)
			}
			completed <- row.DeviceID
			return nil
		})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("scheduler did not stop")
		}
	})
	// The old two-per-tick limit needed 50 ticks (150 seconds), longer than
	// the allocation freshness window. No further tick is sent in this test.
	ticks <- time.Now()
	seen := map[int64]bool{}
	next := func() {
		t.Helper()
		select {
		case id := <-started:
			require.False(t, seen[id], "each controller must refresh once")
			seen[id] = true
		case <-time.After(2 * time.Second):
			t.Fatal("a free observation slot waited for another scheduler tick")
		}
	}
	for range maxOrganizationReadinessTasks {
		next()
	}
	select {
	case <-started:
		t.Fatal("observations exceeded the organization budget")
	case <-time.After(50 * time.Millisecond):
	}
	for range controllers - maxOrganizationReadinessTasks {
		release <- struct{}{}
		next()
	}
	for range maxOrganizationReadinessTasks {
		release <- struct{}{}
	}
	for range controllers {
		select {
		case <-completed:
		case <-time.After(2 * time.Second):
			t.Fatal("observation did not finish")
		}
	}
	var refreshed int
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM container_mining_group_state WHERE device_id=ANY($1) AND observed_at>clock_timestamp()-INTERVAL '2 minutes'`, ids).Scan(&refreshed))
	require.Equal(t, controllers, refreshed)
}

func TestSchedulerSlowReadinessLeavesCapacityForHealthyControllersAndOtherOrganizations(t *testing.T) {
	const controllers = 80
	s, ids, conn := newSchedulerOrganizationsFixture(t, []int{64, 16})
	_, err := conn.ExecContext(t.Context(), `UPDATE container_mining_group_state SET status='idle' WHERE device_id=ANY($1)`, ids)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `UPDATE discovered_device SET device_class=2 WHERE id IN (SELECT discovered_device_id FROM device WHERE id=ANY($1))`, ids)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `INSERT INTO device_pairing(device_id,pairing_status) SELECT id,'PAIRED' FROM device WHERE id=ANY($1)`, ids)
	require.NoError(t, err)
	var otherOrg int64
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT org_id FROM device WHERE id=$1`, ids[64]).Scan(&otherOrg))
	slow := make(chan int64, controllers)
	healthy := make(chan int64, controllers)
	startScheduler(t, s, func(ctx context.Context, row sqlc.ListMiningGroupControllersRow) error {
		deadline, bounded := ctx.Deadline()
		if !bounded || time.Until(deadline) > readinessTaskTimeout {
			return fmt.Errorf("readiness task lacks scheduler deadline")
		}
		// The first organization exceeds its own limit. The second mixes two
		// slow controllers with healthy controllers behind them in due order.
		if row.OrganizationID != otherOrg || row.DeviceID == ids[64] || row.DeviceID == ids[65] {
			slow <- row.DeviceID
			<-ctx.Done()
			return ctx.Err()
		}
		_, err := conn.ExecContext(ctx, `UPDATE container_mining_group_state SET observed_at=clock_timestamp(),next_reconcile_at=clock_timestamp()+INTERVAL '1 minute' WHERE device_id=$1`, row.DeviceID)
		if err == nil {
			healthy <- row.DeviceID
			return nil
		}
		return fmt.Errorf("record readiness observation: %w", err)
	})
	for range 14 {
		select {
		case <-healthy:
		case <-time.After(3 * time.Second):
			t.Fatal("a slow organization or two slow controllers starved healthy readiness")
		}
	}
	require.Eventually(t, func() bool { return len(slow) == maxOrganizationReadinessTasks+2 }, time.Second, 10*time.Millisecond)
}

func TestReadinessCapacityReservesPhysicalDispatchSlots(t *testing.T) {
	s, _, conn := newSchedulerFixture(t, 1)
	for _, tc := range []struct{ pool, want int }{
		{pool: 2, want: 0},
		{pool: 4, want: 1},
		{pool: 16, want: 4},
		{pool: 32, want: 8},
		{pool: 250, want: maxReadinessTasks},
	} {
		t.Run(fmt.Sprintf("pool_%d", tc.pool), func(t *testing.T) {
			conn.SetMaxOpenConns(tc.pool)
			capacity, err := readinessCapacity(s.conn)
			require.NoError(t, err)
			require.Equal(t, tc.want, capacity)
		})
	}
}

func TestReadinessLeavesPhysicalDispatchCapacityForWorkflow(t *testing.T) {
	s, ids, conn := newSchedulerFixture(t, 2)
	conn.SetMaxOpenConns(4) // Two dispatch slots: one observation and one workflow.
	_, err := conn.ExecContext(t.Context(), `UPDATE container_mining_group_state SET status='idle' WHERE device_id=ANY($1)`, ids)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `UPDATE discovered_device SET device_class=2 WHERE id IN (SELECT discovered_device_id FROM device WHERE id=ANY($1))`, ids)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `INSERT INTO device_pairing(device_id,pairing_status) SELECT id,'PAIRED' FROM device WHERE id=ANY($1)`, ids)
	require.NoError(t, err)
	idle := make(chan struct{}, 1)
	workflow := make(chan struct{}, 1)
	startScheduler(t, s, func(ctx context.Context, row sqlc.ListMiningGroupControllersRow) error {
		release, err := db.AcquireDispatchBudget(ctx, conn)
		if err != nil {
			return err
		}
		defer release()
		if row.Readiness {
			idle <- struct{}{}
			<-ctx.Done()
			return ctx.Err()
		}
		workflow <- struct{}{}
		_, err = conn.ExecContext(ctx, `UPDATE container_mining_group_state SET status='complete' WHERE device_id=$1`, row.DeviceID)
		if err != nil {
			return fmt.Errorf("complete test workflow: %w", err)
		}
		return nil
	})
	select {
	case <-idle:
	case <-time.After(2 * time.Second):
		t.Fatal("readiness did not acquire its physical dispatch slot")
	}
	_, err = conn.ExecContext(t.Context(), `UPDATE container_mining_group_state SET status='pending' WHERE device_id=$1`, ids[1])
	require.NoError(t, err)
	select {
	case <-workflow:
	case <-time.After(2 * time.Second):
		t.Fatal("readiness consumed dispatch capacity reserved for workflows")
	}
}
