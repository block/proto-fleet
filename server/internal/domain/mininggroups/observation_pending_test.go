package mininggroups

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

type pendingObservationReply struct {
	observed sdk.MiningGroupResult
	err      error
}

type pendingObservationMiner struct {
	interfaces.Miner
	requests chan chan pendingObservationReply
}

func (m *pendingObservationMiner) GetMinerForCommand(context.Context, int64) (interfaces.Miner, error) {
	return m, nil
}

func (m *pendingObservationMiner) MiningGroups(ctx context.Context, request sdk.MiningGroupRequest) (sdk.MiningGroupResult, error) {
	if request.Action != "observe" {
		return sdk.MiningGroupResult{}, fmt.Errorf("unexpected mining-group action: %s", request.Action)
	}
	reply := make(chan pendingObservationReply, 1)
	select {
	case m.requests <- reply:
	case <-ctx.Done():
		return sdk.MiningGroupResult{}, fmt.Errorf("start observation: %w", ctx.Err())
	}
	select {
	case result := <-reply:
		return result.observed, result.err
	case <-ctx.Done():
		return sdk.MiningGroupResult{}, fmt.Errorf("wait for observation: %w", ctx.Err())
	}
}

type pendingObservationOutcome struct {
	state State
	err   error
}

func startPendingObservation(ctx context.Context, f retryFixture, path string) <-chan pendingObservationOutcome {
	done := make(chan pendingObservationOutcome, 1)
	go func() {
		if path == "dialog" {
			state, err := f.service.GetState(ctx, f.target.DeviceIdentifier)
			done <- pendingObservationOutcome{state: state, err: err}
			return
		}
		done <- pendingObservationOutcome{err: f.service.refreshAllocationReadiness(ctx, f.target.OrganizationID, f.target.DeviceID, f.target.DeviceIdentifier)}
	}()
	return done
}

func nextPendingObservation(t *testing.T, miner *pendingObservationMiner) chan pendingObservationReply {
	t.Helper()
	select {
	case reply := <-miner.requests:
		return reply
	case <-time.After(5 * time.Second):
		t.Fatal("controller observation did not start")
		return nil
	}
}

func finishPendingObservation(t *testing.T, done <-chan pendingObservationOutcome) pendingObservationOutcome {
	t.Helper()
	select {
	case result := <-done:
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("controller observation did not finish")
		return pendingObservationOutcome{}
	}
}

func readyObservation(revision int64) sdk.MiningGroupResult {
	return sdk.MiningGroupResult{Supported: true, Enabled: true, Revision: revision, MaxGroups: 289, ObservedAt: time.Now().UTC(), Groups: []sdk.MiningGroup{{ID: "default", Revision: revision}}}
}

type heldObservationPendingMarker struct {
	sqlc.Querier
	pause   atomic.Bool
	entered chan struct{}
	release chan struct{}
}

func (q *heldObservationPendingMarker) MarkMiningGroupObservationPending(ctx context.Context, params sqlc.MarkMiningGroupObservationPendingParams) error {
	if err := q.Querier.MarkMiningGroupObservationPending(ctx, params); err != nil {
		return err
	}
	if q.pause.CompareAndSwap(true, false) {
		close(q.entered)
		select {
		case <-q.release:
		case <-ctx.Done():
			return fmt.Errorf("wait after pending marker: %w", ctx.Err())
		}
	}
	return nil
}

func holdObservationPendingMarker(t *testing.T, f retryFixture) (*heldObservationPendingMarker, func()) {
	t.Helper()
	queries := &heldObservationPendingMarker{Querier: f.service.queries, entered: make(chan struct{}), release: make(chan struct{})}
	f.service.queries = queries
	release := sync.OnceFunc(func() { close(queries.release) })
	t.Cleanup(release)
	return queries, release
}

func waitForObservationPendingMarker(t *testing.T, q *heldObservationPendingMarker) {
	t.Helper()
	select {
	case <-q.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("observation pending marker did not commit")
	}
}

func TestObservationRevokesAllocationAuthorityBeforeControllerRead(t *testing.T) {
	for _, path := range []string{"dialog", "background"} {
		t.Run(path, func(t *testing.T) {
			ctx, f, cached := observationFailureFixture(t)
			miner := &pendingObservationMiner{requests: make(chan chan pendingObservationReply)}
			f.service.miners = miner
			done := startPendingObservation(ctx, f, path)
			reply := nextPendingObservation(t, miner)
			pending := f.state(t)
			require.False(t, pending.ObservedAt.Valid, "the pending marker must commit before controller I/O")
			require.Equal(t, cached.Observed, pending.Observed, "pending reads retain the last display snapshot")
			requireAllocationReadiness(t, ctx, f, false)
			reply <- pendingObservationReply{observed: readyObservation(2)}
			result := finishPendingObservation(t, done)
			require.NoError(t, result.err)
			require.True(t, f.state(t).ObservedAt.Valid)
			requireAllocationReadiness(t, ctx, f, true)
		})
	}
}

func TestFailedObservationKeepsAllocationBlockedAfterReadAndCommitFailure(t *testing.T) {
	for _, path := range []string{"dialog", "background"} {
		for _, failure := range []string{"read", "commit"} {
			t.Run(path+"/"+failure, func(t *testing.T) {
				ctx, f, cached := observationFailureFixture(t)
				if failure == "commit" {
					_, err := f.conn.ExecContext(ctx, `CREATE FUNCTION reject_fresh_observation_commit() RETURNS trigger LANGUAGE plpgsql AS $$
					 BEGIN RAISE EXCEPTION 'fixture observation commit failure'; END $$;
					 CREATE CONSTRAINT TRIGGER reject_fresh_observation_commit AFTER UPDATE ON container_mining_group_state
					 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.observed_at IS NOT NULL)
					 EXECUTE FUNCTION reject_fresh_observation_commit()`)
					require.NoError(t, err)
				}
				miner := &pendingObservationMiner{requests: make(chan chan pendingObservationReply)}
				f.service.miners = miner
				done := startPendingObservation(ctx, f, path)
				reply := nextPendingObservation(t, miner)
				requireAllocationReadiness(t, ctx, f, false)
				if failure == "read" {
					reply <- pendingObservationReply{err: errors.New("controller unavailable")}
				} else {
					reply <- pendingObservationReply{observed: readyObservation(2)}
				}
				result := finishPendingObservation(t, done)
				after := f.state(t)
				require.False(t, after.ObservedAt.Valid)
				require.Equal(t, cached.Observed, after.Observed)
				requireAllocationReadiness(t, ctx, f, false)
				if failure == "read" && path == "background" {
					require.ErrorContains(t, result.err, "controller unavailable")
				}
			})
		}
	}
}

func TestConcurrentObservationsKeepPendingAndNewerSuccess(t *testing.T) {
	ctx, f, _ := observationFailureFixture(t)
	queries, releaseMarker := holdObservationPendingMarker(t, f)
	queries.pause.Store(true)
	miner := &pendingObservationMiner{requests: make(chan chan pendingObservationReply)}
	f.service.miners = miner
	first := startPendingObservation(ctx, f, "dialog")
	waitForObservationPendingMarker(t, queries)
	requireAllocationReadiness(t, ctx, f, false)
	second := startPendingObservation(ctx, f, "background")
	require.Eventually(t, func() bool {
		var waiting bool
		err := f.conn.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
		 WHERE datname=current_database() AND wait_event_type='Lock'
		 AND query LIKE '%SELECT discovered_device_id FROM device%FOR NO KEY UPDATE%')`).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond, "the second observation must wait at the binding before marking pending")
	select {
	case <-miner.requests:
		t.Fatal("an overlapping observation reached the controller before the first read acquired its journal lock")
	default:
	}
	releaseMarker()
	firstReply := nextPendingObservation(t, miner)
	firstReply <- pendingObservationReply{observed: readyObservation(2)}
	require.NoError(t, finishPendingObservation(t, first).err)
	secondReply := nextPendingObservation(t, miner)
	pending := f.state(t)
	require.False(t, pending.ObservedAt.Valid, "the first success cannot authorize allocation during the next read")
	requireAllocationReadiness(t, ctx, f, false)
	secondReply <- pendingObservationReply{err: errors.New("second observation failed")}
	require.ErrorContains(t, finishPendingObservation(t, second).err, "second observation failed")
	require.Equal(t, pending.Observed, f.state(t).Observed)
	requireAllocationReadiness(t, ctx, f, false)
	third := startPendingObservation(ctx, f, "dialog")
	thirdReply := nextPendingObservation(t, miner)
	thirdReply <- pendingObservationReply{observed: readyObservation(3)}
	require.NoError(t, finishPendingObservation(t, third).err)
	fresh := f.state(t)
	require.True(t, fresh.ObservedAt.Valid)
	require.NotEqual(t, pending.Observed, fresh.Observed)
	after := f.state(t)
	require.Equal(t, fresh.ObservedAt, after.ObservedAt)
	require.Equal(t, fresh.Observed, after.Observed)
	requireAllocationReadiness(t, ctx, f, true)
}
