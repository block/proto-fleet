package rollout

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAllAtOnceRolloutPathsSnapshotCurrentChannelBudget(t *testing.T) {
	for _, trigger := range []string{"reconciliation", "retry", "rollback"} {
		t.Run(trigger, func(t *testing.T) {
			f := newFixture(t, 1)
			f.channel(t, Behavior{Method: MethodAllAtOnce, MaxConcurrentOffline: 2}, "miner-0")
			first := f.apply(t, "fw-1")
			require.Equal(t, int32(2), first.Behavior.MaxConcurrentOffline)
			f.svc.EnforceTick(t.Context())
			f.finishUpdate(t, "miner-0", "1.5.0")
			f.svc.EnforceTick(t.Context())
			require.Equal(t, StatusCompleted, f.rollout(t, first.ID).Status)
			second := f.apply(t, "fw-2")
			require.Equal(t, int32(2), second.Behavior.MaxConcurrentOffline)
			_, err := f.svc.UpdateChannel(t.Context(), f.orgID, f.channelID, ChannelSpec{
				Name: "Test channel", Scope: Scope{DeviceIdentifiers: []string{"miner-0"}},
				Behavior: Behavior{Method: MethodAllAtOnce, MaxConcurrentOffline: 3},
			})
			require.NoError(t, err)
			var next Rollout
			switch trigger {
			case "reconciliation":
				f.svc.EnforceTick(t.Context())
				f.finishUpdate(t, "miner-0", "2.0.0")
				f.svc.EnforceTick(t.Context())
				require.Equal(t, StatusCompleted, f.rollout(t, second.ID).Status)
				f.setReportedVersion(t, "miner-0", "1.0.0")
				f.svc.EnforceTick(t.Context())
				next = f.latestRollout(t)
			case "retry":
				_, _, err := f.svc.CancelRollout(t.Context(), f.orgID, second.ID, byOperator)
				require.NoError(t, err)
				retried, err := f.svc.RetryFailedDevices(t.Context(), f.orgID, second.ID, byOperator)
				require.NoError(t, err)
				next = *retried
			case "rollback":
				f.svc.EnforceTick(t.Context())
				f.finishUpdate(t, "miner-0", "2.0.0")
				f.svc.EnforceTick(t.Context())
				_, restored, err := f.svc.RollbackFirmware(t.Context(), f.orgID, second.ID, byOperator)
				require.NoError(t, err)
				require.Len(t, restored, 1)
				next = restored[0]
			}
			require.NotEqual(t, second.ID, next.ID)
			require.Equal(t, MethodAllAtOnce, next.Behavior.Method)
			require.Equal(t, int32(3), next.Behavior.MaxConcurrentOffline, "new history records the limit in force when it starts")
			require.Equal(t, int32(2), f.rollout(t, second.ID).Behavior.MaxConcurrentOffline, "changing the live limit does not rewrite an existing snapshot")
		})
	}
}
