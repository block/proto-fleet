package mininggroups

import (
	"context"
	"testing"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/miner"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// The remote resolution path does not consult local plugins.
type remoteOnlyPlugins struct{ miner.PluginManager }

func TestDeactivationChecksNodeProtocolBeforeCommittingAdmission(t *testing.T) {
	for _, version := range []gatewaypb.CommandProtocolVersion{gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V14, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V15} {
		t.Run(version.String(), func(t *testing.T) {
			ctx, f := deactivationFixture(t)
			registry := control.NewRegistry()
			var nodeID int64
			require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO fleet_node(org_id,name,identity_pubkey,encryption_pubkey,enrollment_status)
 VALUES($1,'protocol-test',$2,$3,'CONFIRMED') RETURNING id`, f.target.OrganizationID, []byte("node-identity"), []byte("01234567890123456789012345678901")).Scan(&nodeID))
			_, err := f.conn.ExecContext(ctx, `INSERT INTO fleet_node_device(fleet_node_id,device_id,org_id) VALUES($1,$2,$3)`, nodeID, f.target.DeviceID, f.target.OrganizationID)
			require.NoError(t, err)
			stream, err := registry.RegisterAuthenticated(nodeID, "node", version)
			require.NoError(t, err)
			defer stream.Unregister()
			fileStore, err := files.NewService(files.Config{})
			require.NoError(t, err)
			miners := miner.NewMinerService(f.conn, sqlstores.NewSQLUserStore(f.conn), f.service.secrets, fileStore, remoteOnlyPlugins{}).WithCommandSender(registry)
			handle, err := miners.GetMinerForCommand(ctx, f.target.DeviceID)
			require.NoError(t, err)
			require.IsType(t, &miner.RemoteFleetNodeMiner{}, handle)
			f.service.miners = miners
			before := f.state(t)
			request := uuid.New()
			_, err = f.service.Deactivate(ctx, f.target.DeviceIdentifier, request.String())
			if version == gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V15 {
				require.NoError(t, err)
				require.Equal(t, "pending", f.state(t).Status)
				return
			}
			require.ErrorContains(t, err, "Upgrade the Fleet Node")
			after := f.state(t)
			require.Equal(t, before.Status, after.Status)
			require.Equal(t, before.Revision, after.Revision)
			var attempts, requests int
			require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM device_write_attempt WHERE device_id=$1 AND operation_kind='mutation'`, f.target.DeviceID).Scan(&attempts))
			require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM mining_group_request WHERE request_id=$1`, request).Scan(&requests))
			require.Zero(t, attempts)
			require.Zero(t, requests)
		})
	}
}

func deactivationFixture(t *testing.T) (context.Context, retryFixture) {
	t.Helper()
	ctx, f := newIdleRetryFixture(t)
	f.miner.enabled = true
	// This fixture has no default or board allocation. Pairing therefore keeps
	// the existing pools and its policy can finish without any device mutation.
	_, err := f.conn.ExecContext(ctx, `UPDATE pool_lifecycle_transition SET state='completed' WHERE device_id=$1`, f.target.DeviceID)
	require.NoError(t, err)
	state, err := f.service.GetState(ctx, f.target.DeviceIdentifier)
	require.NoError(t, err)
	require.Empty(t, state.DeactivationBlockedReason)
	return ctx, f
}

func TestDeactivationRetainsRequestAfterLostReplyAndVerifiesDisabledMode(t *testing.T) {
	ctx, f := deactivationFixture(t)
	f.miner.loseDeactivate = true
	id := uuid.NewString()
	_, err := f.service.Deactivate(ctx, f.target.DeviceIdentifier, id)
	require.NoError(t, err)
	f.reconcile(t, ctx) // Persist exact request before dispatch.
	prepared := f.state(t)
	require.True(t, prepared.Step.Valid)
	f.reconcile(t, ctx) // Controller commits; response is lost.
	require.Equal(t, "blocked", f.state(t).Status)
	require.False(t, f.miner.enabled)
	f.reconcile(t, ctx) // Replay the same request even though observation is disabled.
	require.Equal(t, prepared.StepKey.UUID.String(), f.miner.deactivatedKey)
	require.False(t, f.state(t).Step.Valid)
	f.reconcile(t, ctx) // Fresh disabled observation settles the workflow.
	require.Equal(t, statusComplete, f.state(t).Status)
	_, err = f.service.Deactivate(ctx, f.target.DeviceIdentifier, id)
	require.NoError(t, err, "completed request replay must not create a new mutation")
	_, err = f.service.Activate(ctx, f.target.DeviceIdentifier, id)
	require.ErrorContains(t, err, "different settings")
	var writes int
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM device_write_attempt WHERE device_id=$1 AND operation_kind='mutation'`, f.target.DeviceID).Scan(&writes))
	require.Equal(t, 1, writes)
}

func TestDeactivationWaitsForOwnershipCleanupButNotRetainedHistory(t *testing.T) {
	for _, state := range []string{"pending", "ready", "applying", "uncertain", "completed", "verified", "failed", "superseded"} {
		t.Run(state, func(t *testing.T) {
			ctx, f := deactivationFixture(t)
			_, err := f.conn.ExecContext(ctx, `UPDATE pool_lifecycle_transition SET state=$2 WHERE device_id=$1`, f.target.DeviceID, state)
			require.NoError(t, err)
			_, err = f.service.Deactivate(ctx, f.target.DeviceIdentifier, uuid.NewString())
			if state == "pending" || state == "ready" || state == "applying" || state == "uncertain" {
				require.ErrorContains(t, err, "ownership cleanup")
				return
			}
			require.NoError(t, err, "terminal history must not block forever")
		})
	}
}

func TestDeactivationBlocksNewPhysicalAndAutomaticWork(t *testing.T) {
	ctx, f := deactivationFixture(t)
	_, err := f.service.Deactivate(ctx, f.target.DeviceIdentifier, uuid.NewString())
	require.NoError(t, err)
	state := f.state(t)
	err = db.WithTransactionNoResult(ctx, f.conn, func(q sqlc.Querier) error {
		if _, err := q.LockDeviceWriteFenceRecord(ctx, sqlc.LockDeviceWriteFenceRecordParams{DeviceID: f.target.DeviceID, OrganizationID: f.target.OrganizationID}); err != nil {
			return err
		}
		return devicewrite.RequireNoMiningGroupDeactivation(ctx, q, f.target.OrganizationID, f.target.DeviceID)
	})
	require.ErrorContains(t, err, "being disabled")
	// Automatic policy is retained without rejecting its pairing or inventory
	// transaction. It must wait without pinning the admitted deactivation.
	var deferred uuid.UUID
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,reason,pairing_cycle_id)
	 SELECT organization_id,device_id,asset_kind,asset_id,reason,pairing_cycle_id FROM pool_lifecycle_transition WHERE device_id=$1 LIMIT 1 RETURNING transition_id`, f.target.DeviceID).Scan(&deferred))
	policy, err := sqlc.New(f.conn).GetPoolLifecycleTransition(ctx, deferred)
	require.NoError(t, err)
	require.Equal(t, "deferred", policy.State)
	require.Equal(t, state.AttemptID, f.state(t).AttemptID)
	require.NoError(t, sqlc.New(f.conn).ReconcilePoolLifecycleOperations(ctx))
	policy, err = sqlc.New(f.conn).GetPoolLifecycleTransition(ctx, deferred)
	require.NoError(t, err)
	require.Equal(t, "deferred", policy.State)
	for range 4 {
		if active(f.state(t).Status) {
			f.reconcile(t, ctx)
		}
	}
	require.Equal(t, statusComplete, f.state(t).Status, "deferred policy must not block the admitted deactivation")
	_, err = f.service.Deactivate(ctx, f.target.DeviceIdentifier, uuid.NewString())
	require.ErrorContains(t, err, "ownership cleanup", "a new admission must respect the retained policy")
	require.NoError(t, sqlc.New(f.conn).ReconcilePoolLifecycleOperations(ctx))
	policy, err = sqlc.New(f.conn).GetPoolLifecycleTransition(ctx, deferred)
	require.NoError(t, err)
	require.Equal(t, "pending", policy.State)
}

func TestDeactivationRequiresItsOwnPermission(t *testing.T) {
	ctx, f := deactivationFixture(t)
	i, err := session.GetInfo(ctx)
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `DELETE FROM role_permission WHERE permission_id=(SELECT id FROM permission WHERE key='miner:deactivate_mining_groups') AND role_id IN (SELECT role_id FROM user_organization_role WHERE user_id=$1)`, i.UserID)
	require.NoError(t, err)
	_, err = f.service.Deactivate(ctx, f.target.DeviceIdentifier, uuid.NewString())
	require.ErrorContains(t, err, "permission")
}

func TestFailedModeObservationCannotAuthorizeAllocation(t *testing.T) {
	ctx, f := deactivationFixture(t)
	q := sqlc.New(f.conn)
	params := sqlc.CT1HashboardAllocationReadyParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID}
	ready, err := q.CT1HashboardAllocationReady(ctx, params)
	require.NoError(t, err)
	require.True(t, ready)
	f.miner.fail = true
	state, err := f.service.GetState(ctx, f.target.DeviceIdentifier)
	require.NoError(t, err, "the previous snapshot remains available for display")
	require.Contains(t, state.DeactivationBlockedReason, "Reconnect")
	ready, err = q.CT1HashboardAllocationReady(ctx, params)
	require.NoError(t, err)
	require.False(t, ready, "a recently cached enabled mode cannot hide the failed refresh")
	f.miner.fail = false
	_, err = f.service.GetState(ctx, f.target.DeviceIdentifier)
	require.NoError(t, err)
	ready, err = q.CT1HashboardAllocationReady(ctx, params)
	require.NoError(t, err)
	require.True(t, ready)
}

func TestDeactivationRejectsAllocatedBoardsAndWaitsAfterFinalRecall(t *testing.T) {
	ctx, f := deactivationFixture(t)
	_, boards := preparePermissionPoolAssignment(t, ctx, f)
	var client int64
	var childID, rootID string
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities)
	 SELECT gen_random_uuid()::text,organization_id,id,1,'Client',31 FROM hosted_account WHERE organization_id=$1 AND depth=0 RETURNING id,account_id`, f.target.OrganizationID).Scan(&client, &childID))
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT account_id FROM hosted_account WHERE organization_id=$1 AND depth=0`, f.target.OrganizationID).Scan(&rootID))
	_, err := f.conn.ExecContext(ctx, `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,hashboard_id,device_id,client_id) VALUES($1,7,$2,$2,$3,$4)`, f.target.OrganizationID, boards[0], f.target.DeviceID, client)
	require.NoError(t, err)
	_, err = f.service.Deactivate(ctx, f.target.DeviceIdentifier, uuid.NewString())
	require.ErrorContains(t, err, "Recall all allocated hashboards")
	i, err := session.GetInfo(ctx)
	require.NoError(t, err)
	err = hostedaccounts.NewService(f.conn, nil).AssignAssets(ctx, i.UserID, i.OrganizationID, &pb.AssignAccountAssetsRequest{AccountId: rootID, ChildAccountId: childID, Intent: pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_RECALL, Assets: []*pb.MiningAssetSelection{{Kind: pb.MiningAssetKind_MINING_ASSET_KIND_HASHBOARD, Id: boards[0], ExpectedRevision: 1}}})
	require.NoError(t, err)
	_, err = f.service.Deactivate(ctx, f.target.DeviceIdentifier, uuid.NewString())
	require.ErrorContains(t, err, "ownership cleanup")
	_, err = f.conn.ExecContext(ctx, `UPDATE pool_lifecycle_transition SET state='completed' WHERE device_id=$1 AND state IN ('pending','ready')`, f.target.DeviceID)
	require.NoError(t, err)
	_, err = f.service.Deactivate(ctx, f.target.DeviceIdentifier, uuid.NewString())
	require.NoError(t, err, "settled cleanup permits deactivation")
}
