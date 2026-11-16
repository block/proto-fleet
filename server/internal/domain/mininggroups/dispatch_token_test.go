package mininggroups

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDispatchPreflightDoesNotMarkSavedStep(t *testing.T) {
	ctx, f, firmware, _, _ := newTransferFailureFixture(t)
	f.reconcile(t, ctx)
	row := f.state(t)
	attempt, err := f.service.writes.ResumeWorkflow(ctx, row.OrganizationID, row.DeviceID, row.AttemptID.UUID)
	require.NoError(t, err)
	require.NoError(t, devicewrite.CompleteWorkflow(ctx, f.service.queries, row.AttemptID.UUID))
	firmware.calls = nil
	_, err = f.service.dispatchStep(attempt.Context(ctx), row, sdk.MiningGroupRequest{Action: "stop", GroupID: "default", Key: row.StepKey.UUID.String()})
	require.ErrorContains(t, err, "admission changed")
	require.Empty(t, firmware.calls)
	unsent, err := f.service.queries.MiningGroupStepIsUnsent(ctx, sqlc.MiningGroupStepIsUnsentParams{StepKey: row.StepKey.UUID, AttemptID: row.AttemptID.UUID})
	require.NoError(t, err)
	require.True(t, unsent, "preflight refusal must leave the saved step provably unsent")
}

func TestDispatchRestoreIsIdempotentAndCannotClearNewerToken(t *testing.T) {
	ctx, f, _, _, _ := newTransferFailureFixture(t)
	f.reconcile(t, ctx)
	row := f.state(t)
	first, second := uuid.New(), uuid.New()
	mark := func(token uuid.UUID) bool {
		wasUnsent, err := f.service.queries.MarkMiningGroupStepDispatch(ctx, sqlc.MarkMiningGroupStepDispatchParams{StepKey: row.StepKey.UUID, AttemptID: row.AttemptID.UUID, DispatchToken: token})
		require.NoError(t, err)
		return wasUnsent
	}
	restore := func(token uuid.UUID) int64 {
		rows, err := f.service.queries.RestoreMiningGroupStepUnsent(ctx, sqlc.RestoreMiningGroupStepUnsentParams{StepKey: row.StepKey.UUID, AttemptID: row.AttemptID.UUID, DispatchToken: token})
		require.NoError(t, err)
		return rows
	}
	require.True(t, mark(first))
	require.EqualValues(t, 1, restore(first))
	require.EqualValues(t, 1, restore(first), "a repeated acknowledgement must keep the same result")
	require.True(t, mark(first))
	require.False(t, mark(second), "a newer invocation must retain the earlier unknown send")
	require.Zero(t, restore(first), "an old refusal cannot clear the newer dispatch")
	unsent, err := f.service.queries.MiningGroupStepIsUnsent(ctx, sqlc.MiningGroupStepIsUnsentParams{StepKey: row.StepKey.UUID, AttemptID: row.AttemptID.UUID})
	require.NoError(t, err)
	require.False(t, unsent)
}

func TestDispatchMissingHistoryCannotBecomeUnsent(t *testing.T) {
	ctx, f, firmware, _, _ := newTransferFailureFixture(t)
	f.reconcile(t, ctx)
	row := f.state(t)
	_, err := f.conn.ExecContext(ctx, `DELETE FROM mining_group_step_dispatch WHERE step_key=$1`, row.StepKey.UUID)
	require.NoError(t, err)
	firmware.refuseMutation = true
	f.reconcile(t, ctx)
	unsent, err := f.service.queries.MiningGroupStepIsUnsent(ctx, sqlc.MiningGroupStepIsUnsentParams{StepKey: row.StepKey.UUID, AttemptID: row.AttemptID.UUID})
	require.NoError(t, err)
	require.False(t, unsent, "a current refusal proves nothing about missing historical delivery")
}

func TestDispatchRestoreFailurePersistsBlockedDiagnostic(t *testing.T) {
	ctx, f, firmware, _, _ := newTransferFailureFixture(t)
	f.reconcile(t, ctx)
	before := f.state(t)
	_, err := f.conn.ExecContext(ctx, `CREATE FUNCTION reject_dispatch_restore() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NOT NEW.dispatch_started THEN RAISE EXCEPTION 'restore unavailable'; END IF; RETURN NEW; END $$;
CREATE TRIGGER reject_dispatch_restore BEFORE UPDATE ON mining_group_step_dispatch FOR EACH ROW EXECUTE FUNCTION reject_dispatch_restore()`)
	require.NoError(t, err)
	firmware.refuseMutation = true
	err = f.service.Reconcile(ctx, before.OrganizationID, before.DeviceID, f.target.DeviceIdentifier)
	require.ErrorContains(t, err, "restore unavailable")
	after := f.state(t)
	require.Equal(t, "blocked", after.Status)
	require.Contains(t, after.Message, "Cannot confirm request delivery")
	require.Equal(t, before.Step, after.Step)
	require.Equal(t, before.StepKey, after.StepKey)
	unsent, err := f.service.queries.MiningGroupStepIsUnsent(ctx, sqlc.MiningGroupStepIsUnsentParams{StepKey: before.StepKey.UUID, AttemptID: before.AttemptID.UUID})
	require.NoError(t, err)
	require.False(t, unsent)
}

type pausedRefusalMiner struct {
	*transferFailureMiner
	entered chan struct{}
	release chan struct{}
}

func (m *pausedRefusalMiner) GetMinerForCommand(context.Context, int64) (interfaces.Miner, error) {
	return m, nil
}

func (m *pausedRefusalMiner) MiningGroups(ctx context.Context, request sdk.MiningGroupRequest) (sdk.MiningGroupResult, error) {
	if request.ReadOnly() {
		return m.transferFailureMiner.MiningGroups(ctx, request)
	}
	close(m.entered)
	select {
	case <-m.release:
	case <-ctx.Done():
	}
	return sdk.MiningGroupResult{}, devicewrite.NotDispatched(errors.New("old invocation refused before send"))
}

func TestLostBindingConnectionCannotEraseNewerDispatch(t *testing.T) {
	ctx, f, firmware, _, _ := newTransferFailureFixture(t)
	f.reconcile(t, ctx)
	prepared := f.state(t)
	firmware.loseReply = true
	paused := &pausedRefusalMiner{transferFailureMiner: firmware, entered: make(chan struct{}), release: make(chan struct{})}
	oldWorker := NewService(f.conn, paused, f.service.pools, f.service.secrets)
	result, done := make(chan error, 1), make(chan struct{})
	release := sync.OnceFunc(func() { close(paused.release) })
	t.Cleanup(func() {
		release()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("old reconciliation did not finish")
		}
	})
	go func() {
		defer close(done)
		result <- oldWorker.Reconcile(ctx, prepared.OrganizationID, prepared.DeviceID, f.target.DeviceIdentifier)
	}()
	select {
	case <-paused.entered:
	case err := <-result:
		t.Fatalf("old worker stopped before dispatch: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("old worker did not reach dispatch")
	}

	// Drop the connection that owns the real journal/binding locks while its
	// adapter is still running. A replacement worker can now take those locks.
	var pid int
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT DISTINCT pid FROM pg_locks WHERE database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND relation='container_mining_group_state'::regclass AND mode='RowShareLock' AND granted AND pid<>pg_backend_pid()`).Scan(&pid))
	var terminated bool
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT pg_terminate_backend($1)`, pid).Scan(&terminated))
	require.True(t, terminated)
	newWorker := NewService(f.conn, firmware, f.service.pools, f.service.secrets)
	require.NoError(t, newWorker.Reconcile(ctx, prepared.OrganizationID, prepared.DeviceID, f.target.DeviceIdentifier))
	require.NotEmpty(t, firmware.receipts, "the newer worker must actually send the saved transfer")
	release()
	require.ErrorContains(t, <-result, "dispatch token changed")
	unsent, err := newWorker.queries.MiningGroupStepIsUnsent(ctx, sqlc.MiningGroupStepIsUnsentParams{StepKey: prepared.StepKey.UUID, AttemptID: prepared.AttemptID.UUID})
	require.NoError(t, err)
	require.False(t, unsent, "the old refusal must not erase the newer uncertain send")
	setMiningGroupPermissions(t, ctx, f, nil)
	firmware.calls = nil
	require.NoError(t, newWorker.Reconcile(ctx, prepared.OrganizationID, prepared.DeviceID, f.target.DeviceIdentifier))
	require.Equal(t, prepared.Step, f.state(t).Step)
	require.Empty(t, firmware.calls, "an unknown transfer must remain protected after authority changes")
}
