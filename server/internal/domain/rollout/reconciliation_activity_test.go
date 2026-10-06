package rollout

import (
	"testing"

	"github.com/stretchr/testify/require"

	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
)

func TestReconciliationActivityIncludesCurrentChannelName(t *testing.T) {
	for _, trigger := range []string{"drift", "late joiner"} {
		t.Run(trigger, func(t *testing.T) {
			f := newFixture(t, 1)
			f.channel(t, allAtOnce, "miner-0")
			initial := f.apply(t, "fw-2")
			f.svc.EnforceTick(t.Context())
			f.finishUpdate(t, "miner-0", "2.0.0")
			f.svc.EnforceTick(t.Context())
			require.Equal(t, StatusCompleted, f.rollout(t, initial.ID).Status)
			if trigger == "drift" {
				f.setReportedVersion(t, "miner-0", "1.0.0")
			} else {
				f.addMiner(t, "miner-1", "Rig")
			}
			const currentName = "Canary / Oslo"
			_, err := f.svc.UpdateChannel(t.Context(), f.orgID, f.channelID, ChannelSpec{
				Name: currentName, Scope: Scope{DeviceIdentifiers: f.allMiners()}, Behavior: allAtOnce,
			})
			require.NoError(t, err)
			f.activity.events = nil
			f.svc.startNeededRollouts(t.Context())
			reconciled := f.latestRollout(t)
			require.NotEqual(t, initial.ID, reconciled.ID)
			require.Len(t, f.activity.events, 1)
			event := f.activity.events[0]
			require.Equal(t, EventRolloutStarted, event.Type)
			require.Equal(t, activitymodels.ActorSystem, event.ActorType)
			require.Equal(t, "Started firmware update: Canary / Oslo Proto Rig → 2.0.0", event.Description)
			require.Equal(t, currentName, event.Metadata["channel_name"])
			require.Equal(t, f.channelID, event.Metadata["channel_id"])
			require.Equal(t, reconciled.ID, event.Metadata["rollout_id"])
			require.Equal(t, true, event.Metadata["reconciliation"])
		})
	}
}
