package rollout

import (
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/stretchr/testify/require"
)

func TestPreparationPreservesRolloutFinishedAfterActiveScan(t *testing.T) {
	for _, action := range []string{"cancel", "supersede"} {
		t.Run(action, func(t *testing.T) {
			f := newFixture(t, 2)
			ctx := t.Context()
			f.channel(t, allAtOnce, "miner-0")
			started := f.apply(t, "fw-1")
			q := f.svc.store.GetQueries(ctx)
			active, err := q.ListActiveFirmwareRollouts(ctx)
			require.NoError(t, err)
			require.Len(t, active, 1)
			stale := active[0].FirmwareRollout
			if action == "cancel" {
				_, _, err = f.svc.CancelRollout(ctx, f.orgID, started.ID, byOperator)
				require.NoError(t, err)
			} else {
				f.apply(t, "fw-2")
			}
			_, err = f.svc.UpdateChannel(ctx, f.orgID, f.channelID, ChannelSpec{
				Name: "Test channel", Scope: Scope{DeviceIdentifiers: []string{"miner-1"}}, Behavior: allAtOnce,
			})
			require.NoError(t, err)
			finished, err := q.GetFirmwareRollout(ctx, sqlc.GetFirmwareRolloutParams{RolloutID: started.ID, OrgID: f.orgID})
			require.NoError(t, err)
			before, err := q.ListFirmwareRolloutDevices(ctx, started.ID)
			require.NoError(t, err)
			require.Len(t, before, 1)

			// The active-row scan predates the terminal operation and scope edit.
			// Preparation must not exclude old targets or append an unsuppressed
			// late joiner to this finished rollout's retained history.
			targets, err := f.svc.prepareRollout(ctx, stale)
			require.NoError(t, err)
			require.Nil(t, targets, "a stale active candidate must be skipped after its rollout finishes")
			after, err := q.ListFirmwareRolloutDevices(ctx, started.ID)
			require.NoError(t, err)
			require.Equal(t, before, after)
			unchanged, err := q.GetFirmwareRollout(ctx, sqlc.GetFirmwareRolloutParams{RolloutID: started.ID, OrgID: f.orgID})
			require.NoError(t, err)
			require.Equal(t, finished, unchanged, "stale preparation must preserve the terminal revision and timestamps")
		})
	}
}

func TestSettledSnapshotCannotAdvanceRetriedWork(t *testing.T) {
	for _, behavior := range []Behavior{allAtOnce, pilotOf1} {
		t.Run(behavior.Method, func(t *testing.T) {
			f := newFixture(t, 1)
			ctx := t.Context()
			f.channel(t, behavior, "miner-0")
			started := f.apply(t, "fw-2")
			require.NoError(t, f.svc.store.GetQueries(ctx).HaltFirmwareRolloutDevices(ctx, sqlc.HaltFirmwareRolloutDevicesParams{
				RolloutID: started.ID, DeviceIds: []int64{f.deviceIDs["miner-0"]},
				HaltReason: HaltReasonFailed, LastError: "Update attempts exhausted",
			}))
			stale, targets := preparedDispatch(t, ctx, f, started.ID)
			require.True(t, allSettled(reviewScope(stale, targets), stale))
			retried, err := f.svc.RetryFailedDevices(ctx, f.orgID, started.ID, byOperator)
			require.NoError(t, err)
			require.Equal(t, PhaseQueued, phaseOf(*retried, "miner-0"))

			// Retry commits after the old tick saw every target settled. That
			// tick cannot finish the rollout or move its batch to a review gate.
			require.NoError(t, f.svc.enforceRollout(ctx, stale, "Test channel", targets))
			current := f.rollout(t, started.ID)
			require.Equal(t, StatusActive, current.Status)
			require.Equal(t, started.Stage, current.Stage)
			require.Equal(t, PhaseQueued, phaseOf(current, "miner-0"))
			require.Equal(t, retried.Revision, current.Revision)
			require.NotContains(t, f.activity.types(), EventRolloutCompletedWithFailures)
			require.NotContains(t, f.activity.types(), EventRolloutReviewReady)
			f.svc.EnforceTick(ctx)
			require.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers(), "the retried work remains dispatchable")
		})
	}
}
