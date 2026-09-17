package rollout

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
)

func cancellationScope(t *testing.T, f *fixture, miners ...string) {
	t.Helper()
	_, err := f.svc.UpdateChannel(t.Context(), f.orgID, f.channelID, ChannelSpec{
		Name: "Test channel", Scope: Scope{DeviceIdentifiers: miners}, Behavior: pilotOf1,
	})
	require.NoError(t, err)
}

func TestCancelRemainingSuppressesExcludedQueuedTargetOnReentry(t *testing.T) {
	f := newFixture(t, 3)
	f.channel(t, pilotOf1, f.allMiners()...)
	started := f.apply(t, "fw-2")
	f.svc.EnforceTick(t.Context())
	f.finishUpdate(t, "miner-0", "2.0.0")
	f.svc.EnforceTick(t.Context())
	require.Equal(t, StageAwaitingReview, f.rollout(t, started.ID).Stage)

	// The queued target leaves before the operator cancels. Exclusion is a
	// membership fact and must not remove it from cancellation's remaining work.
	cancellationScope(t, f, "miner-0", "miner-2")
	f.svc.EnforceTick(t.Context())
	require.Equal(t, PhaseExcluded, phaseOf(f.rollout(t, started.ID), "miner-1"))
	canceled, _, err := f.svc.CancelRollout(t.Context(), f.orgID, started.ID, byOperator)
	require.NoError(t, err)
	require.Equal(t, PhaseExcluded, phaseOf(*canceled, "miner-1"), "the halt does not rewrite the displayed exclusion")
	var reason string
	var haltedAt sql.NullTime
	require.NoError(t, f.conn.QueryRowContext(t.Context(), `SELECT halt_reason, halted_at FROM firmware_rollout_device
		WHERE rollout_id = $1 AND device_id = $2`, started.ID, f.deviceIDs["miner-1"]).Scan(&reason, &haltedAt))
	require.Equal(t, HaltReasonCanceled, reason)
	require.True(t, haltedAt.Valid)

	cancellationScope(t, f, f.allMiners()...)
	for range 2 {
		f.svc.EnforceTick(t.Context())
	}
	require.Equal(t, started.ID, f.latestRollout(t).ID, "reentry at the same assignment must not restart canceled work")
	require.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers())

	// An explicit retry reactivates both canceled queued targets. The old
	// rollout's exclusion remains historical; the replacement has fresh scope.
	retried, err := f.svc.RetryFailedDevices(t.Context(), f.orgID, started.ID, byOperator)
	require.NoError(t, err)
	require.NotEqual(t, started.ID, retried.ID)
	require.Len(t, retried.Devices, 2)
	require.Equal(t, PhaseQueued, phaseOf(*retried, "miner-1"))
	f.svc.EnforceTick(t.Context())
	require.Equal(t, []string{"miner-0", "miner-1", "miner-2"}, f.dispatcher.sentIdentifiers())
	require.Equal(t, PhaseExcluded, phaseOf(f.rollout(t, started.ID), "miner-1"))
}

func TestCancelRemainingDoesNotSuppressExcludedCompletedTarget(t *testing.T) {
	f := newFixture(t, 2)
	f.channel(t, pilotOf1, f.allMiners()...)
	started := f.apply(t, "fw-2")
	f.svc.EnforceTick(t.Context())
	f.finishUpdate(t, "miner-0", "2.0.0")
	f.svc.EnforceTick(t.Context())
	require.Equal(t, PhaseDone, phaseOf(f.rollout(t, started.ID), "miner-0"))
	cancellationScope(t, f, "miner-1")
	f.svc.EnforceTick(t.Context())
	canceled, _, err := f.svc.CancelRollout(t.Context(), f.orgID, started.ID, byOperator)
	require.NoError(t, err)
	require.Equal(t, PhaseExcluded, phaseOf(*canceled, "miner-0"))
	var haltedAt, verifiedAt sql.NullTime
	require.NoError(t, f.conn.QueryRowContext(t.Context(), `SELECT halted_at, verified_at FROM firmware_rollout_device
		WHERE rollout_id = $1 AND device_id = $2`, started.ID, f.deviceIDs["miner-0"]).Scan(&haltedAt, &verifiedAt))
	require.False(t, haltedAt.Valid, "cancel remaining preserves already completed updates, including excluded ones")
	require.True(t, verifiedAt.Valid)

	// Previously completed work retains normal drift correction on reentry;
	// only the target whose queued update was canceled stays suppressed.
	f.setReportedVersion(t, "miner-0", "1.0.0")
	cancellationScope(t, f, f.allMiners()...)
	f.svc.EnforceTick(t.Context())
	reconciled := f.latestRollout(t)
	require.NotEqual(t, started.ID, reconciled.ID)
	require.Len(t, reconciled.Devices, 1)
	require.Equal(t, "miner-0", reconciled.Devices[0].DeviceIdentifier)
	require.Equal(t, []string{"miner-0", "miner-0"}, f.dispatcher.sentIdentifiers())
}
