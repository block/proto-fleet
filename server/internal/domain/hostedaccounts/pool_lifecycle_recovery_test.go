package hostedaccounts_test

import (
	"context"
	"errors"
	"testing"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	hostedpb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAutomaticClearRepreparesAfterSiteMove(t *testing.T) {
	f, device, newWorker, miner, id := automaticClearRecoveryFixture(t)
	require.NoError(t, newWorker().ProcessLifecycle(t.Context(), id))
	require.Zero(t, device.writes)
	var site int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(name,org_id,slug) VALUES('New site',$1,'clear-new-site') RETURNING id`, f.org).Scan(&site))
	_, err := f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, site, miner)
	require.NoError(t, err)
	device.readError = nil
	require.NoError(t, newWorker().ProcessLifecycle(t.Context(), id))
	row, err := sqlc.New(f.db).GetPoolLifecycleTransition(t.Context(), id)
	require.NoError(t, err)
	require.Equal(t, "completed", row.State)
	require.Equal(t, 1, device.writes)
}

func TestAutomaticClearRestartRecoversOnlyRecordedPreDispatchFailure(t *testing.T) {
	for _, state := range []string{"failed", "uncertain", "applying"} {
		t.Run(state, func(t *testing.T) {
			f, device, newWorker, miner, id := automaticClearRecoveryFixture(t)
			ctx := t.Context()
			require.NoError(t, newWorker().ProcessLifecycle(ctx, id))
			q := sqlc.New(f.db)
			// Recreate a process stopping after recording its failure but before making
			// the clear retryable. A completed attempt alone is not proof of no write:
			// uncertain/applying rows must not be replayed, even with the same receipt.
			writes := devicewrite.NewService(f.db)
			err := writes.WithRecordedHostAttempt(devicewrite.WithPoolOperation(ctx, id), f.org, miner, 0, commandtype.UpdateMiningPools, func(q sqlc.Querier, attempt uuid.UUID) error {
				n, err := q.AcceptPoolRemoval(ctx, sqlc.AcceptPoolRemovalParams{RemovalID: id, PhysicalAttemptID: uuid.NullUUID{UUID: attempt, Valid: true}})
				require.EqualValues(t, 1, n)
				return err
			}, func(context.Context) error { return errors.New("preflight read unavailable") })
			require.Error(t, err)
			_, err = q.FinishPoolRemoval(ctx, sqlc.FinishPoolRemovalParams{RemovalID: id, State: state, ResultCode: "clear_not_dispatched"})
			require.NoError(t, err)
			_, err = f.db.ExecContext(ctx, `UPDATE pool_lifecycle_transition SET next_attempt_at=now() WHERE transition_id=$1`, id)
			require.NoError(t, err)
			pending, err := q.ListPendingPoolLifecycleTransitions(ctx)
			require.NoError(t, err)
			found := false
			for _, p := range pending {
				if p.TransitionID == id {
					found = true
				}
			}
			require.True(t, found, "restart polling must revisit the persisted clear")
			device.readError = nil
			require.NoError(t, newWorker().ProcessLifecycle(ctx, id))
			row, err := q.GetPoolLifecycleTransition(ctx, id)
			require.NoError(t, err)
			if state == "failed" {
				require.Equal(t, 1, device.writes, "a recorded no-write failure can retry")
				require.Equal(t, "completed", row.State)
			} else {
				require.Zero(t, device.writes, "a completed attempt without no-write proof must only be observed")
			}
		})
	}
}

func automaticClearRecoveryFixture(t *testing.T) (poolStepUpFixture, *poolDevice, func() *poolapplications.Worker, int64, uuid.UUID) {
	t.Helper()
	f := newPoolStepUpFixture(t)
	_, _, _, cipher, _ := poolFixture(t, f.fixture)
	repo := poolconfigurations.NewRepository(cipher)
	reviewer := poolapplications.NewReviewer(f.pools, poolCapabilities{sdk.CapabilityPoolConfig: true, sdk.CapabilityPoolClear: true})
	device := &poolDevice{readError: errors.New("offline")}
	getter := poolMinerGetter{device}
	newWorker := func() *poolapplications.Worker {
		checker := poolapplications.NewChecker(f.db, reviewer, getter, cipher, f.pools)
		return poolapplications.NewWorker(f.db, repo, reviewer, getter, cipher).WithRemovals(checker)
	}
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, 0)))
	var id uuid.UUID
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT transition_id FROM pool_lifecycle_transition WHERE device_id=$1 AND state='pending'`, miner).Scan(&id))
	return f, device, newWorker, miner, id
}

func TestAutomaticClearFailedReceiptDoesNotAuthorizeUnsafeRetry(t *testing.T) {
	for _, receipt := range []string{"unknown", "dispatched", "workflow"} {
		t.Run(receipt, func(t *testing.T) {
			f, device, newWorker, miner, id := automaticClearRecoveryFixture(t)
			ctx := t.Context()
			require.NoError(t, newWorker().ProcessLifecycle(ctx, id))
			q := sqlc.New(f.db)
			writes := devicewrite.NewService(f.db)
			err := writes.WithRecordedHostAttempt(devicewrite.WithPoolOperation(ctx, id), f.org, miner, 0, commandtype.UpdateMiningPools, func(q sqlc.Querier, attempt uuid.UUID) error {
				_, err := q.AcceptPoolRemoval(ctx, sqlc.AcceptPoolRemovalParams{RemovalID: id, PhysicalAttemptID: uuid.NullUUID{UUID: attempt, Valid: true}})
				return err
			}, func(context.Context) error { return errors.New("preflight read unavailable") })
			require.Error(t, err)
			_, err = q.FinishPoolRemoval(ctx, sqlc.FinishPoolRemovalParams{RemovalID: id, State: "failed", ResultCode: "clear_not_dispatched"})
			require.NoError(t, err)
			before, err := q.GetPoolLifecycleTransition(ctx, id)
			require.NoError(t, err)
			switch receipt {
			case "unknown":
				_, err = f.db.ExecContext(ctx, `UPDATE device_write_attempt SET status='unknown' WHERE attempt_id=$1`, before.PhysicalAttemptID)
			case "dispatched":
				_, err = f.db.ExecContext(ctx, `UPDATE device_write_attempt SET write_outcome='{"outcome":"completed"}' WHERE attempt_id=$1`, before.PhysicalAttemptID)
			case "workflow":
				_, err = f.db.ExecContext(ctx, `UPDATE device_write_attempt SET command_type='MiningGroups' WHERE attempt_id=$1`, before.PhysicalAttemptID)
			}
			require.NoError(t, err)
			_, err = f.db.ExecContext(ctx, `UPDATE pool_lifecycle_transition SET next_attempt_at=now() WHERE transition_id=$1`, id)
			require.NoError(t, err)
			pending, err := q.ListPendingPoolLifecycleTransitions(ctx)
			require.NoError(t, err)
			for _, p := range pending {
				require.NotEqual(t, id, p.TransitionID)
			}
			device.readError = nil
			require.NoError(t, newWorker().ProcessLifecycle(ctx, id))
			after, err := q.GetPoolLifecycleTransition(ctx, id)
			require.NoError(t, err)
			require.Equal(t, "failed", after.State)
			require.Equal(t, before.PhysicalAttemptID, after.PhysicalAttemptID)
			require.Zero(t, device.writes)
		})
	}
}

func TestAutomaticClearDoesNotReplayAfterRecoveryFindsPoolsRemain(t *testing.T) {
	f, device, newWorker, _, id := automaticClearRecoveryFixture(t)
	device.readError = nil
	device.onWrite = func() { device.readError = errors.New("verification unavailable") }
	require.NoError(t, newWorker().ProcessLifecycle(t.Context(), id))
	require.Equal(t, 1, device.writes)
	// The write settled, but a later observation confirms pools remain. That is
	// a terminal clear failure, not a pre-dispatch failure eligible for retry.
	device.readError = nil
	device.pools = []dto.MiningPool{{URL: "stratum+tcp://remaining.example", Username: "remaining"}}
	require.NoError(t, newWorker().ProcessLifecycle(t.Context(), id))
	row, err := sqlc.New(f.db).GetPoolLifecycleTransition(t.Context(), id)
	require.NoError(t, err)
	require.Equal(t, "failed", row.State)
	require.Equal(t, "pools_remain", row.ResultCode)
	require.NoError(t, newWorker().ProcessLifecycle(t.Context(), id))
	require.Equal(t, 1, device.writes, "failed verification must never resend a dispatched clear")
}
