package mininggroups

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/authn"
	"github.com/block/proto-fleet/server/internal/domain/authz"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
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
	require.False(t, source.RetiredAt.Valid, "a board move must retain the pending source")
	var account int64
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities) SELECT gen_random_uuid()::text,organization_id,id,1,'New client',31 FROM hosted_account WHERE organization_id=$1 AND depth=0 RETURNING id`, f.target.OrganizationID).Scan(&account))
	_, err = f.conn.ExecContext(ctx, `UPDATE hosted_account_asset SET client_id=$2,revision=revision+1 WHERE kind=7 AND asset_id=$1`, boards[0], account)
	require.ErrorContains(t, err, "ownership cannot change")
	_, err = f.conn.ExecContext(ctx, `DELETE FROM hosted_account_asset WHERE kind=7 AND asset_id=$1`, boards[0])
	require.ErrorContains(t, err, "ownership cannot change")
}

func TestPendingActivationCanRemoveAssignmentWithMissingBoard(t *testing.T) {
	ctx, f, miner, service, _, boards, _ := newActivationPoolFixture(t)
	checker := poolapplications.NewChecker(f.conn, poolapplications.NewReviewer(f.service.pools, sharedCapabilities{}), miner, f.service.secrets, f.service.pools)
	f.service.WithPoolRemovals(checker)
	service.WithChecker(checker)
	completePendingActivation(t, ctx, f, boards[0])
	// A different current operator can recover after the original actor leaves.
	original, err := session.GetInfo(ctx)
	require.NoError(t, err)
	replacement, err := sqlc.New(f.conn).CreateUser(ctx, sqlc.CreateUserParams{UserID: uuid.NewString(), Username: uuid.NewString(), PasswordHash: "unused", CreatedAt: time.Now()})
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `INSERT INTO user_organization(user_id,organization_id,role_id) SELECT $1,organization_id,role_id FROM user_organization WHERE user_id=$2 AND organization_id=$3`, replacement, original.UserID, original.OrganizationID)
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `INSERT INTO user_organization_role(user_id,organization_id,role_id,scope_type,scope_id) SELECT $1,organization_id,role_id,scope_type,scope_id FROM user_organization_role WHERE user_id=$2 AND organization_id=$3 AND deleted_at IS NULL ON CONFLICT DO NOTHING`, replacement, original.UserID, original.OrganizationID)
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `UPDATE user_organization_role SET deleted_at=now() WHERE user_id=$1 AND organization_id=$2`, original.UserID, original.OrganizationID)
	require.NoError(t, err)
	ctx = authn.SetInfo(ctx, &session.Info{UserID: replacement, OrganizationID: original.OrganizationID, AuthMethod: session.AuthMethodSession})
	miner.inventory.Boards = []sdk.HashboardIdentityClaim{}
	miner.snapshot.Groups[0].MACs = nil
	_, err = f.conn.ExecContext(ctx, `UPDATE hashboard_observation SET released_at=now() WHERE hashboard_id=$1`, boards[0])
	require.NoError(t, err)
	removal, err := service.ReviewRemoval(ctx, "", poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: f.target.DeviceID, DeviceID: f.target.DeviceID})
	require.NoError(t, err)
	require.False(t, removal.ClearSupported)
	p, err := f.service.pools.AuthorizePoolConfiguration(ctx, sqlc.New(f.conn), "", poolconfigurations.Read)
	require.NoError(t, err)
	// A clear request must not use the assignment-only exception.
	err = f.service.writes.WithBindingUpdateTransaction(ctx, func(ctx context.Context, q sqlc.Querier) error {
		return checker.EnqueueRemoval(ctx, q, p, removal.ID, false)
	})
	require.ErrorContains(t, err, "Keep device settings")
	// Current permissions are checked again at confirmation, not only at review.
	setMiningGroupPermissions(t, ctx, f, []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead, authz.PermPoolRead})
	err = f.service.writes.WithBindingUpdateTransaction(ctx, func(ctx context.Context, q sqlc.Querier) error {
		return checker.EnqueueRemoval(ctx, q, p, removal.ID, true)
	})
	require.Error(t, err)
	setMiningGroupPermissions(t, ctx, f, []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead, authz.PermPoolRead, authz.PermMinerActivateMiningGroups, authz.PermMinerUpdatePools})
	// The fence and assignment must both survive a failed commit.
	rollback := errors.New("rollback removal")
	err = f.service.writes.WithBindingUpdateTransaction(ctx, func(ctx context.Context, q sqlc.Querier) error {
		if err := checker.EnqueueRemoval(ctx, q, p, removal.ID, true); err != nil {
			return err
		}
		return rollback
	})
	require.ErrorIs(t, err, rollback)
	stillPending, err := sqlc.New(f.conn).HasPendingMiningGroupPoolHandoff(ctx, sqlc.HasPendingMiningGroupPoolHandoffParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
	require.NoError(t, err)
	require.True(t, stillPending)
	var calls []sdk.MiningGroupRequest
	miner.beforeCall = func(_ context.Context, r sdk.MiningGroupRequest) { calls = append(calls, r) }
	writes := miner.writes
	// Use the same transaction and coordinator as confirmation, without a password fixture.
	err = f.service.writes.WithBindingUpdateTransaction(ctx, func(ctx context.Context, q sqlc.Querier) error {
		return checker.EnqueueRemoval(ctx, q, p, removal.ID, true)
	})
	require.NoError(t, err)
	pending, err := sqlc.New(f.conn).HasPendingMiningGroupPoolHandoff(ctx, sqlc.HasPendingMiningGroupPoolHandoffParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
	require.NoError(t, err)
	require.False(t, pending)
	source, err := sqlc.New(f.conn).GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: f.target.OrganizationID, TargetKind: poolconfigurations.TargetDevice, TargetID: f.target.DeviceID})
	require.NoError(t, err)
	require.True(t, source.RetiredAt.Valid)
	require.Equal(t, writes, miner.writes)
	require.True(t, miner.snapshot.Enabled)
	require.Empty(t, calls, "keep-settings removal must send no controller request")
	result, err := service.GetRemoval(ctx, "", removal.ID)
	require.NoError(t, err)
	require.Equal(t, "completed", result.State)
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
