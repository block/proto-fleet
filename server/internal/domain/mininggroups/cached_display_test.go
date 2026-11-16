package mininggroups

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRepeatedObservationFailuresRetainDisplayWithoutWriteReadiness(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		name := "disabled"
		if enabled {
			name = "enabled"
		}
		t.Run(name, func(t *testing.T) {
			ctx, f := newIdleRetryFixture(t)
			f.miner.enabled = enabled
			cached, err := f.service.GetState(ctx, f.target.DeviceIdentifier)
			require.NoError(t, err)
			require.True(t, cached.Record.ObservedAt.Valid)
			f.miner.fail = true
			for range 3 {
				state, err := f.service.GetState(ctx, f.target.DeviceIdentifier)
				require.NoError(t, err, "a retained snapshot remains available after repeated failed reads")
				require.Equal(t, cached.Observed, state.Observed)
				require.JSONEq(t, string(cached.Record.Observed), string(state.Record.Observed))
				require.False(t, state.Record.ObservedAt.Valid, "the display snapshot cannot make mode changes ready")
				require.Contains(t, state.DeactivationBlockedReason, "Reconnect")
				require.False(t, f.state(t).ObservedAt.Valid)
				requireAllocationReadiness(t, ctx, f, false)
			}
			f.miner.fail = false
			refreshed, err := f.service.GetState(ctx, f.target.DeviceIdentifier)
			require.NoError(t, err)
			require.True(t, refreshed.Record.ObservedAt.Valid, "a new successful observation restores freshness")
			require.Equal(t, enabled, refreshed.Observed.Enabled)
		})
	}
}

func TestFailedObservationWithoutSupportedSnapshotReturnsError(t *testing.T) {
	ctx, f := newIdleRetryFixture(t)
	_, err := f.conn.ExecContext(ctx, `UPDATE container_mining_group_state SET observed='{}',observed_at=NULL WHERE device_id=$1`, f.target.DeviceID)
	require.NoError(t, err)
	f.miner.fail = true
	_, err = f.service.GetState(ctx, f.target.DeviceIdentifier)
	require.ErrorContains(t, err, "controller unavailable")
}
