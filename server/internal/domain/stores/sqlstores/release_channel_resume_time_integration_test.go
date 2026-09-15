package sqlstores_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
)

func TestReleaseChannelQueries_ResumeAccumulatesStagePause(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	f := newReleaseChannelQueryFixture(t)
	rollout := f.rollout(f.channel("resume-timer"), "Bitmain", "S19")
	f.exec(`UPDATE firmware_rollout SET created_at = clock_timestamp() - INTERVAL '1 hour',
		stage_changed_at = clock_timestamp() - INTERVAL '1 hour' WHERE id = $1`, rollout)
	device := f.device("resume-target", "Bitmain", "S19", "v1")
	require.NoError(t, f.q.AppendFirmwareRolloutDevices(t.Context(), sqlc.AppendFirmwareRolloutDevicesParams{
		RolloutID: rollout, DeviceIds: []int64{device.id},
	}))
	f.exec(`UPDATE firmware_rollout_device SET baseline_at = clock_timestamp() - INTERVAL '30 minutes',
		first_sent_at = clock_timestamp() - INTERVAL '20 minutes', last_sent_at = clock_timestamp() - INTERVAL '10 minutes',
		last_dispatched_at = clock_timestamp() - INTERVAL '10 minutes', verified_at = clock_timestamp() - INTERVAL '5 minutes'
		WHERE rollout_id = $1`, rollout)
	var targetBefore string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT to_jsonb(target)::text FROM firmware_rollout_device target WHERE rollout_id = $1`, rollout).Scan(&targetBefore))
	params := sqlc.ResumeFirmwareRolloutParams{RolloutID: rollout, ActorType: "user", ActorID: 42, ActorName: "resuming operator"}
	for range 2 {
		rows, err := f.q.PauseFirmwareRollout(t.Context(), sqlc.PauseFirmwareRolloutParams{RolloutID: rollout, ActorType: "system"})
		require.NoError(t, err)
		require.Equal(t, int64(1), rows)
		f.exec(`UPDATE firmware_rollout SET paused_at = clock_timestamp() - INTERVAL '10 seconds' WHERE id = $1`, rollout)
		before, err := f.q.GetFirmwareRollout(t.Context(), sqlc.GetFirmwareRolloutParams{RolloutID: rollout, OrgID: f.org})
		require.NoError(t, err)
		var beforeResume, afterResume time.Time
		require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT clock_timestamp()`).Scan(&beforeResume))
		rows, err = f.q.ResumeFirmwareRollout(t.Context(), params)
		require.NoError(t, err)
		require.Equal(t, int64(1), rows)
		require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT clock_timestamp()`).Scan(&afterResume))
		after, err := f.q.GetFirmwareRollout(t.Context(), sqlc.GetFirmwareRolloutParams{RolloutID: rollout, OrgID: f.org})
		require.NoError(t, err)
		require.False(t, after.PausedAt.Valid)
		added := after.StagePausedMicroseconds - before.StagePausedMicroseconds
		require.GreaterOrEqual(t, added, beforeResume.Sub(before.PausedAt.Time).Microseconds())
		require.LessOrEqual(t, added, afterResume.Sub(before.PausedAt.Time).Microseconds())
		require.Equal(t, before.StageChangedAt, after.StageChangedAt, "resuming preserves the stage event time")
		require.Equal(t, before.Revision+1, after.Revision)
		require.Equal(t, params.ActorName, after.LastActionByName)
		rows, err = f.q.ResumeFirmwareRollout(t.Context(), sqlc.ResumeFirmwareRolloutParams{RolloutID: rollout, ActorType: "system"})
		require.NoError(t, err)
		require.Zero(t, rows)
		unchanged, err := f.q.GetFirmwareRollout(t.Context(), sqlc.GetFirmwareRolloutParams{RolloutID: rollout, OrgID: f.org})
		require.NoError(t, err)
		require.Equal(t, after, unchanged, "a repeated resume cannot count the pause again")
	}
	var targetAfter string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT to_jsonb(target)::text FROM firmware_rollout_device target WHERE rollout_id = $1`, rollout).Scan(&targetAfter))
	require.Equal(t, targetBefore, targetAfter, "resuming cannot move device evidence or dispatch timestamps")

	// A backwards clock cannot subtract elapsed pauses, and a terminal
	// rollout cannot accumulate another pause or change attribution.
	f.exec(`UPDATE firmware_rollout SET paused_at = clock_timestamp() + INTERVAL '1 hour' WHERE id = $1`, rollout)
	before, err := f.q.GetFirmwareRollout(t.Context(), sqlc.GetFirmwareRolloutParams{RolloutID: rollout, OrgID: f.org})
	require.NoError(t, err)
	rows, err := f.q.ResumeFirmwareRollout(t.Context(), params)
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)
	after, err := f.q.GetFirmwareRollout(t.Context(), sqlc.GetFirmwareRolloutParams{RolloutID: rollout, OrgID: f.org})
	require.NoError(t, err)
	require.Equal(t, before.StagePausedMicroseconds, after.StagePausedMicroseconds)
	require.False(t, after.PausedAt.Valid)
	_, err = f.q.FinishFirmwareRollout(t.Context(), sqlc.FinishFirmwareRolloutParams{RolloutID: rollout, Status: "completed"})
	require.NoError(t, err)
	finished, err := f.q.GetFirmwareRollout(t.Context(), sqlc.GetFirmwareRolloutParams{RolloutID: rollout, OrgID: f.org})
	require.NoError(t, err)
	rows, err = f.q.ResumeFirmwareRollout(t.Context(), params)
	require.NoError(t, err)
	require.Zero(t, rows)
	unchanged, err := f.q.GetFirmwareRollout(t.Context(), sqlc.GetFirmwareRolloutParams{RolloutID: rollout, OrgID: f.org})
	require.NoError(t, err)
	require.Equal(t, finished, unchanged)
}

func TestReleaseChannelQueries_ResumeTimeAfterLockWait(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	for _, updatePause := range []bool{false, true} {
		name := "unchanged header lock"
		if updatePause {
			name = "concurrent pause update"
		}
		t.Run(name, func(t *testing.T) {
			f := newReleaseChannelQueryFixture(t)
			rollout := f.rollout(f.channel("resume-clock"), "Bitmain", "S19")
			f.exec(`UPDATE firmware_rollout SET created_at = clock_timestamp() - INTERVAL '1 hour',
				stage_changed_at = clock_timestamp() - INTERVAL '1 hour', paused_at = clock_timestamp() - INTERVAL '10 seconds',
				stage_paused_microseconds = 5000000 WHERE id = $1`, rollout)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			waiter, err := f.db.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer func() { _ = waiter.Rollback() }()
			var waiterPID int
			require.NoError(t, waiter.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&waiterPID))
			blocker, err := f.db.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer func() { _ = blocker.Rollback() }()
			_, err = blocker.ExecContext(ctx, `SELECT id FROM firmware_rollout WHERE id = $1 FOR UPDATE`, rollout)
			require.NoError(t, err)
			if updatePause {
				_, err = blocker.ExecContext(ctx, `UPDATE firmware_rollout SET paused_at = paused_at - INTERVAL '30 seconds' WHERE id = $1`, rollout)
				require.NoError(t, err)
			}
			locked, err := sqlc.New(blocker).GetFirmwareRollout(ctx, sqlc.GetFirmwareRolloutParams{RolloutID: rollout, OrgID: f.org})
			require.NoError(t, err)
			type result struct {
				rows int64
				err  error
			}
			done := make(chan result, 1)
			q := sqlc.New(waiter)
			go func() {
				rows, err := q.ResumeFirmwareRollout(ctx, sqlc.ResumeFirmwareRolloutParams{RolloutID: rollout, ActorType: "user", ActorID: 42, ActorName: "resuming operator"})
				done <- result{rows, err}
			}()
			require.Eventually(t, func() bool {
				var blocked bool
				err := f.db.QueryRowContext(ctx, `SELECT cardinality(pg_blocking_pids($1)) > 0`, waiterPID).Scan(&blocked)
				return err == nil && blocked
			}, 3*time.Second, 10*time.Millisecond, "resume must wait for the header lock")
			var beforeUnlock time.Time
			require.NoError(t, blocker.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&beforeUnlock))
			require.NoError(t, blocker.Commit())
			outcome := <-done
			require.NoError(t, outcome.err)
			require.Equal(t, int64(1), outcome.rows)
			resumed, err := q.GetFirmwareRollout(ctx, sqlc.GetFirmwareRolloutParams{RolloutID: rollout, OrgID: f.org})
			require.NoError(t, err)
			require.False(t, resumed.PausedAt.Valid)
			require.GreaterOrEqual(t, resumed.StagePausedMicroseconds-locked.StagePausedMicroseconds,
				beforeUnlock.Sub(locked.PausedAt.Time).Microseconds(), "the pause includes the header lock wait")
			require.Equal(t, locked.StageChangedAt, resumed.StageChangedAt)
			require.Equal(t, "resuming operator", resumed.LastActionByName)
			require.NoError(t, waiter.Commit())
		})
	}
}

func TestReleaseChannelQueries_StageTransitionGuardsPauseTimer(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	for _, changed := range []string{"paused", "resumed", "stage timestamp"} {
		t.Run(changed, func(t *testing.T) {
			f := newReleaseChannelQueryFixture(t)
			rollout := f.rollout(f.channel("stage-pause-guard"), "Bitmain", "S19")
			f.exec(`UPDATE firmware_rollout SET created_at = clock_timestamp() - INTERVAL '1 hour',
				stage_changed_at = clock_timestamp() - INTERVAL '1 hour', stage_paused_microseconds = 5000000 WHERE id = $1`, rollout)
			snapshot, err := f.q.GetFirmwareRollout(t.Context(), sqlc.GetFirmwareRolloutParams{RolloutID: rollout, OrgID: f.org})
			require.NoError(t, err)
			params := sqlc.AdvanceFirmwareRolloutStageParams{
				RolloutID: rollout, FromStage: "rest", Stage: "batch", CurrentBatch: 1,
				ExpectedStageChangedAt: snapshot.StageChangedAt, ExpectedStagePausedMicroseconds: snapshot.StagePausedMicroseconds,
			}
			if changed == "stage timestamp" {
				f.exec(`UPDATE firmware_rollout SET stage_changed_at = stage_changed_at + INTERVAL '1 microsecond' WHERE id = $1`, rollout)
			} else {
				_, err := f.q.PauseFirmwareRollout(t.Context(), sqlc.PauseFirmwareRolloutParams{RolloutID: rollout, ActorType: "system"})
				require.NoError(t, err)
				f.exec(`UPDATE firmware_rollout SET paused_at = clock_timestamp() - INTERVAL '10 seconds' WHERE id = $1`, rollout)
				if changed == "resumed" {
					_, err = f.q.ResumeFirmwareRollout(t.Context(), sqlc.ResumeFirmwareRolloutParams{RolloutID: rollout, ActorType: "system"})
					require.NoError(t, err)
				}
			}
			before, err := f.q.GetFirmwareRollout(t.Context(), sqlc.GetFirmwareRolloutParams{RolloutID: rollout, OrgID: f.org})
			require.NoError(t, err)
			_, err = f.q.AdvanceFirmwareRolloutStage(t.Context(), params)
			require.ErrorIs(t, err, sql.ErrNoRows)
			unchanged, err := f.q.GetFirmwareRollout(t.Context(), sqlc.GetFirmwareRolloutParams{RolloutID: rollout, OrgID: f.org})
			require.NoError(t, err)
			require.Equal(t, before, unchanged, "paused or stale timer snapshots cannot advance a stage")
			if changed == "paused" {
				_, err = f.q.ResumeFirmwareRollout(t.Context(), sqlc.ResumeFirmwareRolloutParams{RolloutID: rollout, ActorType: "system"})
				require.NoError(t, err)
			}
			fresh, err := f.q.GetFirmwareRollout(t.Context(), sqlc.GetFirmwareRolloutParams{RolloutID: rollout, OrgID: f.org})
			require.NoError(t, err)
			params.ExpectedStageChangedAt = fresh.StageChangedAt
			params.ExpectedStagePausedMicroseconds = fresh.StagePausedMicroseconds
			stageChangedAt, err := f.q.AdvanceFirmwareRolloutStage(t.Context(), params)
			require.NoError(t, err)
			after, err := f.q.GetFirmwareRollout(t.Context(), sqlc.GetFirmwareRolloutParams{RolloutID: rollout, OrgID: f.org})
			require.NoError(t, err)
			require.Equal(t, "batch", after.Stage)
			require.Equal(t, stageChangedAt, after.StageChangedAt)
			require.Zero(t, after.StagePausedMicroseconds, "a new stage starts its own timer")
		})
	}
}
