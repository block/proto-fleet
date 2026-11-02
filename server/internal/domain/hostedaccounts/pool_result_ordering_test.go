package hostedaccounts_test

import (
	"context"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	hostedpb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPoolWorkerPreservesVerifiedResultDuringRecall(t *testing.T) {
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

	started, resume := make(chan struct{}), make(chan struct{})
	device := &poolDevice{onWrite: func() {
		close(started)
		select {
		case <-resume:
		case <-t.Context().Done():
		}
	}}
	worker := poolapplications.NewWorker(f.db, repo, reviewer, poolMinerGetter{device}, cipher)
	done := make(chan error, 1)
	go func() { done <- worker.Execute(t.Context(), f.org, saved.Operation.ID, target) }()
	select {
	case <-started:
	case err := <-done:
		t.Fatalf("worker stopped before dispatch: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not dispatch")
	}

	// Delay physical-attempt finalization, which happens after the dispatch
	// transaction releases its transfer locks. A real recall can now run in
	// the old gap before the worker's separate result-saving transaction.
	barrier, err := f.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer barrier.Rollback()
	var blocker int
	require.NoError(t, barrier.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&blocker))
	_, err = barrier.ExecContext(ctx, `SELECT attempt_id FROM device_write_attempt WHERE attempt_id=(SELECT physical_attempt_id FROM pool_configuration_operation_target WHERE operation_id=$1) FOR UPDATE`, saved.Operation.ID)
	require.NoError(t, err)
	close(resume)
	require.Eventually(t, func() bool {
		var waiting bool
		err := f.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)) AND query LIKE '-- name: FinishDeviceWriteAttempt%')`, blocker).Scan(&waiting)
		return err == nil && waiting
	}, 2*time.Second, 10*time.Millisecond)
	require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_RECALL, selected(5, miner, 1)))
	require.NoError(t, barrier.Rollback())
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not finish")
	}
	operation, err := service.GetOperation(ctx, f.account.AccountID, saved.Operation.ID)
	require.NoError(t, err)
	assert.Equal(t, "verified", operation.Targets[0].State, "recall must preserve the completed update's historical result")
	assert.Empty(t, operation.Targets[0].ResultCode)
	var retired bool
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT retired_at IS NOT NULL FROM pool_configuration_assignment WHERE organization_id=$1 AND target_kind='device' AND target_id=$2`, f.org, miner).Scan(&retired))
	assert.True(t, retired, "preserving history must not retain the previous owner's assignment")
	assert.Equal(t, 1, device.writes)
}

// Cancel only after successful device readback, before result persistence.
type poolResultDevice struct {
	*poolDevice
	afterReadback func()
}

func (m poolResultDevice) GetStoredMiningPools(ctx context.Context) ([]interfaces.MinerStoredPool, error) {
	pools, err := m.poolDevice.GetStoredMiningPools(ctx)
	if m.afterReadback != nil {
		m.afterReadback()
	}
	return pools, err
}

func TestPoolWorkerResultPersistenceAfterSuccessfulWrite(t *testing.T) {
	for _, scenario := range []string{"caller canceled", "result commit fails"} {
		t.Run(scenario, func(t *testing.T) {
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

			callCtx, cancel := context.WithCancel(t.Context())
			defer cancel()
			device := poolResultDevice{poolDevice: &poolDevice{}}
			if scenario == "caller canceled" {
				device.afterReadback = cancel
			} else {
				// Fail COMMIT, not the UPDATE, to prove a successful query is not
				// mistaken for a durable result. This affects only this test DB.
				_, err = f.db.ExecContext(ctx, `CREATE FUNCTION fail_test_pool_result_commit() RETURNS trigger LANGUAGE plpgsql AS $$
				BEGIN
				    IF NEW.state='verified' THEN RAISE EXCEPTION 'simulated result commit failure'; END IF;
				    RETURN NEW;
				END; $$;
				CREATE CONSTRAINT TRIGGER fail_test_pool_result_commit AFTER UPDATE ON pool_configuration_operation_target
				DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fail_test_pool_result_commit();`)
				require.NoError(t, err)
			}
			worker := poolapplications.NewWorker(f.db, repo, reviewer, poolMinerGetter{device}, cipher)
			err = worker.Execute(callCtx, f.org, saved.Operation.ID, target)
			if scenario == "caller canceled" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "simulated result commit failure")
			}
			var state, code, physicalState string
			require.NoError(t, f.db.QueryRowContext(ctx, `SELECT t.state,t.result_code,w.status FROM pool_configuration_operation_target t JOIN device_write_attempt w ON w.attempt_id=t.physical_attempt_id WHERE t.operation_id=$1`, saved.Operation.ID).Scan(&state, &code, &physicalState))
			require.Equal(t, "complete", physicalState, "saving a result must not change the completed physical write into an uncertain write")
			if scenario == "caller canceled" {
				require.Equal(t, "verified", state)
			} else {
				require.Equal(t, "applying", state, "failed commits must not claim the result was recorded")
				_, err = f.db.ExecContext(ctx, `UPDATE device_write_attempt SET finished_at=now()-interval '2 minutes' WHERE attempt_id=(SELECT physical_attempt_id FROM pool_configuration_operation_target WHERE operation_id=$1)`, saved.Operation.ID)
				require.NoError(t, err)
				require.NoError(t, sqlc.New(f.db).RecoverSettledPoolConfigurationWork(ctx))
				operation, err := service.GetOperation(ctx, f.account.AccountID, saved.Operation.ID)
				require.NoError(t, err)
				require.Equal(t, "failed", operation.Targets[0].State)
				require.Equal(t, "interrupted", operation.Targets[0].ResultCode)
			}
			require.Empty(t, code)
			require.NoError(t, worker.Execute(t.Context(), f.org, saved.Operation.ID, target))
			require.Equal(t, 1, device.writes, "neither cancellation nor failed bookkeeping may replay the write")
		})
	}
}
