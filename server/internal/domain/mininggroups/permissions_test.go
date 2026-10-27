package mininggroups

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func setMiningGroupPermissions(t *testing.T, ctx context.Context, f retryFixture, keys []string) int64 {
	t.Helper()
	info, err := session.GetInfo(ctx)
	require.NoError(t, err)
	q := sqlc.New(f.conn)
	role, err := q.CreateCustomRole(ctx, sqlc.CreateCustomRoleParams{Name: uuid.NewString(), OrganizationID: sql.NullInt64{Int64: info.OrganizationID, Valid: true}})
	require.NoError(t, err)
	permissions, err := q.GetPermissionsByKeys(ctx, keys)
	require.NoError(t, err)
	require.Len(t, permissions, len(keys))
	for _, permission := range permissions {
		require.NoError(t, q.AssignPermissionToRole(ctx, sqlc.AssignPermissionToRoleParams{RoleID: role.ID, PermissionID: permission.ID}))
	}
	_, err = f.conn.ExecContext(ctx, `UPDATE user_organization_role SET deleted_at=now() WHERE user_id=$1 AND organization_id=$2 AND deleted_at IS NULL`, info.UserID, info.OrganizationID)
	require.NoError(t, err)
	_, err = q.AssignRole(ctx, sqlc.AssignRoleParams{UserID: info.UserID, OrganizationID: info.OrganizationID, RoleID: role.ID, ScopeType: "org"})
	require.NoError(t, err)
	return role.ID
}

func preparePermissionPoolAssignment(t *testing.T, ctx context.Context, f retryFixture) (string, []int64) {
	t.Helper()
	info, err := session.GetInfo(ctx)
	require.NoError(t, err)
	profile, err := f.service.pools.CreateHost(ctx, info.UserID, info.OrganizationID, hostedaccounts.PoolRevisionOptions{Name: "Pools", Slots: []hostedaccounts.PoolSlotOptions{{URL: "stratum+tcp://pool.example:3333", Username: "worker"}}})
	require.NoError(t, err)
	_, err = sqlc.New(f.conn).AdvanceContainerInventory(ctx, sqlc.AdvanceContainerInventoryParams{DeviceIdentifier: f.target.DeviceIdentifier, ObservedAt: time.Now()})
	require.NoError(t, err)
	var tank, module, board int64
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO container_tank(device_id,position,first_seen_at,last_seen_at) VALUES($1,1,now(),now()) RETURNING id`, f.target.DeviceID).Scan(&tank))
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO container_module(tank_id,position,reported_id,first_seen_at,last_seen_at) VALUES($1,1,'module',now(),now()) RETURNING id`, tank).Scan(&module))
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO hashboard(org_id,first_seen_at,last_seen_at) VALUES($1,now(),now()) RETURNING id`, info.OrganizationID).Scan(&board))
	_, err = f.conn.ExecContext(ctx, `INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at,metadata) VALUES($1,$2,1,now(),now(),'{"macAddress":"02:00:00:00:00:01"}')`, board, module)
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `INSERT INTO hashboard_slot(module_id,hashboard_id,position,occupancy,observed_at) VALUES($1,$2,1,'occupied',now())`, module, board)
	require.NoError(t, err)
	f.miner.enabled = true
	_, err = f.service.GetState(ctx, f.target.DeviceIdentifier)
	require.NoError(t, err)
	return profile.RevisionID, []int64{board}
}

func TestMiningGroupSubmissionRequiresStartAndStopPermissions(t *testing.T) {
	for _, kind := range []string{"pools", operationActivate} {
		for _, missing := range []string{authz.PermMinerStartMining, authz.PermMinerStopMining} {
			t.Run(kind+"/"+missing, func(t *testing.T) {
				ctx, f := newIdleRetryFixture(t)
				permission := authz.PermMinerActivateMiningGroups
				config := ""
				var boards []int64
				if kind == "pools" {
					permission = authz.PermMinerUpdatePools
					config, boards = preparePermissionPoolAssignment(t, ctx, f)
				}
				keys := []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead, permission}
				for _, key := range []string{authz.PermMinerStartMining, authz.PermMinerStopMining} {
					if key != missing {
						keys = append(keys, key)
					}
				}
				role := setMiningGroupPermissions(t, ctx, f, keys)
				state, err := f.service.GetState(ctx, f.target.DeviceIdentifier)
				require.NoError(t, err, "state reads do not require start or stop authority")
				requestID := uuid.NewString()
				_, err = f.service.Submit(ctx, f.target.DeviceIdentifier, requestID, config, boards, state.Record.Revision)
				var denied fleeterror.FleetError
				require.ErrorAs(t, err, &denied)
				require.Equal(t, connect.CodePermissionDenied, denied.GRPCCode)
				require.Equal(t, "idle", f.state(t).Status)
				var attempts, intents int
				require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM device_write_attempt WHERE device_id=$1`, f.target.DeviceID).Scan(&attempts))
				require.Zero(t, attempts)
				require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM hashboard_pool_intent WHERE device_id=$1`, f.target.DeviceID).Scan(&intents))
				require.Zero(t, intents)
				permissions, err := sqlc.New(f.conn).GetPermissionsByKeys(ctx, []string{missing})
				require.NoError(t, err)
				require.Len(t, permissions, 1)
				require.NoError(t, sqlc.New(f.conn).AssignPermissionToRole(ctx, sqlc.AssignPermissionToRoleParams{RoleID: role, PermissionID: permissions[0].ID}))
				_, err = f.service.Submit(ctx, f.target.DeviceIdentifier, requestID, config, boards, state.Record.Revision)
				require.NoError(t, err, "the same request is accepted once the missing permission is granted")
			})
		}
	}
}

func TestMiningGroupWorkerRechecksStartAndStopPermissions(t *testing.T) {
	for _, kind := range []string{"pools", operationActivate} {
		for _, missing := range []string{authz.PermMinerStartMining, authz.PermMinerStopMining} {
			t.Run(kind+"/"+missing, func(t *testing.T) {
				ctx, f := newIdleRetryFixture(t)
				permission := authz.PermMinerActivateMiningGroups
				config := ""
				var boards []int64
				if kind == "pools" {
					permission = authz.PermMinerUpdatePools
					config, boards = preparePermissionPoolAssignment(t, ctx, f)
				}
				role := setMiningGroupPermissions(t, ctx, f, []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead, permission, authz.PermMinerStartMining, authz.PermMinerStopMining})
				_, err := f.service.Submit(ctx, f.target.DeviceIdentifier, uuid.NewString(), config, boards, f.state(t).Revision)
				require.NoError(t, err)
				if kind == operationActivate {
					f.prepareActivation(t, ctx)
				} else {
					f.reconcile(t, ctx)
				}
				prepared := f.state(t)
				require.True(t, prepared.Step.Valid)
				_, err = f.conn.ExecContext(ctx, `DELETE FROM role_permission WHERE role_id=$1 AND permission_id=(SELECT id FROM permission WHERE key=$2)`, role, missing)
				require.NoError(t, err)
				f.miner.calls = nil
				f.reconcile(t, ctx)
				require.Empty(t, f.miner.calls, "revoked authority must prevent dispatch of the saved step")
				blocked := f.state(t)
				require.Equal(t, "blocked", blocked.Status)
				require.Equal(t, prepared.Step, blocked.Step)
				require.Equal(t, prepared.StepKey, blocked.StepKey)
				require.Equal(t, prepared.AttemptID, blocked.AttemptID)
			})
		}
	}
}
