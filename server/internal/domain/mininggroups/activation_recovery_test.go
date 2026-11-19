package mininggroups

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/domain/session"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func completePendingActivation(t *testing.T, ctx context.Context, f retryFixture, board int64) {
	t.Helper()
	_, err := f.conn.ExecContext(ctx, `DELETE FROM hashboard_slot WHERE hashboard_id=$1`, board)
	require.NoError(t, err)
	_, err = f.service.Activate(ctx, f.target.DeviceIdentifier, uuid.NewString())
	require.NoError(t, err)
	for range 12 {
		if active(f.state(t).Status) {
			f.reconcile(t, ctx)
		}
	}
	require.Equal(t, statusComplete, f.state(t).Status)
}

func TestPendingActivationAllowsBoardPlacementMove(t *testing.T) {
	ctx, f, _, _, _, boards, _ := newActivationPoolFixture(t)
	// A released allocation keeps its row even though the operator owns the board.
	_, err := f.conn.ExecContext(ctx, `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,hashboard_id,device_id) VALUES($1,7,$2,$2,$3)`, f.target.OrganizationID, boards[0], f.target.DeviceID)
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `UPDATE pool_configuration_assignment SET operation_id=NULL,adopted_at=now(),version=version+1 WHERE device_id=$1`, f.target.DeviceID)
	require.NoError(t, err)
	completePendingActivation(t, ctx, f, boards[0])
	q := sqlc.New(f.conn)
	identifier := uuid.NewString()
	discovery, err := q.UpsertDiscoveredDevice(ctx, sqlc.UpsertDiscoveredDeviceParams{OrgID: f.target.OrganizationID, DeviceIdentifier: identifier, IpAddress: "127.0.254.1", Port: "8080", UrlScheme: "http", IsActive: true, DriverName: "proto"})
	require.NoError(t, err)
	destination, err := q.InsertDevice(ctx, sqlc.InsertDeviceParams{OrgID: f.target.OrganizationID, DiscoveredDeviceID: discovery, DeviceIdentifier: identifier, MacAddress: "02:00:00:00:ff:01"})
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `UPDATE hashboard_observation SET device_id=$1 WHERE hashboard_id=$2`, destination, boards[0])
	require.NoError(t, err, "placement must not be rejected by the ownership fence")
	var deviceID int64
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT device_id FROM hosted_account_asset WHERE kind=7 AND asset_id=$1`, boards[0]).Scan(&deviceID))
	require.Equal(t, destination, deviceID)
	source, err := q.GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: f.target.OrganizationID, TargetKind: poolconfigurations.TargetDevice, TargetID: f.target.DeviceID})
	require.NoError(t, err)
	require.True(t, source.RetiredAt.Valid, "a board move retains identity intent instead of the container assignment")
	var account int64
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities) SELECT gen_random_uuid()::text,organization_id,id,1,'New client',31 FROM hosted_account WHERE organization_id=$1 AND depth=0 RETURNING id`, f.target.OrganizationID).Scan(&account))
	_, err = f.conn.ExecContext(ctx, `UPDATE hosted_account_asset SET client_id=$2,revision=revision+1 WHERE kind=7 AND asset_id=$1`, boards[0], account)
	require.NoError(t, err, "ownership can change; recovery must recheck it")
	_, err = f.conn.ExecContext(ctx, `DELETE FROM hosted_account_asset WHERE kind=7 AND asset_id=$1`, boards[0])
	require.NoError(t, err, "ownership can change; recovery must recheck it")
}

func TestPendingActivationRecoveryDoesNotRequireOriginalActor(t *testing.T) {
	ctx, f, _, _, _, boards, _ := newActivationPoolFixture(t)
	completePendingActivation(t, ctx, f, boards[0])
	info, err := session.GetInfo(ctx)
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `UPDATE user_organization_role SET deleted_at=now() WHERE user_id=$1 AND organization_id=$2`, info.UserID, info.OrganizationID)
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `INSERT INTO hashboard_slot(module_id,hashboard_id,position,occupancy,observed_at) SELECT module_id,hashboard_id,position,'occupied',now() FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL`, boards[0])
	require.NoError(t, err)
	require.NoError(t, f.service.finishPendingActivationPlacement(ctx, info.OrganizationID, f.target.DeviceID))
	pending, err := sqlc.New(f.conn).HasPendingMiningGroupPoolHandoff(ctx, sqlc.HasPendingMiningGroupPoolHandoffParams{OrganizationID: info.OrganizationID, DeviceID: f.target.DeviceID})
	require.NoError(t, err)
	require.False(t, pending)
}

func TestPendingActivationInventoryReadDoesNotLockOrganization(t *testing.T) {
	ctx, f, miner, _, _, boards, _ := newActivationPoolFixture(t)
	completePendingActivation(t, ctx, f, boards[0])
	called := false
	miner.beforeCall = func(_ context.Context, request sdk.MiningGroupRequest) {
		if request.Action != "activation_inventory" {
			return
		}
		called = true
		lockCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		tx, err := f.conn.BeginTx(lockCtx, nil)
		require.NoError(t, err)
		defer tx.Rollback()
		_, err = sqlc.New(tx).LockPoolConfigurationImportOrganization(lockCtx, f.target.OrganizationID)
		require.NoError(t, err, "slow controller reads must not block other pool updates")
	}
	require.NoError(t, f.service.finishPendingActivationPlacement(ctx, f.target.OrganizationID, f.target.DeviceID))
	require.True(t, called)
}

func TestActivationHandoffInventoryReadDoesNotLockOrganization(t *testing.T) {
	ctx, f, miner, _, _, _, _ := newActivationPoolFixture(t)
	_, err := f.service.Activate(ctx, f.target.DeviceIdentifier, uuid.NewString())
	require.NoError(t, err)
	f.prepareActivation(t, ctx)
	f.reconcile(t, ctx)
	ready := &activationPoolHandoffReady{row: f.state(t), observed: miner.snapshot, observedAt: time.Now()}
	attempt, err := f.service.writes.ResumeWorkflow(ctx, f.target.OrganizationID, f.target.DeviceID, ready.row.AttemptID.UUID)
	require.NoError(t, err)
	called := false
	miner.beforeCall = func(_ context.Context, request sdk.MiningGroupRequest) {
		if request.Action != "activation_inventory" {
			return
		}
		called = true
		lockCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		tx, err := f.conn.BeginTx(lockCtx, nil)
		require.NoError(t, err)
		defer tx.Rollback()
		_, err = sqlc.New(tx).LockPoolConfigurationImportOrganization(lockCtx, f.target.OrganizationID)
		require.NoError(t, err)
	}
	applied, err := f.service.commitActivationPoolHandoff(attempt.Context(ctx), ready)
	require.NoError(t, err)
	require.True(t, applied)
	require.True(t, called)
}

func TestPendingActivationAllowsStopAndReboot(t *testing.T) {
	ctx, f, _, _, _, boards, _ := newActivationPoolFixture(t)
	completePendingActivation(t, ctx, f, boards[0])
	q := sqlc.New(f.conn)
	before, err := q.GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: f.target.OrganizationID, TargetKind: poolconfigurations.TargetDevice, TargetID: f.target.DeviceID})
	require.NoError(t, err)
	for _, kind := range []commandtype.Type{commandtype.StopMining, commandtype.Reboot, commandtype.StartMining, commandtype.UpdateMiningPools, commandtype.ContainerMining, commandtype.ControlPowerFeed, commandtype.Type(-1)} {
		called := false
		err := f.service.writes.WithHostAttempt(ctx, f.target.OrganizationID, f.target.DeviceID, f.state(t).WriteEpoch, kind, func(context.Context) error { called = true; return nil })
		if kind == commandtype.StopMining || kind == commandtype.Reboot {
			require.NoError(t, err)
		} else {
			require.ErrorContains(t, err, devicewrite.ActivationPoolPlacementPending)
		}
		require.Equal(t, kind == commandtype.StopMining || kind == commandtype.Reboot, called)
	}
	after, err := q.GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: f.target.OrganizationID, TargetKind: poolconfigurations.TargetDevice, TargetID: f.target.DeviceID})
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.ErrorContains(t, devicewrite.RequireNoPendingActivationPoolHandoff(ctx, q, f.target.OrganizationID, f.target.DeviceID), devicewrite.ActivationPoolPlacementPending)
	// A pending placement is not permission to interrupt unfinished activation.
	for _, status := range []string{"pending", "applying", "blocked", "failed"} {
		_, err = f.conn.ExecContext(ctx, `UPDATE container_mining_group_state SET status=$1 WHERE device_id=$2`, status, f.target.DeviceID)
		require.NoError(t, err)
		for _, kind := range []commandtype.Type{commandtype.StopMining, commandtype.Reboot} {
			err = f.service.writes.WithHostAttempt(ctx, f.target.OrganizationID, f.target.DeviceID, f.state(t).WriteEpoch, kind, func(context.Context) error { t.Fatal("unfinished activation must block control"); return nil })
			require.Error(t, err)
		}
	}
}

func TestActivationJournalWithoutInventoryRecoversAfterUpgrade(t *testing.T) {
	for _, running := range []bool{false, true} {
		t.Run(fmt.Sprint(running), func(t *testing.T) {
			ctx, f, miner, service, config, boards, _ := newActivationPoolFixture(t)
			miner.running = running
			_, err := f.service.Activate(ctx, f.target.DeviceIdentifier, uuid.NewString())
			require.NoError(t, err)
			// Upgrade after the controller enabled groups, before assignment transfer or resume.
			for range 6 {
				if !miner.snapshot.Enabled {
					f.reconcile(t, ctx)
				}
			}
			require.True(t, miner.snapshot.Enabled)
			require.False(t, miner.running)

			q := sqlc.New(f.conn)
			row := f.state(t)
			capture, done, err := f.service.activationPoolCapture(ctx, q, row)
			require.NoError(t, err)
			require.False(t, done)
			require.NotEmpty(t, capture.Boards, "capture: %+v", capture)
			// Recreate the persisted base-version capture, including its installation ownership evidence.
			scope := poolconfigurations.Scope{OrganizationID: row.OrganizationID}
			actorCtx, err := poolActorContext(ctx, row.OrganizationID, row.ActorID.Int64, "")
			require.NoError(t, err)
			capture.Ownership, err = f.service.pools.AuthorizePoolEquipment(actorCtx, q, scope, row.ActorID.Int64, poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: row.DeviceID, DeviceID: row.DeviceID})
			require.NoError(t, err)
			for i := range capture.Boards {
				capture.Boards[i].Ownership, err = f.service.pools.AuthorizePoolEquipment(actorCtx, q, scope, row.ActorID.Int64, poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetInstallation, ID: capture.Boards[i].Binding.InstallationID, DeviceID: row.DeviceID})
				require.NoError(t, err)
			}
			encoded, err := encode(capture)
			require.NoError(t, err)
			_, err = f.conn.ExecContext(ctx, `UPDATE mining_group_pool_handoff SET captured=$1::jsonb-'Inventory'-'Deferred'-'PartialTransfer' WHERE device_id=$2`, encoded, row.DeviceID)
			require.NoError(t, err)
			miner.beforeCall = func(_ context.Context, r sdk.MiningGroupRequest) {
				require.NotEqual(t, "activation_inventory", r.Action, "old journals must not require the new inventory API")
			}
			for range 12 {
				if active(f.state(t).Status) {
					f.reconcile(t, ctx)
				}
			}
			require.Equal(t, statusComplete, f.state(t).Status)
			require.Equal(t, running, miner.snapshot.Groups[0].Mining)
			assignments, err := service.GetBoardAssignments(ctx, "", poolconfigurations.BoardSelection{HashboardIDs: boards})
			require.NoError(t, err)
			require.Len(t, assignments.Assignments, 1)
			require.Equal(t, config.ID, assignments.Assignments[0].ConfigurationID)
		})
	}
}

type assignmentReadCounter struct {
	sqlc.Querier
	reads int
}

func (q *assignmentReadCounter) GetPoolConfigurationAssignment(ctx context.Context, p sqlc.GetPoolConfigurationAssignmentParams) (sqlc.GetPoolConfigurationAssignmentRow, error) {
	q.reads++
	return q.Querier.GetPoolConfigurationAssignment(ctx, p)
}
func TestIncompleteActivationPlacementDoesNotReadAssignmentsPerBoard(t *testing.T) {
	ctx, f, miner, _, _, boards, _ := newActivationPoolFixture(t, true)
	q := sqlc.New(f.conn)
	inventory, _, err := resolveActivationInventory(ctx, q, f.state(t), true, miner.inventory)
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `DELETE FROM hashboard_slot WHERE hashboard_id=$1`, boards[0])
	require.NoError(t, err)
	counted := &assignmentReadCounter{Querier: q}
	_, complete, err := activationPoolPlacements(ctx, counted, f.state(t), inventory)
	require.NoError(t, err)
	require.False(t, complete)
	require.Zero(t, counted.reads, "placement recovery must use a batch assignment check")
}

func TestActivationPlacementBatchPreservesAssignmentConflicts(t *testing.T) {
	ctx, f, miner, _, _, boards, _ := newActivationPoolFixture(t, true)
	q := sqlc.New(f.conn)
	inventory, _, err := resolveActivationInventory(ctx, q, f.state(t), true, miner.inventory)
	require.NoError(t, err)
	var installation int64
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT id FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL`, boards[1]).Scan(&installation))
	_, err = f.conn.ExecContext(ctx, `UPDATE pool_configuration_assignment SET retired_at=now(),version=version+1 WHERE device_id=$1 AND target_kind='device'`, f.target.DeviceID)
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `INSERT INTO pool_configuration_assignment(organization_id,target_kind,target_id,device_id,configuration_id,revision_id,version,adopted_at) SELECT organization_id,'hashboard_installation',$2,device_id,configuration_id,revision_id,1,now() FROM pool_configuration_assignment WHERE device_id=$1 AND target_kind='device'`, f.target.DeviceID, installation)
	require.NoError(t, err)
	for _, retired := range []bool{false, true} {
		if retired {
			_, err = f.conn.ExecContext(ctx, `UPDATE pool_configuration_assignment SET retired_at=now(),version=version+1 WHERE target_kind='hashboard_installation' AND target_id=$1`, installation)
			require.NoError(t, err)
		}
		_, _, err = activationPoolPlacements(ctx, q, f.state(t), inventory)
		require.ErrorContains(t, err, activationPoolRecovery)
	}
	found, err := q.HasActivationPoolInstallationAssignments(ctx, sqlc.HasActivationPoolInstallationAssignmentsParams{OrganizationID: f.target.OrganizationID + 1, InstallationIds: []int64{installation}})
	require.NoError(t, err)
	require.False(t, found, "assignments from another organization must not affect recovery")
}
