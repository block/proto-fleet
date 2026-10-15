package command

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

func TestScheduleCapabilityFilterOnlyChecksScheduledCommands(t *testing.T) {
	filter := NewScheduleCapabilityFilter(nil, nil)
	for _, tc := range []struct {
		name  string
		actor session.Actor
		kind  commandtype.Type
	}{
		{"manual", "", commandtype.StopMining},
		{"curtailment", session.ActorCurtailment, commandtype.Curtail},
		{"container control", session.ActorScheduler, commandtype.SetTankMining},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := filter.Apply(t.Context(), CommandFilterInput{
				Actor: tc.actor, CommandType: tc.kind, DeviceIdentifiers: []string{"device"},
			})
			require.NoError(t, err)
			require.Equal(t, []string{"device"}, out.Kept)
			require.Empty(t, out.Skipped)
		})
	}
	_, err := filter.Apply(t.Context(), CommandFilterInput{
		Actor: session.ActorScheduler, CommandType: commandtype.StopMining, DeviceIdentifiers: []string{"device"},
	})
	require.ErrorContains(t, err, "schedule capability provider is not configured")
}
