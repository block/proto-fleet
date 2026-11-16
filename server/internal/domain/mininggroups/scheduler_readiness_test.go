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
