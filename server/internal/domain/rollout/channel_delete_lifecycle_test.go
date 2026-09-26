package rollout

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeleteChannelRejectsActiveAndPausedRollouts(t *testing.T) {
	f := newFixture(t, 1)
	channel := f.channel(t, allAtOnce, "miner-0")
	started := f.apply(t, "fw-2")
	require.ErrorContains(t, f.svc.DeleteChannel(t.Context(), f.orgID, channel.ID), "active firmware update")
	_, err := f.svc.PauseRollout(t.Context(), f.orgID, started.ID, byOperator)
	require.NoError(t, err)
	require.ErrorContains(t, f.svc.DeleteChannel(t.Context(), f.orgID, channel.ID), "active firmware update")
	_, _, err = f.svc.CancelRollout(t.Context(), f.orgID, started.ID, byOperator)
	require.NoError(t, err)
	require.NoError(t, f.svc.DeleteChannel(t.Context(), f.orgID, channel.ID), "canceling unsent work makes deletion safe")
}

func TestDeleteChannelPreservesCanceledCommandsUntilTheyFinish(t *testing.T) {
	for _, status := range []string{"PENDING", "PROCESSING"} {
		t.Run(status, func(t *testing.T) {
			f := newFixture(t, 1)
			useQueuedBudgetDispatcher(f)
			channel := f.channel(t, allAtOnce, "miner-0")
			started := f.apply(t, "fw-2")
			f.svc.EnforceTick(t.Context())
			f.finishQueuedBudgetCommand(t, "miner-0", status)
			_, _, err := f.svc.CancelRollout(t.Context(), f.orgID, started.ID, byOperator)
			require.NoError(t, err)
			require.ErrorContains(t, f.svc.DeleteChannel(t.Context(), f.orgID, channel.ID), "commands for this channel are still running")
			_, err = f.svc.GetRollout(t.Context(), f.orgID, started.ID)
			require.NoError(t, err, "rejected deletion must retain the command's rollout history")
			f.finishUpdate(t, "miner-0", "2.0.0")
			f.finishQueuedBudgetCommand(t, "miner-0", "SUCCESS")
			f.svc.EnforceTick(t.Context())
			require.NoError(t, f.svc.DeleteChannel(t.Context(), f.orgID, channel.ID))
			var checksum string
			require.NoError(t, f.conn.QueryRowContext(t.Context(), `SELECT firmware_checksum FROM device_firmware_deployment
				WHERE device_id = $1`, f.deviceIDs["miner-0"]).Scan(&checksum))
			require.Equal(t, checksum2, checksum, "successful deployment evidence survives eventual deletion")
		})
	}
}
