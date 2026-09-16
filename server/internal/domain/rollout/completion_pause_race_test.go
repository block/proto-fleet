package rollout

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/stretchr/testify/require"
)

func TestStaleCompletionPreservesCommittedPause(t *testing.T) {
	for _, failed := range []bool{false, true} {
		status, event := StatusCompleted, EventRolloutCompleted
		if failed {
			status, event = StatusCompletedWithFailures, EventRolloutCompletedWithFailures
		}
		t.Run(status, func(t *testing.T) {
			f := newFixture(t, 1)
			ctx := t.Context()
			f.channel(t, allAtOnce, "miner-0")
			started := f.apply(t, "fw-2")
			if failed {
				require.NoError(t, f.svc.store.GetQueries(ctx).HaltFirmwareRolloutDevices(ctx, sqlc.HaltFirmwareRolloutDevicesParams{
					RolloutID: started.ID, DeviceIds: []int64{f.deviceIDs["miner-0"]},
					HaltReason: HaltReasonFailed, LastError: "Update attempts exhausted",
				}))
			} else {
				f.svc.EnforceTick(ctx)
				f.finishUpdate(t, "miner-0", "2.0.0")
			}
			stale, targets := preparedDispatch(t, ctx, f, started.ID)
			require.True(t, allSettled(reviewScope(stale, targets), stale))
			require.False(t, stale.PausedAt.Valid)

			// The operator's pause commits after this tick loaded its settled
			// targets, before the tick attempts the final completion write.
			paused, err := f.svc.PauseRollout(ctx, f.orgID, started.ID, byOperator)
			require.NoError(t, err)
			require.NotNil(t, paused.PausedAt)
			require.NoError(t, f.svc.enforceRollout(ctx, stale, "Test channel", targets))
			current := f.rollout(t, started.ID)
			require.Equal(t, StatusActive, current.Status)
			require.Equal(t, paused.PausedAt, current.PausedAt)
			require.Equal(t, paused.Revision, current.Revision)
			require.Nil(t, current.FinishedAt)
			require.NotContains(t, f.activity.types(), event, "a rejected stale completion must not emit a completion event")

			_, err = f.svc.ResumeRollout(ctx, f.orgID, started.ID, Mutation{Actor: testActor, ExpectedRevision: paused.Revision})
			require.NoError(t, err, "the committed pause must remain resumable at its returned revision")
			f.svc.EnforceTick(ctx)
			current = f.rollout(t, started.ID)
			require.Equal(t, status, current.Status)
			require.Nil(t, current.PausedAt)
			require.NotNil(t, current.FinishedAt)
			require.Contains(t, f.activity.types(), event)
		})
	}
}

func TestCompletionWaitingForPausePreservesPause(t *testing.T) {
	f := newFixture(t, 1)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	f.channel(t, allAtOnce, "miner-0")
	started := f.apply(t, "fw-2")
	f.svc.EnforceTick(ctx)
	f.finishUpdate(t, "miner-0", "2.0.0")
	stale, targets := preparedDispatch(t, ctx, f, started.ID)
	require.True(t, allSettled(reviewScope(stale, targets), stale))
	f.svc.activity = nil // The fixture's event recorder is not concurrency safe.

	paused := make(chan struct{})
	commitPause := make(chan struct{})
	release := sync.OnceFunc(func() { close(commitPause) })
	var workers sync.WaitGroup
	t.Cleanup(func() {
		cancel()
		release()
		workers.Wait()
	})
	pauseDone := startDispatchWorker(&workers, func() error {
		return f.svc.tx.RunInTx(ctx, func(txCtx context.Context) error {
			if _, err := f.svc.PauseRollout(txCtx, f.orgID, started.ID, byOperator); err != nil {
				return err
			}
			close(paused)
			select {
			case <-commitPause:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	})
	select {
	case <-paused:
	case <-ctx.Done():
		t.Fatal("pause did not acquire the rollout lock")
	}
	finishDone := startDispatchWorker(&workers, func() error {
		return f.svc.enforceRollout(ctx, stale, "Test channel", targets)
	})
	waitForDispatchBlock(t, ctx, f, finishDone)
	release()
	require.NoError(t, <-pauseDone)
	require.NoError(t, <-finishDone)
	current := f.rollout(t, started.ID)
	require.Equal(t, StatusActive, current.Status, "completion must recheck the pause after waiting for its row lock")
	require.NotNil(t, current.PausedAt)
	require.Nil(t, current.FinishedAt)
}
