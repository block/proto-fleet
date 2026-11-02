package hostedaccounts_test

import (
	"testing"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	hostedpb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPoolCrashRetainsAdmissionUntilExplicitRecovery(t *testing.T) {
	f := newPoolStepUpFixture(t)
	_, _, _, cipher, _ := poolFixture(t, f.fixture)
	ctx, err := hostedaccounts.WithRequestAccountID(f.sessionContext(t), f.account.AccountID)
	require.NoError(t, err)
	reviewer := poolapplications.NewReviewer(f.pools, poolCapabilities{sdk.CapabilityPoolConfig: true})
	repo := poolconfigurations.NewRepository(cipher)
	service := poolconfigurations.NewService(f.db, repo, f.pools, reviewer)
	config, err := service.Create(ctx, f.account.AccountID, poolconfigurations.RevisionOptions{Name: "Production", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example", Username: "payout"}}})
	require.NoError(t, err)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, 0)))
	target := poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: miner, DeviceID: miner}
	review, err := service.ReviewAssignment(ctx, f.account.AccountID, config.ID, config.Version, []poolconfigurations.AssignmentTarget{target})
	require.NoError(t, err)
	saved, err := service.ConfirmReview(ctx, f.account.AccountID, review.ID, "actor-password")
	require.NoError(t, err)
	var attemptID uuid.UUID
	device := &poolDevice{onWrite: func() {
		// This independent connection sees the committed claim before I/O.
		require.NoError(t, f.db.QueryRowContext(ctx, `SELECT physical_attempt_id FROM pool_configuration_operation_target WHERE operation_id=$1 AND state='applying'`, saved.Operation.ID).Scan(&attemptID))
		require.NotEqual(t, uuid.Nil, attemptID)
		panic("simulated dispatcher crash")
	}}
	worker := poolapplications.NewWorker(f.db, repo, reviewer, poolMinerGetter{device}, cipher)
	require.Panics(t, func() { _ = worker.Execute(t.Context(), f.org, saved.Operation.ID, target) })
	q := sqlc.New(f.db)
	_, err = f.db.ExecContext(ctx, `UPDATE pool_configuration_operation_target SET updated_at=now()-interval '1 day' WHERE operation_id=$1`, saved.Operation.ID)
	require.NoError(t, err)
	require.NoError(t, q.RecoverSettledPoolConfigurationWork(ctx))
	current, err := service.GetOperation(ctx, f.account.AccountID, saved.Operation.ID)
	require.NoError(t, err)
	require.Equal(t, "applying", current.Targets[0].State, "elapsed time is not termination evidence")
	require.NoError(t, worker.Execute(t.Context(), f.org, saved.Operation.ID, target))
	require.Equal(t, 1, device.writes, "a restarted executor must not redispatch")
	_, err = service.ReviewRetry(ctx, f.account.AccountID, saved.Operation.ID, []poolconfigurations.AssignmentTarget{target})
	require.Error(t, err)
	writes := devicewrite.NewService(f.db)
	inspections, err := writes.InspectRecovery(ctx, f.org, miner)
	require.NoError(t, err)
	require.Len(t, inspections, 1)
	require.Equal(t, attemptID, inspections[0].Attempt.AttemptID)
	evidence := inspections[0].EvidenceTemplate
	evidence.Operator = "test operator"
	evidence.MaintenanceTermination = "all test dispatchers stopped"
	evidence.OwnerTermination = "test callback terminated"
	evidence.TransportQuiescence = "test transport has no pending calls"
	evidence.DeviceQuiescence = "test device is quiescent"
	evidence.ConfigurationVerification = "test settings inspected"
	_, err = writes.ResolveOffline(ctx, evidence, true)
	require.NoError(t, err)
	require.NoError(t, q.RecoverSettledPoolConfigurationWork(ctx))
	current, err = service.GetOperation(ctx, f.account.AccountID, saved.Operation.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", current.Targets[0].State)
	require.Equal(t, "interrupted", current.Targets[0].ResultCode)
	require.Equal(t, 1, device.writes)
	retry, err := service.ReviewRetry(ctx, f.account.AccountID, saved.Operation.ID, []poolconfigurations.AssignmentTarget{target})
	require.NoError(t, err)
	retried, err := service.ConfirmReview(ctx, f.account.AccountID, retry.ID, "actor-password")
	require.NoError(t, err)
	require.NotEqual(t, saved.Operation.ID, retried.Operation.ID)
	device.onWrite = nil
	require.NoError(t, worker.Execute(t.Context(), f.org, retried.Operation.ID, target))
	require.Equal(t, 2, device.writes)
	// Simulate loss of result bookkeeping after a completed physical call.
	_, err = f.db.ExecContext(ctx, `UPDATE pool_configuration_operation_target SET state='applying',result_code='' WHERE operation_id=$1`, retried.Operation.ID)
	require.NoError(t, err)
	_, err = f.db.ExecContext(ctx, `UPDATE device_write_attempt SET finished_at=now()-interval '2 minutes' WHERE attempt_id=(SELECT physical_attempt_id FROM pool_configuration_operation_target WHERE operation_id=$1)`, retried.Operation.ID)
	require.NoError(t, err)
	require.NoError(t, q.RecoverSettledPoolConfigurationWork(ctx))
	current, err = service.GetOperation(ctx, f.account.AccountID, retried.Operation.ID)
	require.NoError(t, err)
	require.Equal(t, "interrupted", current.Targets[0].ResultCode, "drain alone must not claim settings were verified")
	require.NoError(t, worker.Execute(t.Context(), f.org, retried.Operation.ID, target))
	require.Equal(t, 2, device.writes)
}
