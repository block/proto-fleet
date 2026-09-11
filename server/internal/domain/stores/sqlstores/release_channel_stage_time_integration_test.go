package sqlstores_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/infrastructure/dbtypes"
	"github.com/stretchr/testify/require"
)

func TestReleaseChannelQueries_StageTimeAfterLockWait(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	for _, updateHeader := range []bool{false, true} {
		name := "unchanged row lock"
		if updateHeader {
			name = "concurrent header update"
		}
		t.Run(name, func(t *testing.T) {
			f := newReleaseChannelQueryFixture(t)
			rollout := f.rollout(f.channel("stage-time"), "Bitmain", "S19")
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			waiter, err := f.db.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer func() { _ = waiter.Rollback() }()
			var waiterPID int
			var txStart time.Time
			require.NoError(t, waiter.QueryRowContext(ctx, `SELECT pg_backend_pid(), now()`).Scan(&waiterPID, &txStart))

			blocker, err := f.db.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer func() { _ = blocker.Rollback() }()
			_, err = blocker.ExecContext(ctx, `SELECT id FROM firmware_rollout WHERE id = $1 FOR UPDATE`, rollout)
			require.NoError(t, err)
			if updateHeader {
				_, err = blocker.ExecContext(ctx, `UPDATE firmware_rollout SET last_action_by_name = 'lock holder' WHERE id = $1`, rollout)
				require.NoError(t, err)
			}

			type result struct {
				rows int64
				err  error
			}
			done := make(chan result, 1)
			q := sqlc.New(waiter)
			go func() {
				rows, err := q.AdvanceFirmwareRolloutStage(ctx, sqlc.AdvanceFirmwareRolloutStageParams{
					RolloutID: rollout, FromStage: "rest", Stage: "waiting", CurrentBatch: 1,
				})
				done <- result{rows, err}
			}()
			require.Eventually(t, func() bool {
				var blocked bool
				err := f.db.QueryRowContext(ctx, `SELECT cardinality(pg_blocking_pids($1)) > 0`, waiterPID).Scan(&blocked)
				return err == nil && blocked
			}, 3*time.Second, 10*time.Millisecond, "transition must wait for the row lock")
			// Use the database clock after the statement is already blocked. A
			// timestamp evaluated before acquiring the lock is too early, even
			// when the holder releases the row without changing its tuple.
			var beforeUnlock time.Time
			require.NoError(t, blocker.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&beforeUnlock))
			require.NoError(t, blocker.Commit())
			outcome := <-done
			require.NoError(t, outcome.err)
			require.Equal(t, int64(1), outcome.rows)
			row, err := q.GetFirmwareRollout(ctx, sqlc.GetFirmwareRolloutParams{RolloutID: rollout, OrgID: f.org})
			require.NoError(t, err)
			require.False(t, row.StageChangedAt.Before(beforeUnlock), "stage time must exclude transaction age and lock wait")
			require.True(t, row.StageChangedAt.After(txStart))
			require.Equal(t, "waiting", row.Stage)
			require.Equal(t, int32(1), row.CurrentBatch)
			if updateHeader {
				require.Equal(t, "lock holder", row.LastActionByName, "omitted actor preserves the latest attribution")
			}
			require.NoError(t, waiter.Commit())
		})
	}
}

func TestReleaseChannelQueries_StageTimeMonotonicAndGuarded(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	f := newReleaseChannelQueryFixture(t)
	rollout := f.rollout(f.channel("monotonic-stage-time"), "Bitmain", "S19")
	// A persisted timestamp ahead of the current clock must not move back.
	f.exec(`UPDATE firmware_rollout SET stage_changed_at = clock_timestamp() + INTERVAL '1 hour' WHERE id = $1`, rollout)
	before, err := f.q.GetFirmwareRollout(t.Context(), sqlc.GetFirmwareRolloutParams{RolloutID: rollout, OrgID: f.org})
	require.NoError(t, err)
	params := sqlc.AdvanceFirmwareRolloutStageParams{
		RolloutID: rollout, FromStage: "rest", Stage: "batch", CurrentBatch: 2,
		ActorType: sql.NullString{String: "user", Valid: true},
		ActorID:   sql.NullInt64{Int64: 42, Valid: true},
		ActorName: sql.NullString{String: "operator", Valid: true},
	}
	rows, err := f.q.AdvanceFirmwareRolloutStage(t.Context(), params)
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)
	after, err := f.q.GetFirmwareRollout(t.Context(), sqlc.GetFirmwareRolloutParams{RolloutID: rollout, OrgID: f.org})
	require.NoError(t, err)
	require.True(t, after.StageChangedAt.Equal(before.StageChangedAt))
	require.Equal(t, before.Revision+1, after.Revision)
	require.Equal(t, "operator", after.LastActionByName)

	// Losing a stage race must leave the timestamp and revision unchanged.
	rows, err = f.q.AdvanceFirmwareRolloutStage(t.Context(), params)
	require.NoError(t, err)
	require.Zero(t, rows)
	unchanged, err := f.q.GetFirmwareRollout(t.Context(), sqlc.GetFirmwareRolloutParams{RolloutID: rollout, OrgID: f.org})
	require.NoError(t, err)
	require.Equal(t, after, unchanged)

	f.exec(`UPDATE firmware_rollout SET status = 'completed' WHERE id = $1`, rollout)
	before, err = f.q.GetFirmwareRollout(t.Context(), sqlc.GetFirmwareRolloutParams{RolloutID: rollout, OrgID: f.org})
	require.NoError(t, err)
	params.FromStage = "batch"
	params.Stage = "waiting"
	rows, err = f.q.AdvanceFirmwareRolloutStage(t.Context(), params)
	require.NoError(t, err)
	require.Zero(t, rows)
	unchanged, err = f.q.GetFirmwareRollout(t.Context(), sqlc.GetFirmwareRolloutParams{RolloutID: rollout, OrgID: f.org})
	require.NoError(t, err)
	require.Equal(t, before, unchanged, "inactive rollouts cannot transition")
}

func TestReleaseChannelQueries_InitialStageTimeAfterLockWait(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	f := newReleaseChannelQueryFixture(t)
	channel := f.channel("initial-stage-time")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	waiter, err := f.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = waiter.Rollback() }()
	var waiterPID int
	var txStart time.Time
	require.NoError(t, waiter.QueryRowContext(ctx, `SELECT pg_backend_pid(), now()`).Scan(&waiterPID, &txStart))

	blocker, err := f.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = blocker.Rollback() }()
	require.NoError(t, sqlc.New(blocker).LockReleaseChannelScopes(ctx, f.org))
	q := sqlc.New(waiter)
	locked := make(chan error, 1)
	go func() { locked <- q.LockReleaseChannelScopes(ctx, f.org) }()
	require.Eventually(t, func() bool {
		var blocked bool
		err := f.db.QueryRowContext(ctx, `SELECT cardinality(pg_blocking_pids($1)) > 0`, waiterPID).Scan(&blocked)
		return err == nil && blocked
	}, 3*time.Second, 10*time.Millisecond, "creation transaction must wait for the scope lock")
	var beforeUnlock time.Time
	require.NoError(t, blocker.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&beforeUnlock))
	require.NoError(t, blocker.Commit())
	require.NoError(t, <-locked)

	// Both an initial batch and an unbatched stage receive the creation
	// statement's time, independent of how old their transaction is.
	for _, stage := range []string{"batch", "rest"} {
		var beforeInsert time.Time
		require.NoError(t, waiter.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&beforeInsert))
		row, err := q.CreateFirmwareRollout(ctx, sqlc.CreateFirmwareRolloutParams{
			OrgID: f.org, ChannelID: channel, Manufacturer: "Bitmain", Model: stage,
			FirmwareChecksum: "sum", FirmwareVersion: "v2", AssignmentGeneration: 1,
			Stage: stage, ActorType: "system",
			BehaviorSnapshot: dbtypes.RolloutBehaviorSnapshot{Method: "all_at_once", OrderBy: "least_efficient_first"},
		})
		require.NoError(t, err)
		require.Equal(t, stage, row.Stage)
		require.False(t, row.StageChangedAt.Before(beforeInsert), "initial stage starts at creation")
		require.False(t, row.StageChangedAt.Before(beforeUnlock), "initial stage excludes prior lock waits")
		require.True(t, row.StageChangedAt.After(txStart))
		require.Equal(t, int64(1), row.Revision)
	}
	require.NoError(t, waiter.Commit())
}
