package rollout

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
)

func TestPausePreservesRemainingStageTime(t *testing.T) {
	for _, autoContinue := range []bool{false, true} {
		name := "between batches"
		behavior := Behavior{Method: MethodBatched, BatchSize: 1, WaitBetweenBatchesSeconds: 120}
		stage := StageWaiting
		if autoContinue {
			name = "telemetry stabilization"
			behavior = Behavior{Method: MethodBatched, BatchSize: 1, ReviewAfterEachBatch: true, AutoContinue: true, StabilizationSeconds: 120}
			stage = StageAwaitingReview
		}
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, 2)
			ctx := t.Context()
			f.channel(t, behavior, f.allMiners()...)
			started := f.apply(t, "fw-2")
			f.svc.EnforceTick(ctx)
			f.finishUpdate(t, "miner-0", "2.0.0")
			f.svc.EnforceTick(ctx)
			require.Equal(t, stage, f.rollout(t, started.ID).Stage)

			// Model two maintenance windows without sleeping or advancing only
			// the service clock: resume measures each interval in the database.
			for _, elapsed := range []struct{ active, paused time.Duration }{
				{30 * time.Second, 5 * time.Minute},
				{20 * time.Second, 10 * time.Minute},
			} {
				_, err := f.svc.PauseRollout(ctx, f.orgID, started.ID, byOperator)
				require.NoError(t, err)
				_, err = f.conn.ExecContext(ctx, `
					UPDATE firmware_rollout
					SET stage_changed_at = stage_changed_at - $2 * INTERVAL '1 microsecond',
					    paused_at = paused_at - $3 * INTERVAL '1 microsecond'
					WHERE id = $1
				`, started.ID, (elapsed.active + elapsed.paused).Microseconds(), elapsed.paused.Microseconds())
				require.NoError(t, err)
				before := f.rollout(t, started.ID)
				_, err = f.svc.ResumeRollout(ctx, f.orgID, started.ID, byOperator)
				require.NoError(t, err)
				require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&f.clock))
				resumed := f.rollout(t, started.ID)
				require.True(t, before.StageChangedAt.Equal(resumed.StageChangedAt), "resume preserves the historical stage timestamp")
				require.Equal(t, before.CreatedAt, resumed.CreatedAt)
				require.NotNil(t, before.Devices[0].LastSentAt)
				require.Equal(t, before.Devices[0].LastSentAt, resumed.Devices[0].LastSentAt, "in-flight commands keep their actual dispatch time")
				f.svc.EnforceTick(ctx)
				require.Equal(t, stage, f.rollout(t, started.ID).Stage, "paused time must not exhaust the stage timer")
			}
			if autoContinue {
				require.InDelta(t, 70, f.rollout(t, started.ID).Evidence.StabilizationRemainingSeconds, 1)
			}
			f.advanceClock(60 * time.Second)
			f.svc.EnforceTick(ctx)
			require.Equal(t, stage, f.rollout(t, started.ID).Stage)
			f.advanceClock(11 * time.Second)
			f.svc.EnforceTick(ctx)
			next := f.rollout(t, started.ID)
			require.Equal(t, StageBatch, next.Stage)
			require.Equal(t, int32(1), next.CurrentBatch)
		})
	}
}

func TestStaleStageTimerCannotAdvanceAfterResume(t *testing.T) {
	f := newFixture(t, 2)
	ctx := t.Context()
	f.channel(t, Behavior{Method: MethodBatched, BatchSize: 1, WaitBetweenBatchesSeconds: 120}, f.allMiners()...)
	started := f.apply(t, "fw-2")
	f.svc.EnforceTick(ctx)
	f.finishUpdate(t, "miner-0", "2.0.0")
	f.svc.EnforceTick(ctx)
	_, err := f.conn.ExecContext(ctx, `UPDATE firmware_rollout SET stage_changed_at = clock_timestamp() - INTERVAL '330 seconds' WHERE id = $1`, started.ID)
	require.NoError(t, err)
	q := sqlc.New(f.conn)
	stale, err := q.GetFirmwareRollout(ctx, sqlc.GetFirmwareRolloutParams{RolloutID: started.ID, OrgID: f.orgID})
	require.NoError(t, err)
	_, err = f.svc.PauseRollout(ctx, f.orgID, started.ID, byOperator)
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `UPDATE firmware_rollout SET paused_at = clock_timestamp() - INTERVAL '300 seconds' WHERE id = $1`, started.ID)
	require.NoError(t, err)
	_, err = f.svc.ResumeRollout(ctx, f.orgID, started.ID, byOperator)
	require.NoError(t, err)
	targets, err := f.svc.listTargets(ctx, stale)
	require.NoError(t, err)
	// This models an enforcement tick whose active-row snapshot predates
	// the pause and resume; its apparently elapsed timer is now obsolete.
	require.ErrorContains(t, f.svc.enforceRollout(ctx, stale, "", targets), "changed or was paused")
	require.Equal(t, StageWaiting, f.rollout(t, started.ID).Stage)
	var onDone bool
	require.ErrorContains(t, f.svc.transition(ctx, &stale, StageWaiting, StageBatch, func() { onDone = true }), "changed or was paused")
	require.False(t, onDone, "a stale transition cannot record a successful stage change")
	require.Equal(t, StageWaiting, f.rollout(t, started.ID).Stage)
}

func TestStageTransitionsResetPausedTime(t *testing.T) {
	for _, manual := range []bool{false, true} {
		name := "transition"
		if manual {
			name = "advance"
		}
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, 2)
			ctx := t.Context()
			f.channel(t, batchesOf2, f.allMiners()...)
			started := f.apply(t, "fw-2")
			_, err := f.conn.ExecContext(ctx, `
				UPDATE firmware_rollout
				SET stage = 'awaiting_review', stage_paused_microseconds = 300000000,
				    stage_changed_at = clock_timestamp() - INTERVAL '7 minutes'
				WHERE id = $1
			`, started.ID)
			require.NoError(t, err)
			q := sqlc.New(f.conn)
			row, err := q.GetFirmwareRollout(ctx, sqlc.GetFirmwareRolloutParams{RolloutID: started.ID, OrgID: f.orgID})
			require.NoError(t, err)
			f.advanceClock(time.Hour)
			if manual {
				require.NoError(t, f.svc.advance(ctx, &row, StageAwaitingReview, &byOperator))
			} else {
				require.NoError(t, f.svc.transition(ctx, &row, StageAwaitingReview, StageBatch, nil))
			}
			persisted, err := q.GetFirmwareRollout(ctx, sqlc.GetFirmwareRolloutParams{RolloutID: started.ID, OrgID: f.orgID})
			require.NoError(t, err)
			require.Zero(t, row.StagePausedMicroseconds)
			require.Zero(t, persisted.StagePausedMicroseconds)
			require.Equal(t, persisted.StageChangedAt, row.StageChangedAt, "later decisions use the database's actual stage timestamp")
		})
	}
}

func TestContinuePausedRolloutRequiresResume(t *testing.T) {
	f := newFixture(t, 2)
	ctx := t.Context()
	f.channel(t, pilotOf1, f.allMiners()...)
	started := f.apply(t, "fw-2")
	f.svc.EnforceTick(ctx)
	f.finishUpdate(t, "miner-0", "2.0.0")
	f.svc.EnforceTick(ctx)
	paused, err := f.svc.PauseRollout(ctx, f.orgID, started.ID, byOperator)
	require.NoError(t, err)
	_, err = f.svc.ContinueRollout(ctx, f.orgID, started.ID, byOperator)
	require.Error(t, err)
	info, ok := ReasonOf(err)
	require.True(t, ok)
	require.Equal(t, ReasonPaused, info.Reason)
	unchanged := f.rollout(t, started.ID)
	require.Equal(t, paused.Revision, unchanged.Revision)
	require.Equal(t, paused.Stage, unchanged.Stage)
}
