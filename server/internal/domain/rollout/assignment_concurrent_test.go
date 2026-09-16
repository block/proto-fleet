package rollout

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConcurrentApplyFirmwareUsesCommittedAssignment(t *testing.T) {
	for _, tc := range []struct {
		name, initial, first, second string
		wantChecksum, wantPrevious   string
		wantGeneration               int64
		wantStarted                  int
	}{
		{
			name: "new pair preserves the immediately preceding assignment", first: "fw-1", second: "fw-2",
			wantChecksum: checksum2, wantPrevious: checksum1, wantGeneration: 2, wantStarted: 1,
		},
		{
			name: "reapplying the original checksum restores it", initial: "fw-1", first: "fw-2", second: "fw-1",
			wantChecksum: checksum1, wantPrevious: checksum2, wantGeneration: 3, wantStarted: 1,
		},
		{
			name: "identical assignments remain a no-op", first: "fw-1", second: "fw-1",
			wantChecksum: checksum1, wantGeneration: 1,
		},
		{
			name: "clear observes the preceding first assignment", first: "fw-1",
			wantGeneration: 2,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, 1)
			f.channel(t, allAtOnce, f.allMiners()...)
			if tc.initial != "" {
				f.apply(t, tc.initial)
			}
			// Activity is incidental to this race, and the fixture's logger
			// intentionally does not support concurrent writes.
			f.svc.activity = nil
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			apply := func(fileID string) func(Actor) ([]Rollout, error) {
				return func(actor Actor) ([]Rollout, error) {
					return f.svc.ApplyFirmware(ctx, f.orgID, actor, f.channelID, []Assignment{rigAssignment(fileID)}, nil)
				}
			}
			first, second := concurrentAssignmentWrites(t, ctx, f, apply(tc.first), apply(tc.second))
			require.NoError(t, first.err)
			require.NoError(t, second.err)
			require.Len(t, first.rollouts, 1)
			require.Len(t, second.rollouts, tc.wantStarted)

			assignment, err := f.svc.store.GetQueries(ctx).GetReleaseChannelFirmware(ctx, sqlc.GetReleaseChannelFirmwareParams{
				ChannelID: f.channelID, Manufacturer: "Proto", Model: "Rig",
			})
			require.NoError(t, err)
			assert.Equal(t, tc.wantChecksum, assignment.FirmwareChecksum)
			assert.Equal(t, tc.wantGeneration, assignment.AssignmentGeneration)
			if tc.wantStarted > 0 {
				assert.Equal(t, tc.wantPrevious, second.rollouts[0].PreviousFirmwareChecksum)
				assert.Equal(t, fakeArtifacts[tc.first].Metadata.FirmwareVersion, second.rollouts[0].PreviousFirmwareVersion)
				assert.Equal(t, CancelReasonSuperseded, f.rollout(t, first.rollouts[0].ID).CancelReason)
				_, rolledBack, err := f.svc.RollbackFirmware(ctx, f.orgID, second.rollouts[0].ID, byOperator)
				require.NoError(t, err)
				require.Len(t, rolledBack, 1)
				assert.Equal(t, tc.wantPrevious, rolledBack[0].FirmwareChecksum)
			}
		})
	}
}

func TestConcurrentApplyFirmwareObservesRollback(t *testing.T) {
	f := newFixture(t, 1)
	f.channel(t, allAtOnce, f.allMiners()...)
	f.apply(t, "fw-1")
	latest := f.apply(t, "fw-2")
	f.svc.activity = nil
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	rollback := func(actor Actor) ([]Rollout, error) {
		_, started, err := f.svc.RollbackFirmware(ctx, f.orgID, latest.ID, Mutation{Actor: actor})
		return started, err
	}
	apply := func(actor Actor) ([]Rollout, error) {
		return f.svc.ApplyFirmware(ctx, f.orgID, actor, f.channelID, []Assignment{rigAssignment("fw-2")}, nil)
	}
	first, second := concurrentAssignmentWrites(t, ctx, f, rollback, apply)
	require.NoError(t, first.err)
	require.NoError(t, second.err)
	require.Len(t, first.rollouts, 1)
	require.Len(t, second.rollouts, 1, "apply must restore fw-2 after the concurrent rollback to fw-1")
	assert.Equal(t, checksum1, first.rollouts[0].FirmwareChecksum)
	assert.Equal(t, checksum2, second.rollouts[0].FirmwareChecksum)
	assert.Equal(t, checksum1, second.rollouts[0].PreviousFirmwareChecksum)
	assert.Equal(t, int64(4), second.rollouts[0].AssignmentGeneration)
	assert.Equal(t, CancelReasonSuperseded, f.rollout(t, first.rollouts[0].ID).CancelReason)
	assert.Equal(t, "fw-2", f.assignedFirmware(t))
}

func TestConcurrentRollbackRechecksAfterApply(t *testing.T) {
	for _, checkRevision := range []bool{false, true} {
		name, wantReason := "assignment generation", ReasonStaleGeneration
		if checkRevision {
			name, wantReason = "expected revision", ReasonStaleRevision
		}
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, 1)
			f.channel(t, allAtOnce, f.allMiners()...)
			f.apply(t, "fw-1")
			latest := f.apply(t, "fw-2")
			f.svc.activity = nil
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			apply := func(actor Actor) ([]Rollout, error) {
				return f.svc.ApplyFirmware(ctx, f.orgID, actor, f.channelID, []Assignment{rigAssignment("fw-1")}, nil)
			}
			rollback := func(actor Actor) ([]Rollout, error) {
				mutation := Mutation{Actor: actor}
				if checkRevision {
					mutation.ExpectedRevision = latest.Revision
				}
				_, started, err := f.svc.RollbackFirmware(ctx, f.orgID, latest.ID, mutation)
				return started, err
			}
			first, second := concurrentAssignmentWrites(t, ctx, f, apply, rollback)
			require.NoError(t, first.err)
			require.Len(t, first.rollouts, 1)
			require.Error(t, second.err)
			info, ok := ReasonOf(second.err)
			require.True(t, ok)
			assert.Equal(t, wantReason, info.Reason)
			assert.Empty(t, second.rollouts)
			assert.Equal(t, "fw-1", f.assignedFirmware(t))
			assert.Equal(t, int64(3), f.rigGroup(t).AssignmentGeneration)
		})
	}
}

type assignmentWriteResult struct {
	rollouts []Rollout
	err      error
}

// Hold the first mutation immediately before its assignment write. The second
// mutation either waits for that channel transaction or, without serialization,
// reads stale state and reaches its own blocked write (or premature no-op).
// Separate test-owned locks then commit the first write before the second.
func concurrentAssignmentWrites(t *testing.T, ctx context.Context, f *fixture, firstWrite, secondWrite func(Actor) ([]Rollout, error)) (assignmentWriteResult, assignmentWriteResult) {
	t.Helper()
	_, err := f.conn.ExecContext(ctx, `
		CREATE FUNCTION wait_before_assignment_write() RETURNS trigger AS $$
		BEGIN
			IF NEW.assigned_by IN (101, 102) THEN
				PERFORM pg_advisory_xact_lock(1017, NEW.assigned_by::integer);
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER wait_before_assignment_write BEFORE INSERT OR UPDATE ON release_channel_firmware
		FOR EACH ROW EXECUTE FUNCTION wait_before_assignment_write();`)
	require.NoError(t, err)
	block := func(actorID int) *sql.Tx {
		t.Helper()
		tx, err := f.conn.BeginTx(ctx, nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = tx.Rollback() })
		_, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(1017, $1)`, actorID)
		require.NoError(t, err)
		return tx
	}
	firstBlocker, secondBlocker := block(101), block(102)
	start := func(write func(Actor) ([]Rollout, error), actorID int64) <-chan assignmentWriteResult {
		done := make(chan assignmentWriteResult, 1)
		go func() {
			actor := testActor
			actor.ID = actorID
			rollouts, err := write(actor)
			done <- assignmentWriteResult{rollouts: rollouts, err: err}
		}()
		return done
	}
	blockedCount := func() int {
		var blocked int
		err := f.conn.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database() AND cardinality(pg_blocking_pids(pid)) > 0`).Scan(&blocked)
		if err != nil {
			return 0
		}
		return blocked
	}
	firstDone := start(firstWrite, 101)
	require.Eventually(t, func() bool { return blockedCount() == 1 }, 3*time.Second, 10*time.Millisecond,
		"first mutation must reach its assignment write")
	secondDone := start(secondWrite, 102)
	var second *assignmentWriteResult
	require.Eventually(t, func() bool {
		select {
		case result := <-secondDone:
			second = &result
			return true
		default:
			return blockedCount() == 2
		}
	}, 3*time.Second, 10*time.Millisecond, "second mutation must reach its lock, write, or no-op")
	require.NoError(t, firstBlocker.Rollback())
	first := <-firstDone
	require.NoError(t, secondBlocker.Rollback())
	if second == nil {
		result := <-secondDone
		second = &result
	}
	return first, *second
}
