package rollout

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
)

func TestManualFirmwareGuardRetainsActiveEnrollmentAfterHardwareRename(t *testing.T) {
	for _, pair := range []struct{ name, manufacturer, model string }{
		{"model", "Proto", "Proto Rig"},
		{"manufacturer", "ProtoOS", "Rig"},
		{"both", "ProtoOS", "Proto Rig"},
	} {
		t.Run(pair.name, func(t *testing.T) {
			f := newFixture(t, 1)
			f.channel(t, allAtOnce, "miner-0")
			f.apply(t, "fw-2")
			f.svc.EnforceTick(t.Context())
			setModelTransition(t, f, pair.manufacturer, pair.model)
			filter := command.NewReleaseChannelFirmwareFilter(f.conn)
			input := command.CommandFilterInput{CommandType: commandtype.FirmwareUpdate,
				OrganizationID: f.orgID, DeviceIdentifiers: []string{"miner-0"}}
			out, err := filter.Apply(t.Context(), input)
			require.NoError(t, err)
			require.Empty(t, out.Kept)
			require.Len(t, out.Skipped, 1, "the same paired target is still controlled by its active rollout")
			require.Equal(t, "miner-0", out.Skipped[0].DeviceIdentifier)
			require.Contains(t, out.Skipped[0].Reason, "Test channel")

			_, err = f.svc.ApplyFirmware(t.Context(), f.orgID, testActor, f.channelID, []Assignment{rigAssignment("")}, nil)
			require.NoError(t, err)
			out, err = filter.Apply(t.Context(), input)
			require.NoError(t, err)
			require.Equal(t, []string{"miner-0"}, out.Kept, "a cleared assignment no longer owns the target")
			require.Empty(t, out.Skipped)
		})
	}
}

func TestManualFirmwareGuardIgnoresInactiveEnrollment(t *testing.T) {
	for _, state := range []string{"excluded", "finished", "older generation"} {
		t.Run(state, func(t *testing.T) {
			f := newFixture(t, 1)
			f.channel(t, allAtOnce, "miner-0")
			started := f.apply(t, "fw-2")
			f.svc.EnforceTick(t.Context())
			setModelTransition(t, f, "ProtoOS", "Proto Rig")
			var err error
			switch state {
			case "excluded":
				_, err = f.conn.ExecContext(t.Context(), `UPDATE firmware_rollout_device SET excluded_at = now() WHERE rollout_id = $1`, started.ID)
			case "finished":
				_, err = f.conn.ExecContext(t.Context(), `UPDATE firmware_rollout SET status = 'completed', finished_at = now() WHERE id = $1`, started.ID)
			case "older generation":
				_, err = f.conn.ExecContext(t.Context(), `UPDATE release_channel_firmware SET assignment_generation = assignment_generation + 1 WHERE channel_id = $1`, f.channelID)
			}
			require.NoError(t, err)
			out, err := command.NewReleaseChannelFirmwareFilter(f.conn).Apply(t.Context(), command.CommandFilterInput{
				CommandType: commandtype.FirmwareUpdate, OrganizationID: f.orgID, DeviceIdentifiers: []string{"miner-0"},
			})
			require.NoError(t, err)
			require.Equal(t, []string{"miner-0"}, out.Kept)
			require.Empty(t, out.Skipped)
		})
	}
}
