package sqlstores_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
)

func TestReleaseChannelQueries_PauseTimeAfterLockWait(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	for _, advanceStage := range []bool{false, true} {
		name := "unchanged header lock"
		if advanceStage {
			name = "concurrent stage transition"
		}
		t.Run(name, func(t *testing.T) {
			f := newReleaseChannelQueryFixture(t)
			channel := f.channel("pause-clock")
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			waiter, err := f.db.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer func() { _ = waiter.Rollback() }()
			var waiterPID int
			var txStart time.Time
			require.NoError(t, waiter.QueryRowContext(ctx, `SELECT pg_backend_pid(), now()`).Scan(&waiterPID, &txStart))
			rollout := f.rollout(channel, "Bitmain", "S19")
			blocker, err := f.db.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer func() { _ = blocker.Rollback() }()
			_, err = blocker.ExecContext(ctx, `SELECT id FROM firmware_rollout WHERE id = $1 FOR UPDATE`, rollout)
			require.NoError(t, err)
			if advanceStage {
				before, err := sqlc.New(blocker).GetFirmwareRollout(ctx, sqlc.GetFirmwareRolloutParams{RolloutID: rollout, OrgID: f.org})
				require.NoError(t, err)
				_, err = sqlc.New(blocker).AdvanceFirmwareRolloutStage(ctx, sqlc.AdvanceFirmwareRolloutStageParams{
					RolloutID: rollout, FromStage: "rest", Stage: "waiting", CurrentBatch: 1,
					ExpectedStageChangedAt: before.StageChangedAt, ExpectedStagePausedMicroseconds: before.StagePausedMicroseconds,
				})
				require.NoError(t, err)
				_, err = blocker.ExecContext(ctx, `UPDATE firmware_rollout SET stage_changed_at = clock_timestamp() + INTERVAL '1 hour' WHERE id = $1`, rollout)
				require.NoError(t, err)
			}
			q := sqlc.New(waiter)
			done := make(chan error, 1)
			go func() {
				_, err := q.PauseFirmwareRollout(ctx, sqlc.PauseFirmwareRolloutParams{
					RolloutID: rollout, ActorType: "user", ActorID: 42, ActorName: "pause operator",
				})
				done <- err
			}()
			require.Eventually(t, func() bool {
				var blocked bool
				err := f.db.QueryRowContext(ctx, `SELECT cardinality(pg_blocking_pids($1)) > 0`, waiterPID).Scan(&blocked)
				return err == nil && blocked
			}, 3*time.Second, 10*time.Millisecond, "pause must wait for the header lock")
			var beforeUnlock time.Time
			require.NoError(t, blocker.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&beforeUnlock))
			require.NoError(t, blocker.Commit())
			require.NoError(t, <-done)
			row, err := q.GetFirmwareRollout(ctx, sqlc.GetFirmwareRolloutParams{RolloutID: rollout, OrgID: f.org})
			require.NoError(t, err)
			require.True(t, row.PausedAt.Valid)
			require.False(t, row.PausedAt.Time.Before(beforeUnlock), "pause time excludes transaction age and lock waits")
			require.True(t, row.PausedAt.Time.After(txStart))
			require.False(t, row.PausedAt.Time.Before(row.CreatedAt))
			require.False(t, row.PausedAt.Time.Before(row.StageChangedAt))
			require.Equal(t, "pause operator", row.LastActionByName)
			if advanceStage {
				require.Equal(t, "waiting", row.Stage)
			}
			require.NoError(t, waiter.Commit())
			// A repeated pause preserves its timestamp, actor and revision.
			rows, err := f.q.PauseFirmwareRollout(ctx, sqlc.PauseFirmwareRolloutParams{
				RolloutID: rollout, ActorType: "user", ActorID: 99, ActorName: "later caller",
			})
			require.NoError(t, err)
			require.Zero(t, rows)
			unchanged, err := f.q.GetFirmwareRollout(ctx, sqlc.GetFirmwareRolloutParams{RolloutID: rollout, OrgID: f.org})
			require.NoError(t, err)
			require.Equal(t, row, unchanged)
		})
	}
}

func TestReleaseChannelQueries_PauseTimeRespectsLifecycleClocksAndStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	for _, laterClock := range []string{"created_at", "stage_changed_at"} {
		t.Run(laterClock, func(t *testing.T) {
			f := newReleaseChannelQueryFixture(t)
			rollout := f.rollout(f.channel("pause-monotonic"), "Bitmain", "S19")
			// A clock adjustment cannot make the pause precede persisted events.
			f.exec(`UPDATE firmware_rollout
				SET created_at = clock_timestamp() + CASE WHEN $2 = 'created_at' THEN INTERVAL '2 hours' ELSE INTERVAL '1 hour' END,
				    stage_changed_at = clock_timestamp() + CASE WHEN $2 = 'stage_changed_at' THEN INTERVAL '2 hours' ELSE INTERVAL '1 hour' END
				WHERE id = $1`, rollout, laterClock)
			params := sqlc.PauseFirmwareRolloutParams{RolloutID: rollout, ActorType: "system"}
			rows, err := f.q.PauseFirmwareRollout(t.Context(), params)
			require.NoError(t, err)
			require.Equal(t, int64(1), rows)
			row, err := f.q.GetFirmwareRollout(t.Context(), sqlc.GetFirmwareRolloutParams{RolloutID: rollout, OrgID: f.org})
			require.NoError(t, err)
			require.True(t, row.PausedAt.Valid)
			require.False(t, row.PausedAt.Time.Before(row.CreatedAt))
			require.False(t, row.PausedAt.Time.Before(row.StageChangedAt))
			f.exec(`UPDATE firmware_rollout SET status = 'completed', paused_at = NULL WHERE id = $1`, rollout)
			before, err := f.q.GetFirmwareRollout(t.Context(), sqlc.GetFirmwareRolloutParams{RolloutID: rollout, OrgID: f.org})
			require.NoError(t, err)
			rows, err = f.q.PauseFirmwareRollout(t.Context(), params)
			require.NoError(t, err)
			require.Zero(t, rows)
			after, err := f.q.GetFirmwareRollout(t.Context(), sqlc.GetFirmwareRolloutParams{RolloutID: rollout, OrgID: f.org})
			require.NoError(t, err)
			require.Equal(t, before, after, "completed rollouts cannot be paused")
		})
	}
}
