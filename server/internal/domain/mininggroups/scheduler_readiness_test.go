package mininggroups

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
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
	next()
	next()
	select {
	case <-started:
		t.Fatal("observations exceeded their two-slot budget")
	case <-time.After(50 * time.Millisecond):
	}
	for range controllers - 2 {
		release <- struct{}{}
		next()
	}
	release <- struct{}{}
	release <- struct{}{}
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
