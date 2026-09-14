package sqlstores_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
)

func TestReleaseChannelQueries_FinishTimeAfterLockWait(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	for _, mutation := range rolloutTerminalMutations() {
		t.Run(mutation.name, func(t *testing.T) {
			f := newReleaseChannelQueryFixture(t)
			channel := f.channel("finish-clock")
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			waiter, err := f.db.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer func() { _ = waiter.Rollback() }()
			var waiterPID int
			var txStart time.Time
			require.NoError(t, waiter.QueryRowContext(ctx, `SELECT pg_backend_pid(), now()`).Scan(&waiterPID, &txStart))

			// The finishing transaction predates creation, then waits behind an
			// unchanged header lock. Neither interval belongs in the finish time.
			rollout := f.rollout(channel, "Bitmain", "S19")
			blocker, err := f.db.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer func() { _ = blocker.Rollback() }()
			_, err = blocker.ExecContext(ctx, `SELECT id FROM firmware_rollout WHERE id = $1 FOR UPDATE`, rollout)
			require.NoError(t, err)
			q := sqlc.New(waiter)
			done := make(chan error, 1)
			go func() { done <- mutation.run(ctx, q, channel, rollout) }()
			require.Eventually(t, func() bool {
				var blocked bool
				err := f.db.QueryRowContext(ctx, `SELECT cardinality(pg_blocking_pids($1)) > 0`, waiterPID).Scan(&blocked)
				return err == nil && blocked
			}, 3*time.Second, 10*time.Millisecond, "completion must wait for the header lock")
			var beforeUnlock time.Time
			require.NoError(t, blocker.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&beforeUnlock))
			require.NoError(t, blocker.Commit())
			require.NoError(t, <-done)
			row, err := q.GetFirmwareRollout(ctx, sqlc.GetFirmwareRolloutParams{RolloutID: rollout, OrgID: f.org})
			require.NoError(t, err)
			require.Equal(t, mutation.status, row.Status)
			require.Equal(t, mutation.cancelReason, row.CancelReason)
			require.True(t, row.FinishedAt.Valid)
			require.False(t, row.FinishedAt.Time.Before(beforeUnlock), "finish time excludes transaction age and lock wait")
			require.False(t, row.FinishedAt.Time.Before(row.CreatedAt), "a rollout cannot finish before it was created")
			require.True(t, row.FinishedAt.Time.After(txStart))
			require.NoError(t, waiter.Commit())

			// Every terminal path must preserve the first outcome, attribution,
			// timestamp and revision when invoked again after completion.
			for _, repeat := range rolloutTerminalMutations() {
				require.NoError(t, repeat.run(ctx, f.q, channel, rollout))
			}
			unchanged, err := f.q.GetFirmwareRollout(ctx, sqlc.GetFirmwareRolloutParams{RolloutID: rollout, OrgID: f.org})
			require.NoError(t, err)
			require.Equal(t, row, unchanged)
		})
	}
}

func TestReleaseChannelQueries_FinishTimeRespectsPersistedClocks(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	for _, mutation := range rolloutTerminalMutations() {
		t.Run(mutation.name, func(t *testing.T) {
			f := newReleaseChannelQueryFixture(t)
			channel := f.channel("finish-monotonic")
			for _, laterClock := range []string{"created_at", "stage_changed_at"} {
				t.Run(laterClock, func(t *testing.T) {
					rollout := f.rollout(channel, "Bitmain", "S19")
					// Simulate a wall clock moving back after either creation or
					// a stage transition; completion still follows both events.
					f.exec(`UPDATE firmware_rollout
						SET created_at = clock_timestamp() + CASE WHEN $2 = 'created_at' THEN INTERVAL '2 hours' ELSE INTERVAL '1 hour' END,
						    stage_changed_at = clock_timestamp() + CASE WHEN $2 = 'stage_changed_at' THEN INTERVAL '2 hours' ELSE INTERVAL '1 hour' END
						WHERE id = $1`, rollout, laterClock)
					require.NoError(t, mutation.run(t.Context(), f.q, channel, rollout))
					row, err := f.q.GetFirmwareRollout(t.Context(), sqlc.GetFirmwareRolloutParams{RolloutID: rollout, OrgID: f.org})
					require.NoError(t, err)
					require.True(t, row.FinishedAt.Valid)
					require.False(t, row.FinishedAt.Time.Before(row.CreatedAt))
					require.False(t, row.FinishedAt.Time.Before(row.StageChangedAt))
				})
			}
		})
	}
}

type rolloutTerminalMutation struct {
	name         string
	status       string
	cancelReason string
	run          func(context.Context, sqlc.Querier, int64, int64) error
}

func rolloutTerminalMutations() []rolloutTerminalMutation {
	return []rolloutTerminalMutation{
		{
			name: "assignment cancellation", status: "canceled", cancelReason: "superseded",
			run: func(ctx context.Context, q sqlc.Querier, channel, _ int64) error {
				return q.CancelActiveFirmwareRollout(ctx, sqlc.CancelActiveFirmwareRolloutParams{
					ChannelID: channel, Manufacturer: "Bitmain", Model: "S19", CancelReason: "superseded",
					ActorType: "user", ActorID: 42, ActorName: "assignment operator",
				})
			},
		},
		{
			name: "operator cancellation", status: "canceled", cancelReason: "canceled_remaining",
			run: func(ctx context.Context, q sqlc.Querier, _, rollout int64) error {
				_, err := q.CancelFirmwareRollout(ctx, sqlc.CancelFirmwareRolloutParams{
					RolloutID: rollout, ActorType: "user", ActorID: 43, ActorName: "canceling operator",
				})
				return err
			},
		},
		{
			name: "completion", status: "completed",
			run: func(ctx context.Context, q sqlc.Querier, _, rollout int64) error {
				_, err := q.FinishFirmwareRollout(ctx, sqlc.FinishFirmwareRolloutParams{RolloutID: rollout, Status: "completed"})
				return err
			},
		},
	}
}
