package sqlstores_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
)

func TestReleaseChannelQueries_TerminalTransitionsClearPause(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	for _, mutation := range pausedRolloutTerminalMutations() {
		t.Run(mutation.name, func(t *testing.T) {
			f := newReleaseChannelQueryFixture(t)
			channel := f.channel("terminal-pause")
			rollout := f.rollout(channel, "Bitmain", "S19")
			rows, err := f.q.PauseFirmwareRollout(t.Context(), sqlc.PauseFirmwareRolloutParams{
				RolloutID: rollout, ActorType: "user", ActorID: 41, ActorName: "pausing operator",
			})
			require.NoError(t, err)
			require.Equal(t, int64(1), rows)
			// Simulate the wall clock moving back after the pause. Clearing the
			// pause must still retain its time as a lower bound on completion.
			f.exec(`UPDATE firmware_rollout SET paused_at = clock_timestamp() + INTERVAL '1 hour', stage_paused_microseconds = 5000000 WHERE id = $1`, rollout)
			paused, err := f.q.GetFirmwareRollout(t.Context(), sqlc.GetFirmwareRolloutParams{RolloutID: rollout, OrgID: f.org})
			require.NoError(t, err)
			require.True(t, paused.PausedAt.Valid)

			require.NoError(t, mutation.run(t.Context(), f.q, channel, rollout))
			finished, err := f.q.GetFirmwareRollout(t.Context(), sqlc.GetFirmwareRolloutParams{RolloutID: rollout, OrgID: f.org})
			require.NoError(t, err)
			require.Equal(t, mutation.status, finished.Status)
			require.Equal(t, mutation.cancelReason, finished.CancelReason)
			require.False(t, finished.PausedAt.Valid, "terminal rollouts cannot remain paused")
			require.Equal(t, paused.StagePausedMicroseconds, finished.StagePausedMicroseconds)
			require.True(t, finished.FinishedAt.Valid)
			require.False(t, finished.FinishedAt.Time.Before(paused.PausedAt.Time), "completion must follow the pause even after a clock correction")
			require.Greater(t, finished.Revision, paused.Revision)

			// Repeated terminal transitions cannot change the first outcome,
			// attribution, lifecycle timestamps or revision.
			for _, repeat := range pausedRolloutTerminalMutations() {
				require.NoError(t, repeat.run(t.Context(), f.q, channel, rollout))
			}
			unchanged, err := f.q.GetFirmwareRollout(t.Context(), sqlc.GetFirmwareRolloutParams{RolloutID: rollout, OrgID: f.org})
			require.NoError(t, err)
			require.Equal(t, finished, unchanged)
		})
	}
}

func pausedRolloutTerminalMutations() []rolloutTerminalMutation {
	mutations := rolloutTerminalMutations()
	for _, reason := range []string{"cleared", "rolled_back"} {
		mutations = append(mutations, rolloutTerminalMutation{
			name: "assignment " + reason, status: "canceled", cancelReason: reason,
			run: func(ctx context.Context, q sqlc.Querier, channel, _ int64) error {
				return q.CancelActiveFirmwareRollout(ctx, sqlc.CancelActiveFirmwareRolloutParams{
					ChannelID: channel, Manufacturer: "Bitmain", Model: "S19", CancelReason: reason,
					ActorType: "user", ActorID: 42, ActorName: "assignment operator",
				})
			},
		})
	}
	return append(mutations, rolloutTerminalMutation{
		name: "completion with failures", status: "completed_with_failures",
		run: func(ctx context.Context, q sqlc.Querier, _, rollout int64) error {
			_, err := q.FinishFirmwareRollout(ctx, sqlc.FinishFirmwareRolloutParams{RolloutID: rollout, Status: "completed_with_failures"})
			return err
		},
	})
}
