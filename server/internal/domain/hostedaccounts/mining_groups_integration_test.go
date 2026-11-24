package hostedaccounts_test

import (
	"context"
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func TestCT1AllocationRequiresReadyMiningGroups(t *testing.T) {
	cases := []struct {
		name    string
		query   string
		allowed bool
		message string
	}{
		{name: "disabled", query: `UPDATE container_mining_group_state SET observed=observed||'{"enabled":false}' WHERE device_id=$1`, message: "Enable mining groups"},
		{name: "unsupported", query: `UPDATE container_mining_group_state SET observed=observed||'{"supported":false}' WHERE device_id=$1`, message: "does not support mining groups"},
		{name: "unknown", query: `DELETE FROM container_mining_group_state WHERE device_id=$1`, message: "Refresh the container's mining-group state"},
		{name: "stale", query: `UPDATE container_mining_group_state SET observed_at=now()-interval '3 minutes' WHERE device_id=$1`, message: "Refresh the container's mining-group state"},
		{name: "stale disabled", query: `UPDATE container_mining_group_state SET observed_at=now()-interval '3 minutes',observed=observed||'{"enabled":false}' WHERE device_id=$1`, message: "Refresh the container's mining-group state"},
		{name: "future", query: `UPDATE container_mining_group_state SET observed_at=now()+interval '1 minute' WHERE device_id=$1`, message: "Refresh the container's mining-group state"},
		{name: "suspended", query: `UPDATE container_mining_group_state SET observed=observed||'{"suspended":true}' WHERE device_id=$1`, message: "pending mining-group or device changes"},
		{name: "changing", query: `UPDATE container_mining_group_state SET status='pending',operation_kind='deactivate' WHERE device_id=$1`, message: "pending mining-group changes"},
		{name: "pending", query: `UPDATE container_mining_group_state SET observed=observed||'{"pending":true}' WHERE device_id=$1`, message: "pending mining-group or device changes"},
		{name: "ready", allowed: true},
	}
	for _, selection := range []struct {
		name   string
		module bool
	}{{name: "board"}, {name: "module", module: true}} {
		for _, tc := range cases {
			t.Run(tc.name+"/"+selection.name, func(t *testing.T) {
				f := setup(t)
				client := f.client(t, "Client", f.user, hostedaccounts.AllCapabilities)
				module, controller := f.miningModule(t)
				snapshot := f.moduleSelection(t, module)
				asset := selected(7, snapshot.Boards[0].Id, 0)
				if selection.module {
					asset = snapshot
				}
				if tc.query != "" {
					_, err := f.db.ExecContext(t.Context(), tc.query, controller)
					require.NoError(t, err)
				}
				err := f.allocate(t, f.host, f.root, client.AccountID, 1, asset)
				if tc.allowed {
					require.NoError(t, err)
					return
				}
				require.ErrorContains(t, err, tc.message)
				if tc.name != "disabled" {
					require.NotContains(t, err.Error(), "Enable mining groups")
				}
				var count int
				require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_asset WHERE client_id IS NOT NULL`).Scan(&count))
				require.Zero(t, count)
			})
		}
	}
}

func TestCT1DelegationRequiresGroupsButRecallRemainsAvailable(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Client", f.user, hostedaccounts.AllCapabilities)
	child, err := f.svc.CreateChild(t.Context(), f.user, f.org, client.AccountID, "Subclient", f.host, hostedaccounts.DefaultCapabilities)
	require.NoError(t, err)
	module, controller := f.miningModule(t)
	asset := f.moduleSelection(t, module)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, asset))
	asset = f.moduleSelection(t, module)
	_, err = f.db.ExecContext(t.Context(), `UPDATE container_mining_group_state SET observed=observed||'{"enabled":false}' WHERE device_id=$1`, controller)
	require.NoError(t, err)
	require.ErrorContains(t, f.allocate(t, f.user, client.AccountID, child.AccountID, 1, asset), "Enable mining groups")
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 2, asset))
	reason, err := sqlc.New(f.db).GetMiningGroupDeactivationBlocker(t.Context(), sqlc.GetMiningGroupDeactivationBlockerParams{OrganizationID: f.org, DeviceID: controller})
	require.NoError(t, err)
	require.Contains(t, reason, "ownership cleanup", "recalling the final board does not settle its pool cleanup")
}

func TestCT1AllocationRechecksModeAfterAdmissionWait(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Client", f.user, hostedaccounts.AllCapabilities)
	module, controller := f.miningModule(t)
	asset := f.moduleSelection(t, module)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	tx, err := f.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = sqlc.New(tx).LockPoolConfigurationImportOrganization(ctx, f.org)
	require.NoError(t, err)
	var pid int
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid))
	done := make(chan error, 1)
	go func() {
		done <- f.svc.AssignAssets(ctx, f.host, f.org, &pb.AssignAccountAssetsRequest{AccountId: f.root, ChildAccountId: client.AccountID, Intent: 1, Assets: []*pb.MiningAssetSelection{asset}})
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := f.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`, pid).Scan(&waiting)
		return err == nil && waiting
	}, 3*time.Second, 10*time.Millisecond)
	_, err = tx.ExecContext(ctx, `UPDATE container_mining_group_state SET operation_kind='deactivate',status='pending' WHERE device_id=$1`, controller)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	require.ErrorContains(t, <-done, "pending mining-group changes")
}
