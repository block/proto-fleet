package mininggroups

import (
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/stretchr/testify/require"
)

func TestScheduledReadinessKeepsUnallocatedControllersReady(t *testing.T) {
	ctx, f := deactivationFixture(t)
	preparePermissionPoolAssignment(t, ctx, f)
	var allocated int
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM hosted_account_asset WHERE device_id=$1 AND client_id IS NOT NULL`, f.target.DeviceID).Scan(&allocated))
	require.Zero(t, allocated)
	_, err := f.conn.ExecContext(ctx, `UPDATE container_mining_group_state SET observed_at=now()-INTERVAL '3 minutes',next_reconcile_at=now()-INTERVAL '1 second' WHERE device_id=$1`, f.target.DeviceID)
	require.NoError(t, err)
	ready, err := f.service.queries.CT1HashboardAllocationReady(ctx, sqlc.CT1HashboardAllocationReadyParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
	require.NoError(t, err)
	require.False(t, ready, "enabled mode cannot authorize allocation from an expired observation")
	rows, err := f.service.queries.ListMiningGroupReadinessControllers(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1, "a container must stay ready before its first allocation and after its last recall")
	require.NoError(t, f.service.reconcileScheduled(ctx, sqlc.ListMiningGroupControllersRow(rows[0])))
	ready, err = f.service.queries.CT1HashboardAllocationReady(ctx, sqlc.CT1HashboardAllocationReadyParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
	require.NoError(t, err)
	require.True(t, ready)
	require.False(t, f.state(t).AttemptID.Valid, "refresh must not enable groups or start a mutation")
	rows, err = f.service.queries.ListMiningGroupReadinessControllers(ctx)
	require.NoError(t, err)
	require.Empty(t, rows, "a fresh idle controller does not need another read")
}

func TestScheduledAllocationReadinessRefreshesIdleControllers(t *testing.T) {
	for _, status := range []string{"idle", "complete", "failed", "missing"} {
		t.Run(status, func(t *testing.T) {
			ctx, f := deactivationFixture(t)
			_, boards := preparePermissionPoolAssignment(t, ctx, f)
			var client int64
			require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities)
			 SELECT gen_random_uuid()::text,organization_id,id,1,'Client',31 FROM hosted_account WHERE organization_id=$1 AND depth=0 RETURNING id`, f.target.OrganizationID).Scan(&client))
			_, err := f.conn.ExecContext(ctx, `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,hashboard_id,device_id,client_id)
			 VALUES($1,7,$2,$2,$3,$4)`, f.target.OrganizationID, boards[0], f.target.DeviceID, client)
			require.NoError(t, err)
			if status == "missing" {
				_, err = f.conn.ExecContext(ctx, `DELETE FROM container_mining_group_state WHERE device_id=$1`, f.target.DeviceID)
			} else {
				_, err = f.conn.ExecContext(ctx, `UPDATE container_mining_group_state SET status=$2,observed_at=now()-interval '3 minutes',next_reconcile_at=now()-interval '1 second' WHERE device_id=$1`, f.target.DeviceID, status)
			}
			require.NoError(t, err)
			rows, err := f.service.queries.ListMiningGroupReadinessControllers(ctx)
			require.NoError(t, err)
			require.Len(t, rows, 1, "stale allocated controllers must refresh without a user request")
			require.NoError(t, f.service.reconcileScheduled(ctx, sqlc.ListMiningGroupControllersRow(rows[0])))
			state := f.state(t)
			require.True(t, state.ObservedAt.Valid)
			require.WithinDuration(t, time.Now(), state.ObservedAt.Time, 5*time.Second)
			require.True(t, state.NextReconcileAt.After(time.Now().Add(50*time.Second)))
			require.False(t, state.AttemptID.Valid, "a mode observation must not create a mutation workflow")
			if status == "missing" {
				require.Equal(t, "idle", state.Status)
			} else {
				require.Equal(t, status, state.Status, "refresh must preserve retained outcomes")
			}
			ready, err := f.service.queries.CT1HashboardAllocationReady(ctx, sqlc.CT1HashboardAllocationReadyParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
			require.NoError(t, err)
			require.True(t, ready)
			rows, err = f.service.queries.ListMiningGroupReadinessControllers(ctx)
			require.NoError(t, err)
			require.Empty(t, rows, "idle observations must not run every worker tick")

			// A failed poll invalidates the snapshot and receives durable backoff.
			_, err = f.conn.ExecContext(ctx, `UPDATE container_mining_group_state SET observed_at=now()-interval '3 minutes' WHERE device_id=$1`, f.target.DeviceID)
			require.NoError(t, err)
			f.miner.fail = true
			err = f.service.reconcileScheduled(ctx, f.makeReadinessDue(t))
			require.ErrorContains(t, err, "unavailable")
			state = f.state(t)
			require.False(t, state.ObservedAt.Valid)
			require.EqualValues(t, 1, state.RetryCount)
			require.True(t, state.NextReconcileAt.After(time.Now()))
			f.miner.fail = false
			f.miner.enabled = false
			require.NoError(t, f.service.reconcileScheduled(ctx, f.makeReadinessDue(t)))
			ready, err = f.service.queries.CT1HashboardAllocationReady(ctx, sqlc.CT1HashboardAllocationReadyParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
			require.NoError(t, err)
			require.False(t, ready, "refresh must not enable a disabled controller")
			require.Zero(t, f.state(t).RetryCount)
		})
	}
}

func (f retryFixture) makeReadinessDue(t *testing.T) sqlc.ListMiningGroupControllersRow {
	t.Helper()
	_, err := f.conn.ExecContext(t.Context(), `UPDATE container_mining_group_state SET next_reconcile_at=clock_timestamp()-INTERVAL '1 second' WHERE device_id=$1`, f.target.DeviceID)
	require.NoError(t, err)
	rows, err := f.service.queries.ListMiningGroupReadinessControllers(t.Context())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	return sqlc.ListMiningGroupControllersRow(rows[0])
}
