package rollout

import (
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/stretchr/testify/require"
)

func TestTerminalRetryPreservesOriginalRecoveryBaseline(t *testing.T) {
	f := newFixture(t, 1)
	f.channel(t, allAtOnce, "miner-0")
	first := f.apply(t, "fw-2")
	f.svc.EnforceTick(t.Context())
	f.setReportedVersion(t, "miner-0", "2.0.0")
	f.setStatus(t, "miner-0", "NEEDS_MINING_POOL")
	for range MaxAttempts + 1 {
		f.backdateSends(t)
		f.svc.EnforceTick(t.Context())
	}
	failed := f.rollout(t, first.ID)
	require.Equal(t, StatusCompletedWithFailures, failed.Status)
	require.Equal(t, PhaseFailed, phaseOf(failed, "miner-0"))
	q := f.svc.store.GetQueries(t.Context())
	original, err := q.ListFirmwareRolloutDevices(t.Context(), first.ID)
	require.NoError(t, err)
	require.Len(t, original, 1)

	retried, err := f.svc.RetryFailedDevices(t.Context(), f.orgID, first.ID, byOperator)
	require.NoError(t, err)
	require.NotEqual(t, first.ID, retried.ID)
	targets, err := q.ListFirmwareRolloutDevices(t.Context(), retried.ID)
	require.NoError(t, err)
	require.Len(t, targets, 1)
	require.Equal(t, original[0].BaselineStatus, targets[0].BaselineStatus)
	require.Equal(t, original[0].BaselineAt, targets[0].BaselineAt)
	require.Equal(t, original[0].BaselineHashRateHs, targets[0].BaselineHashRateHs)
	f.svc.EnforceTick(t.Context())
	current := f.rollout(t, retried.ID)
	require.Equal(t, StatusActive, current.Status, "retry must not accept the update's degraded state as its baseline")
	require.NotEqual(t, PhaseDone, phaseOf(current, "miner-0"))

	f.setStatus(t, "miner-0", "ACTIVE")
	f.svc.EnforceTick(t.Context())
	require.Equal(t, StatusCompleted, f.rollout(t, retried.ID).Status)
}

func TestActiveRetryPreservesEarlierSuppressedTargetsBaseline(t *testing.T) {
	f := newFixture(t, 2)
	channel := f.channel(t, allAtOnce, "miner-0")
	first := f.apply(t, "fw-2")
	f.svc.EnforceTick(t.Context())
	f.setStatus(t, "miner-0", "NEEDS_MINING_POOL")
	_, _, err := f.svc.CancelRollout(t.Context(), f.orgID, first.ID, byOperator)
	require.NoError(t, err)
	_, err = f.svc.UpdateChannel(t.Context(), f.orgID, channel.ID, ChannelSpec{
		Name: channel.Name, Scope: Scope{DeviceIdentifiers: f.allMiners()}, Behavior: allAtOnce,
	})
	require.NoError(t, err)
	f.svc.EnforceTick(t.Context())
	active := f.latestRollout(t)
	require.NotEqual(t, first.ID, active.ID)
	require.Len(t, active.Devices, 1)
	retried, err := f.svc.RetryFailedDevices(t.Context(), f.orgID, active.ID, byOperator)
	require.NoError(t, err)
	require.Equal(t, active.ID, retried.ID)
	q := f.svc.store.GetQueries(t.Context())
	original, err := q.ListFirmwareRolloutDevices(t.Context(), first.ID)
	require.NoError(t, err)
	targets, err := q.ListFirmwareRolloutDevices(t.Context(), active.ID)
	require.NoError(t, err)
	for _, target := range targets {
		if target.DeviceID == f.deviceIDs["miner-0"] {
			require.Equal(t, original[0].BaselineStatus, target.BaselineStatus)
			require.Equal(t, original[0].BaselineAt, target.BaselineAt)
			return
		}
	}
	t.Fatal("retry did not add the earlier suppressed target")
}

func TestTerminalRetryKeepsMissingBaselineForLateJoiner(t *testing.T) {
	f := newFixture(t, 1)
	f.channel(t, allAtOnce, "miner-0")
	first := f.apply(t, "fw-2")
	_, err := f.conn.ExecContext(t.Context(), `UPDATE firmware_rollout_device
		SET baseline_at = NULL, baseline_status = NULL WHERE rollout_id = $1`, first.ID)
	require.NoError(t, err)
	require.NoError(t, f.svc.store.GetQueries(t.Context()).HaltFirmwareRolloutDevices(t.Context(), sqlc.HaltFirmwareRolloutDevicesParams{
		RolloutID: first.ID, DeviceIds: []int64{f.deviceIDs["miner-0"]}, HaltReason: HaltReasonFailed,
	}))
	f.svc.EnforceTick(t.Context())
	f.setStatus(t, "miner-0", "NEEDS_MINING_POOL")
	retried, err := f.svc.RetryFailedDevices(t.Context(), f.orgID, first.ID, byOperator)
	require.NoError(t, err)
	targets, err := f.svc.store.GetQueries(t.Context()).ListFirmwareRolloutDevices(t.Context(), retried.ID)
	require.NoError(t, err)
	require.Len(t, targets, 1)
	require.False(t, targets[0].BaselineAt.Valid, "retry must preserve the no-baseline requirement for current hashing")
	require.False(t, targets[0].BaselineStatus.Valid)
}
