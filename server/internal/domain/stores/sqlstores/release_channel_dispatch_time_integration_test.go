package sqlstores_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
)

func TestReleaseChannelQueries_DispatchTimeAfterLockWait(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	for _, concurrentAttempt := range []bool{false, true} {
		name := "unchanged target lock"
		if concurrentAttempt {
			name = "concurrent attempt"
		}
		t.Run(name, func(t *testing.T) {
			f := newReleaseChannelQueryFixture(t)
			device := f.device("dispatch-clock", "Bitmain", "S19", "v1")
			rollout := f.rollout(f.channel("dispatch-clock"), "Bitmain", "S19")
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			require.NoError(t, f.q.AppendFirmwareRolloutDevices(ctx, sqlc.AppendFirmwareRolloutDevicesParams{
				RolloutID: rollout, DeviceIds: []int64{device.id},
			}))
			waiter, err := f.db.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer func() { _ = waiter.Rollback() }()
			var waiterPID int
			var txStart time.Time
			require.NoError(t, waiter.QueryRowContext(ctx, `SELECT pg_backend_pid(), now()`).Scan(&waiterPID, &txStart))

			blocker, err := f.db.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer func() { _ = blocker.Rollback() }()
			_, err = blocker.ExecContext(ctx, `SELECT device_id FROM firmware_rollout_device WHERE rollout_id = $1 FOR UPDATE`, rollout)
			require.NoError(t, err)
			params := sqlc.MarkFirmwareRolloutDevicesSentParams{
				RolloutID: rollout, DeviceIds: []int64{device.id}, DispatchedDeviceIds: []int64{device.id},
				BatchUuid: "00000000-0000-0000-0000-000000000011",
			}
			var firstAttempt time.Time
			if concurrentAttempt {
				require.NoError(t, sqlc.New(blocker).MarkFirmwareRolloutDevicesSent(ctx, params))
				require.NoError(t, blocker.QueryRowContext(ctx, `SELECT first_sent_at FROM firmware_rollout_device WHERE rollout_id = $1`, rollout).Scan(&firstAttempt))
			}
			params.BatchUuid = "00000000-0000-0000-0000-000000000012"
			q := sqlc.New(waiter)
			done := make(chan error, 1)
			go func() { done <- q.MarkFirmwareRolloutDevicesSent(ctx, params) }()
			require.Eventually(t, func() bool {
				var blocked bool
				err := f.db.QueryRowContext(ctx, `SELECT cardinality(pg_blocking_pids($1)) > 0`, waiterPID).Scan(&blocked)
				return err == nil && blocked
			}, 3*time.Second, 10*time.Millisecond, "dispatch recording must wait for the target lock")
			var beforeUnlock time.Time
			require.NoError(t, blocker.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&beforeUnlock))
			require.NoError(t, blocker.Commit())
			require.NoError(t, <-done)
			rows, err := q.ListFirmwareRolloutDevices(ctx, rollout)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			row := rows[0]
			require.True(t, row.LastSentAt.Valid)
			require.False(t, row.LastSentAt.Time.Before(beforeUnlock), "retry pacing excludes transaction age and lock waits")
			require.True(t, row.LastSentAt.Time.After(txStart))
			require.Equal(t, row.LastSentAt, row.LastDispatchedAt, "one timestamp describes the accepted attempt")
			require.Equal(t, params.BatchUuid, row.LastDispatchedBatchUuid.String)
			if concurrentAttempt {
				require.Equal(t, int32(2), row.Attempts)
				require.Equal(t, firstAttempt, row.FirstSentAt.Time, "a retry preserves the first attempt")
			} else {
				require.Equal(t, int32(1), row.Attempts)
				require.Equal(t, row.LastSentAt, row.FirstSentAt)
			}
			require.NoError(t, waiter.Commit())
		})
	}
}

func TestReleaseChannelQueries_DispatchTimeNeverMovesBackward(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	f := newReleaseChannelQueryFixture(t)
	device := f.device("dispatch-monotonic", "Bitmain", "S19", "v1")
	rollout := f.rollout(f.channel("dispatch-monotonic"), "Bitmain", "S19")
	ctx := t.Context()
	require.NoError(t, f.q.AppendFirmwareRolloutDevices(ctx, sqlc.AppendFirmwareRolloutDevicesParams{
		RolloutID: rollout, DeviceIds: []int64{device.id},
	}))
	params := sqlc.MarkFirmwareRolloutDevicesSentParams{
		RolloutID: rollout, DeviceIds: []int64{device.id}, DispatchedDeviceIds: []int64{device.id},
		BatchUuid: "00000000-0000-0000-0000-000000000021",
	}
	require.NoError(t, f.q.MarkFirmwareRolloutDevicesSent(ctx, params))
	// Simulate the clock moving behind an already persisted dispatch.
	f.exec(`UPDATE firmware_rollout_device SET last_sent_at = clock_timestamp() + INTERVAL '1 hour',
		last_dispatched_at = clock_timestamp() + INTERVAL '1 hour' WHERE rollout_id = $1`, rollout)
	before, err := f.q.ListFirmwareRolloutDevices(ctx, rollout)
	require.NoError(t, err)
	params.BatchUuid = "00000000-0000-0000-0000-000000000022"
	require.NoError(t, f.q.MarkFirmwareRolloutDevicesSent(ctx, params))
	after, err := f.q.ListFirmwareRolloutDevices(ctx, rollout)
	require.NoError(t, err)
	require.False(t, after[0].LastSentAt.Time.Before(before[0].LastSentAt.Time))
	require.False(t, after[0].LastDispatchedAt.Time.Before(before[0].LastDispatchedAt.Time))
	require.Equal(t, before[0].FirstSentAt, after[0].FirstSentAt)
	require.Equal(t, after[0].LastSentAt, after[0].LastDispatchedAt)
	require.Equal(t, int32(2), after[0].Attempts)
}
