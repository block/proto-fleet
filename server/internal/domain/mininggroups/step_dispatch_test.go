package mininggroups

import (
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/domain/session"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestRapidRecallRetiresPreparedClientCleanupAndCompletesHostCleanup(t *testing.T) {
	ctx, f := newIdleRetryFixture(t)
	_, boards := preparePermissionPoolAssignment(t, ctx, f)
	publishSharedBoardInventory(t, ctx, f)
	firmware := &simulatedGroups{snapshot: sdk.MiningGroupResult{Supported: true, Enabled: true, Revision: 1, MaxGroups: 4, Groups: []sdk.MiningGroup{{ID: "default", Revision: 1, Mining: true, MACs: []string{"02:00:00:00:00:01"}, Pools: []sdk.MiningGroupPool{{URL: "stratum+tcp://host.example:3333", Username: "host", Enabled: true}}}}}, receipts: map[string]sdk.MiningGroupResult{}}
	guarded := &admittedGroups{firmware}
	f.service.miners = guarded
	repo := poolconfigurations.NewRepository(f.service.secrets)
	reviewer := poolapplications.NewReviewer(f.service.pools, removalCapabilities{})
	checker := poolapplications.NewChecker(f.conn, reviewer, guarded, f.service.secrets, f.service.pools)
	f.service.WithPoolConfigurations(repo, reviewer).WithPoolRemovals(checker)
	worker := poolapplications.NewWorker(f.conn, repo, reviewer, guarded, f.service.secrets).WithRemovals(checker).WithBoardApplications(f.service)
	_, err := f.conn.ExecContext(ctx, `UPDATE pool_lifecycle_transition SET state='completed' WHERE device_id=$1`, f.target.DeviceID)
	require.NoError(t, err)
	_, err = f.service.GetState(ctx, f.target.DeviceIdentifier)
	require.NoError(t, err)
	var rootID, clientID string
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT account_id FROM hosted_account WHERE organization_id=$1 AND depth=0`, f.target.OrganizationID).Scan(&rootID))
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities) SELECT gen_random_uuid()::text,organization_id,id,1,'Client',31 FROM hosted_account WHERE organization_id=$1 AND depth=0 RETURNING account_id`, f.target.OrganizationID).Scan(&clientID))
	info, err := session.GetInfo(ctx)
	require.NoError(t, err)
	accounts := hostedaccounts.NewService(f.conn, nil)
	require.NoError(t, accounts.AssignAssets(ctx, info.UserID, info.OrganizationID, &pb.AssignAccountAssetsRequest{AccountId: rootID, ChildAccountId: clientID, Intent: pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, Assets: []*pb.MiningAssetSelection{{Kind: pb.MiningAssetKind_MINING_ASSET_KIND_HASHBOARD, Id: boards[0]}}}))
	var clientPolicy uuid.UUID
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT transition_id FROM pool_lifecycle_transition WHERE asset_kind=7 AND asset_id=$1 ORDER BY created_at DESC LIMIT 1`, boards[0]).Scan(&clientPolicy))
	require.NoError(t, worker.ProcessLifecycle(ctx, clientPolicy))
	f.reconcile(t, ctx)
	prepared := f.state(t)
	require.True(t, prepared.Step.Valid)
	require.Empty(t, firmware.receipts, "the old client cleanup has prepared but has not sent its request")
	require.NoError(t, accounts.AssignAssets(ctx, info.UserID, info.OrganizationID, &pb.AssignAccountAssetsRequest{AccountId: rootID, ChildAccountId: clientID, Intent: pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_RECALL, Assets: []*pb.MiningAssetSelection{{Kind: pb.MiningAssetKind_MINING_ASSET_KIND_HASHBOARD, Id: boards[0], ExpectedRevision: 1}}}))
	var hostPolicy uuid.UUID
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT transition_id FROM pool_lifecycle_transition WHERE asset_kind=7 AND asset_id=$1 ORDER BY created_at DESC LIMIT 1`, boards[0]).Scan(&hostPolicy))
	require.NotEqual(t, clientPolicy, hostPolicy)
	firmware.calls = nil
	f.reconcile(t, ctx)
	require.False(t, active(f.state(t).Status), f.state(t).Message)
	for _, call := range firmware.calls {
		require.True(t, call.ReadOnly(), "old client authority cannot send a new mutation")
	}
	old, err := f.service.queries.GetPoolLifecycleTransition(ctx, clientPolicy)
	require.NoError(t, err)
	require.Equal(t, "superseded", old.State)
	require.NoError(t, worker.ProcessLifecycle(ctx, hostPolicy))
	for range 40 {
		if active(f.state(t).Status) {
			f.reconcile(t, ctx)
		}
	}
	require.Equal(t, statusComplete, f.state(t).Status, f.state(t).Message)
	recalled, err := f.service.queries.GetPoolLifecycleTransition(ctx, hostPolicy)
	require.NoError(t, err)
	require.Equal(t, "completed", recalled.State)
	review := reviewDefaultReturn(t, ctx, f, boards)
	require.NotEmpty(t, review.Token, "the operator can now confirm the move to Default")
}

func TestInterruptedPreparedStepCanReleaseAdmissionAfterRestart(t *testing.T) {
	for _, reason := range []string{"revoked", "removed"} {
		t.Run(reason, func(t *testing.T) {
			ctx, f, firmware, _, boards := newTransferFailureFixture(t)
			f.reconcile(t, ctx)
			prepared := f.state(t)
			require.True(t, prepared.Step.Valid)
			require.Empty(t, firmware.receipts)
			if reason == "revoked" {
				setMiningGroupPermissions(t, ctx, f, nil)
			} else {
				detachPoolBoard(t, ctx, f, boards[0])
			}
			f.service = NewService(f.conn, firmware, f.service.pools, f.service.secrets)
			firmware.calls = nil
			f.reconcile(t, ctx)
			require.Equal(t, "failed", f.state(t).Status)
			require.False(t, f.state(t).Step.Valid)
			require.Empty(t, firmware.receipts)
			for _, call := range firmware.calls {
				require.True(t, call.ReadOnly())
			}
			var status string
			require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT status FROM device_write_attempt WHERE attempt_id=$1`, prepared.AttemptID.UUID).Scan(&status))
			require.Equal(t, "complete", status)
		})
	}
}

func TestDispatchMarkerSurvivesLostJournalCommit(t *testing.T) {
	ctx, f, firmware, _, _ := newTransferFailureFixture(t)
	firmware.transferPhase = transferPhaseComplete
	f.reconcile(t, ctx)
	prepared := f.state(t)
	_, err := f.conn.ExecContext(ctx, `CREATE FUNCTION reject_receipt_save() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF OLD.step IS NOT NULL AND NEW.step IS NULL THEN RAISE EXCEPTION 'receipt save failed'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_receipt_save BEFORE UPDATE ON container_mining_group_state FOR EACH ROW EXECUTE FUNCTION reject_receipt_save()`)
	require.NoError(t, err)
	require.ErrorContains(t, f.service.Reconcile(ctx, f.target.OrganizationID, f.target.DeviceID, f.target.DeviceIdentifier), "receipt save failed")
	require.Len(t, firmware.receipts, 1, "controller accepted the physical request")
	require.Equal(t, prepared.Step, f.state(t).Step, "journal transaction rolled back")
	unsent, err := f.service.queries.MiningGroupStepIsUnsent(ctx, sqlc.MiningGroupStepIsUnsentParams{StepKey: prepared.StepKey.UUID, AttemptID: prepared.AttemptID.UUID})
	require.NoError(t, err)
	require.False(t, unsent, "independently committed dispatch marker must survive rollback")
	_, err = f.conn.ExecContext(ctx, `DROP TRIGGER reject_receipt_save ON container_mining_group_state`)
	require.NoError(t, err)
	setMiningGroupPermissions(t, ctx, f, nil)
	f.service = NewService(f.conn, firmware, f.service.pools, f.service.secrets)
	firmware.calls = nil
	f.reconcile(t, ctx)
	require.Equal(t, "blocked", f.state(t).Status)
	require.Equal(t, prepared.Step, f.state(t).Step)
	require.Empty(t, firmware.calls, "unknown writes cannot be replayed or discarded after access changes")
}

func TestInterruptedStepWithoutDispatchHistoryRemainsProtected(t *testing.T) {
	ctx, f, firmware, _, _ := newTransferFailureFixture(t)
	f.reconcile(t, ctx)
	prepared := f.state(t)
	_, err := f.conn.ExecContext(ctx, `DELETE FROM mining_group_step_dispatch WHERE step_key=$1`, prepared.StepKey.UUID)
	require.NoError(t, err, "simulate a request saved before dispatch tracking existed")
	setMiningGroupPermissions(t, ctx, f, nil)
	f.service = NewService(f.conn, firmware, f.service.pools, f.service.secrets)
	firmware.calls = nil
	f.reconcile(t, ctx)
	require.Equal(t, "blocked", f.state(t).Status)
	require.Equal(t, prepared.Step, f.state(t).Step)
	require.Equal(t, prepared.StepKey, f.state(t).StepKey)
	require.Empty(t, firmware.calls)
}

func TestDispatchMarkerFailurePreventsControllerMutation(t *testing.T) {
	ctx, f, firmware, _, _ := newTransferFailureFixture(t)
	f.reconcile(t, ctx)
	_, err := f.conn.ExecContext(ctx, `CREATE FUNCTION reject_dispatch_marker() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'dispatch marker unavailable'; END $$; CREATE TRIGGER reject_dispatch_marker BEFORE UPDATE ON mining_group_step_dispatch FOR EACH ROW EXECUTE FUNCTION reject_dispatch_marker()`)
	require.NoError(t, err)
	firmware.calls = nil
	require.ErrorContains(t, f.service.Reconcile(ctx, f.target.OrganizationID, f.target.DeviceID, f.target.DeviceIdentifier), "dispatch marker unavailable")
	require.Empty(t, firmware.receipts)
	for _, call := range firmware.calls {
		require.True(t, call.ReadOnly())
	}
}
