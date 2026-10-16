package command

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

func TestScheduleContainerFilterOnlyChecksScheduledCommands(t *testing.T) {
	filter := NewScheduleContainerFilter(nil)
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
}

func TestScheduleContainerFilterLookupFailureIsAnError(t *testing.T) {
	conn, err := sql.Open("pgx", "")
	require.NoError(t, err)
	require.NoError(t, conn.Close())
	out, err := NewScheduleContainerFilter(conn).Apply(t.Context(), CommandFilterInput{
		Actor: session.ActorScheduler, CommandType: commandtype.SetPowerTarget,
		OrganizationID: 1, DeviceIdentifiers: []string{"miner"},
	})
	require.Error(t, err)
	require.Empty(t, out.Kept)
	require.Empty(t, out.Skipped, "a failed lookup must remain retryable, not become a successful skip")
}
