package rollout

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	commandpb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
	"github.com/stretchr/testify/require"
)

func TestStaleTickCannotDispatchAfterOperatorControl(t *testing.T) {
	for _, action := range []string{"pause", "cancel"} {
		t.Run(action, func(t *testing.T) {
			f := newFixture(t, 1)
			f.channel(t, allAtOnce, "miner-0")
			started := f.apply(t, "fw-2")
			ctx := t.Context()
			stale, targets := preparedDispatch(t, ctx, f, started.ID)

			require.NoError(t, controlRollout(ctx, f, started.ID, action))
			require.NoError(t, f.svc.enforceRollout(ctx, stale, "Test channel", targets))
			require.Empty(t, f.dispatcher.sentIdentifiers(), "a tick prepared before the operator action must not dispatch afterward")
			rows, err := f.svc.store.GetQueries(ctx).ListFirmwareRolloutDevices(ctx, started.ID)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			require.Zero(t, rows[0].Attempts)
			require.False(t, rows[0].LastSentAt.Valid)
		})
	}
}

func TestOperatorControlWaitsForDispatchToFinish(t *testing.T) {
	for _, action := range []string{"pause", "cancel"} {
		t.Run(action, func(t *testing.T) {
			f := newFixture(t, 1)
			f.channel(t, allAtOnce, "miner-0")
			started := f.apply(t, "fw-2")
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			stale, targets := preparedDispatch(t, ctx, f, started.ID)
			dispatcher, release := blockRolloutDispatch(f)
			var workers sync.WaitGroup
			t.Cleanup(func() {
				cancel()
				release()
				workers.Wait()
			})
			dispatchDone := startDispatchWorker(&workers, func() error {
				return f.svc.enforceRollout(ctx, stale, "Test channel", targets)
			})
			select {
			case <-dispatcher.entered:
			case <-ctx.Done():
				t.Fatal("tick did not reach dispatch")
			}

			controlDone := startDispatchWorker(&workers, func() error {
				return controlRollout(ctx, f, started.ID, action)
			})
			waitForDispatchBlock(t, ctx, f, controlDone)
			release()
			require.NoError(t, <-dispatchDone)
			require.NoError(t, <-controlDone)
			require.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers())

			// Even the pre-control snapshot cannot send another command once
			// the action commits, including after the usual resend interval.
			f.backdateSends(t)
			require.NoError(t, f.svc.enforceRollout(ctx, stale, "Test channel", targets))
			require.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers())
			current := f.rollout(t, started.ID)
			if action == "pause" {
				require.NotNil(t, current.PausedAt)
			} else {
				require.Equal(t, StatusCanceled, current.Status)
			}
		})
	}
}

func TestConcurrentPreparedTicksDoNotDispatchTwice(t *testing.T) {
	f := newFixture(t, 1)
	f.channel(t, allAtOnce, "miner-0")
	started := f.apply(t, "fw-2")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	stale, targets := preparedDispatch(t, ctx, f, started.ID)
	dispatcher, release := blockRolloutDispatch(f)
	var workers sync.WaitGroup
	t.Cleanup(func() {
		cancel()
		release()
		workers.Wait()
	})
	enforce := func() error { return f.svc.enforceRollout(ctx, stale, "Test channel", targets) }
	firstDone := startDispatchWorker(&workers, enforce)
	select {
	case <-dispatcher.entered:
	case <-ctx.Done():
		t.Fatal("first tick did not reach dispatch")
	}
	secondDone := startDispatchWorker(&workers, enforce)
	waitForDispatchBlock(t, ctx, f, secondDone)
	release()
	require.NoError(t, <-firstDone)
	require.NoError(t, <-secondDone)
	require.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers(), "the second tick must reread the first tick's committed attempt")
	rows, err := f.svc.store.GetQueries(ctx).ListFirmwareRolloutDevices(ctx, started.ID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, int32(1), rows[0].Attempts)
}

func TestDispatchIsNotReplayedAfterRetryableDatabaseFailure(t *testing.T) {
	f := newFixture(t, 1)
	f.channel(t, allAtOnce, "miner-0")
	started := f.apply(t, "fw-2")
	ctx := t.Context()
	row, targets := preparedDispatch(t, ctx, f, started.ID)
	_, err := f.conn.ExecContext(ctx, `
		CREATE FUNCTION fail_after_firmware_dispatch() RETURNS trigger AS $$
		BEGIN
			RAISE EXCEPTION 'injected failure after external dispatch' USING ERRCODE = '40001';
		END;
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER fail_after_firmware_dispatch BEFORE UPDATE ON firmware_rollout_device
		FOR EACH ROW WHEN (NEW.attempts > OLD.attempts)
		EXECUTE FUNCTION fail_after_firmware_dispatch();`)
	require.NoError(t, err)

	err = f.svc.enforceRollout(ctx, row, "Test channel", targets)
	require.ErrorContains(t, err, "injected failure after external dispatch")
	require.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers(), "a retryable database error must not replay the external command")
	rows, err := f.svc.store.GetQueries(ctx).ListFirmwareRolloutDevices(ctx, started.ID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Zero(t, rows[0].Attempts, "the failed transaction rolls back the attempt bookkeeping")
}

func TestDispatchRejectsAnOuterRetryableTransaction(t *testing.T) {
	f := newFixture(t, 1)
	f.channel(t, allAtOnce, "miner-0")
	started := f.apply(t, "fw-2")
	ctx := t.Context()
	row, targets := preparedDispatch(t, ctx, f, started.ID)

	err := f.svc.tx.RunInTx(ctx, func(ctx context.Context) error {
		return f.svc.enforceRollout(ctx, row, "Test channel", targets)
	})
	require.ErrorContains(t, err, "non-retryable transaction cannot be nested")
	require.Empty(t, f.dispatcher.sentIdentifiers(), "an outer transaction must not be able to retry external dispatch")
}

func preparedDispatch(t *testing.T, ctx context.Context, f *fixture, rolloutID int64) (sqlc.FirmwareRollout, []target) {
	t.Helper()
	row, err := f.svc.store.GetQueries(ctx).GetFirmwareRollout(ctx, sqlc.GetFirmwareRolloutParams{RolloutID: rolloutID, OrgID: f.orgID})
	require.NoError(t, err)
	targets, err := f.svc.prepareRollout(ctx, row)
	require.NoError(t, err)
	require.Len(t, targets, 1)
	return row, targets
}

func controlRollout(ctx context.Context, f *fixture, rolloutID int64, action string) error {
	if action == "pause" {
		_, err := f.svc.PauseRollout(ctx, f.orgID, rolloutID, byOperator)
		return err
	}
	_, _, err := f.svc.CancelRollout(ctx, f.orgID, rolloutID, byOperator)
	return err
}

type blockingRolloutDispatcher struct {
	delegate *fakeDispatcher
	entered  chan struct{}
	release  <-chan struct{}
	mu       sync.Mutex
}

func blockRolloutDispatch(f *fixture) (*blockingRolloutDispatcher, func()) {
	release := make(chan struct{})
	d := &blockingRolloutDispatcher{delegate: f.dispatcher, entered: make(chan struct{}, 2), release: release}
	f.svc.commands = d
	// The fixture's activity recorder is intentionally not concurrency safe.
	f.svc.activity = nil
	return d, sync.OnceFunc(func() { close(release) })
}

func (d *blockingRolloutDispatcher) FirmwareUpdateArtifact(ctx context.Context, selector *commandpb.DeviceSelector, checksum string, metadata files.FirmwareMetadata) (*command.CommandResult, error) {
	d.entered <- struct{}{}
	select {
	case <-d.release:
	case <-ctx.Done():
		return nil, fmt.Errorf("waiting for dispatch release: %w", ctx.Err())
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.delegate.FirmwareUpdateArtifact(ctx, selector, checksum, metadata)
}

func startDispatchWorker(workers *sync.WaitGroup, run func() error) <-chan error {
	done := make(chan error, 1)
	workers.Go(func() { done <- run() })
	return done
}

// The dispatcher holds the first tick at its external call. Observing a
// PostgreSQL waiter proves the competing action reached the held row lock;
// an arbitrary delay would not establish the ordering under test.
func waitForDispatchBlock(t *testing.T, ctx context.Context, f *fixture, done <-chan error) {
	t.Helper()
	var completed, blocked bool
	var actionErr error
	require.Eventually(t, func() bool {
		select {
		case actionErr = <-done:
			completed = true
			return true
		default:
		}
		var count int
		err := f.conn.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database() AND cardinality(pg_blocking_pids(pid)) > 0`).Scan(&count)
		blocked = err == nil && count > 0
		return blocked
	}, 3*time.Second, 10*time.Millisecond, "competing action did not reach the dispatch lock")
	require.False(t, completed, "competing action completed before dispatch finished: %v", actionErr)
	require.True(t, blocked)
}
