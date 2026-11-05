package mininggroups

import (
	"context"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func detachPoolBoard(t *testing.T, ctx context.Context, f retryFixture, board int64) {
	t.Helper()
	_, err := f.conn.ExecContext(ctx, `UPDATE hashboard_installation SET removed_at=now() WHERE hashboard_id=$1 AND removed_at IS NULL`, board)
	require.NoError(t, err)
}

func TestInterruptedPoolOperationWithoutOutstandingStepIsReleased(t *testing.T) {
	for _, reason := range []string{"removed", "reinstalled", "revoked"} {
		t.Run(reason, func(t *testing.T) {
			ctx, f, m, config, boards := newTransferFailureFixture(t)
			original := f.state(t)
			if reason == "revoked" {
				setMiningGroupPermissions(t, ctx, f, nil)
			} else {
				detachPoolBoard(t, ctx, f, boards[0])
				if reason == "reinstalled" {
					_, err := f.conn.ExecContext(ctx, `INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at,metadata) SELECT hashboard_id,module_id,position,now(),now(),'{"macAddress":"02:00:00:00:00:03"}' FROM hashboard_installation WHERE hashboard_id=$1 ORDER BY id DESC LIMIT 1`, boards[0])
					require.NoError(t, err)
				}
			}
			m.calls = nil
			f.reconcile(t, ctx)
			require.Equal(t, "failed", f.state(t).Status)
			require.Equal(t, original.Bindings, f.state(t).Bindings)
			for _, call := range m.calls {
				require.True(t, call.ReadOnly(), "interrupted operations must not perform mutations")
			}
			intents, err := f.service.queries.ListHashboardPoolIntents(ctx, sqlc.ListHashboardPoolIntentsParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
			require.NoError(t, err)
			require.Empty(t, intents, "removed installations and unapplied assignments cannot survive cancellation")
			epoch, err := f.service.writes.Reserve(ctx, f.target.OrganizationID, f.target.DeviceID)
			require.NoError(t, err)
			require.NoError(t, f.service.writes.ReleaseHost(ctx, f.target.OrganizationID, f.target.DeviceID, epoch))
			if reason == "revoked" {
				setMiningGroupPermissions(t, ctx, f, []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead, authz.PermMinerUpdatePools, authz.PermMinerStartMining, authz.PermMinerStopMining})
				_, err = f.service.submitLegacyRequest(ctx, f.target.DeviceIdentifier, uuid.NewString(), config, boards, f.state(t).Revision)
				require.NoError(t, err, "a currently authorized operator can submit a fresh operation")
			}
		})
	}
}

func TestInterruptedTransferIsPolledUntilTerminal(t *testing.T) {
	for _, reason := range []string{"removed", "revoked"} {
		t.Run(reason, func(t *testing.T) {
			ctx, f, m, _, boards := newTransferFailureFixture(t)
			m.transferPhase = pendingTransferPhase
			f.reconcile(t, ctx)
			f.reconcile(t, ctx)
			original := f.state(t)
			require.NotEmpty(t, original.ControllerOperationID)
			if reason == "removed" {
				detachPoolBoard(t, ctx, f, boards[0])
			} else {
				setMiningGroupPermissions(t, ctx, f, nil)
			}
			m.calls = nil
			f.reconcile(t, ctx)
			require.Len(t, m.calls, 1)
			require.Equal(t, "operation", m.calls[0].Action, "receipt polling cannot depend on current board freshness or user write authority")
			require.Equal(t, original.Step, f.state(t).Step)
			_, err := f.service.writes.Reserve(ctx, f.target.OrganizationID, f.target.DeviceID)
			require.Error(t, err)
			m.receipts[original.ControllerOperationID].Phase = transferPhaseFailed
			f.reconcile(t, ctx)
			require.False(t, f.state(t).Step.Valid)
			f.reconcile(t, ctx)
			require.Equal(t, "failed", f.state(t).Status)
			for _, call := range m.calls {
				require.True(t, call.ReadOnly())
			}
			require.Len(t, m.receipts, 1, "revocation or detachment must not dispatch a replacement transfer")
		})
	}
}

func TestInterruptedUnknownWriteKeepsItsIdentity(t *testing.T) {
	for _, reason := range []string{"removed", "revoked"} {
		t.Run(reason, func(t *testing.T) {
			ctx, f, m, _, boards := newTransferFailureFixture(t)
			m.loseReply = true
			f.reconcile(t, ctx)
			f.reconcile(t, ctx)
			original := f.state(t)
			require.Empty(t, original.ControllerOperationID)
			if reason == "removed" {
				detachPoolBoard(t, ctx, f, boards[0])
			} else {
				setMiningGroupPermissions(t, ctx, f, nil)
			}
			m.calls = nil
			f.reconcile(t, ctx)
			require.Empty(t, m.calls, "unknown writes cannot be replayed without valid intent and authority")
			require.Equal(t, original.Step, f.state(t).Step)
			require.Equal(t, original.StepKey, f.state(t).StepKey)
			require.Equal(t, "blocked", f.state(t).Status)
			_, err := f.service.writes.Reserve(ctx, f.target.OrganizationID, f.target.DeviceID)
			require.Error(t, err)
		})
	}
}

func TestStaleInventoryDoesNotCancelPoolIntent(t *testing.T) {
	ctx, f, m, _, boards := newTransferFailureFixture(t)
	_, err := f.conn.ExecContext(ctx, `UPDATE hashboard_slot SET observed_at=now()-INTERVAL '5 minutes' WHERE hashboard_id=$1`, boards[0])
	require.NoError(t, err)
	m.calls = nil
	f.reconcile(t, ctx)
	require.Equal(t, "blocked", f.state(t).Status)
	intents, err := f.service.queries.ListHashboardPoolIntents(ctx, sqlc.ListHashboardPoolIntentsParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
	require.NoError(t, err)
	require.Len(t, intents, 1)
	require.Empty(t, m.calls)
}

func TestInterruptedActivationSettlesOnlyKnownStopOutcome(t *testing.T) {
	for _, stage := range []string{"not_started", "stop_unconfirmed", "stop_accepted", "enabled"} {
		t.Run(stage, func(t *testing.T) {
			ctx, f := newRetryFixture(t)
			if stage != "not_started" {
				f.reconcile(t, ctx) // Capture pre-stop state.
				if stage == "stop_accepted" || stage == "enabled" {
					f.reconcile(t, ctx)
				}
			}
			setMiningGroupPermissions(t, ctx, f, nil)
			f.miner.calls = nil
			if stage == "stop_accepted" {
				f.miner.mining = true
				f.miner.activeBoards = 1
				f.reconcile(t, ctx)
				require.Equal(t, "blocked", f.state(t).Status)
				f.miner.mining = false
				f.miner.activeBoards = 0
			}
			if stage == "enabled" {
				f.miner.enabled = true
			}
			f.reconcile(t, ctx)
			if stage == "stop_unconfirmed" {
				require.Equal(t, "blocked", f.state(t).Status)
			} else {
				require.Equal(t, "failed", f.state(t).Status)
			}
			for _, call := range f.miner.calls {
				require.True(t, call.ReadOnly(), "revocation must never stop, activate, or resume mining")
			}
		})
	}
}

func TestInterruptedOperationWaitsForObservationBeforePruning(t *testing.T) {
	ctx, f, m, _, boards := newTransferFailureFixture(t)
	detachPoolBoard(t, ctx, f, boards[0])
	for _, waiting := range []string{"unavailable", "pending"} {
		m.observeErr = waiting == "unavailable"
		m.pending = waiting == "pending"
		f.reconcile(t, ctx)
		require.Equal(t, "blocked", f.state(t).Status)
		intents, err := f.service.queries.ListHashboardPoolIntents(ctx, sqlc.ListHashboardPoolIntentsParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
		require.NoError(t, err)
		require.Len(t, intents, 1, "retain interruption evidence until it is safe to settle")
		_, err = f.service.writes.Reserve(ctx, f.target.OrganizationID, f.target.DeviceID)
		require.Error(t, err)
	}
	m.pending = false
	f.reconcile(t, ctx)
	require.Equal(t, "failed", f.state(t).Status)
}
