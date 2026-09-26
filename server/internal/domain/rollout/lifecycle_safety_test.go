package rollout

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAutoContinueRequiresPostUpdateTelemetry(t *testing.T) {
	f := newFixture(t, 2)
	f.channel(t, Behavior{Method: MethodPilotThenContinue, PilotSize: 1, AutoContinue: true,
		Thresholds: Thresholds{MaxHashrateDropPercent: ptr(10.0)}}, f.allMiners()...)
	started := f.apply(t, "fw-2")
	f.svc.EnforceTick(t.Context())
	f.finishUpdate(t, "miner-0", "2.0.0")
	var after int
	require.NoError(t, f.conn.QueryRowContext(t.Context(), `SELECT count(*) FROM device_metrics m
  JOIN device d ON d.device_identifier = m.device_identifier
  JOIN firmware_rollout_device rd ON rd.device_id = d.id
  WHERE rd.rollout_id = $1 AND d.device_identifier = 'miner-0' AND m.time >= rd.last_dispatched_at`, started.ID).Scan(&after))
	require.Zero(t, after, "only the pre-update baseline sample exists")
	f.svc.EnforceTick(t.Context())
	gated := f.rollout(t, started.ID)
	require.Equal(t, StageAwaitingReview, gated.Stage)
	require.Equal(t, PhaseDone, phaseOf(gated, "miner-0"), "progress remains verified while telemetry catches up")
	require.False(t, gated.Evidence.ReadyToAdvance)
	require.Zero(t, gated.Evidence.HashRateHs.SampledDevices)
	require.Contains(t, gated.Evidence.HoldReason, "sample covers 0 of 1")
	f.svc.EnforceTick(t.Context())
	require.Equal(t, StageAwaitingReview, f.rollout(t, started.ID).Stage)

	f.reportHashrate(t, "miner-0", 100)
	require.True(t, f.rollout(t, started.ID).Evidence.ReadyToAdvance)
	f.svc.EnforceTick(t.Context())
	require.Equal(t, StageRest, f.rollout(t, started.ID).Stage)
}

func TestAutoContinueRequiresCurrentRecoveryWithoutRewritingDone(t *testing.T) {
	for _, status := range []string{"OFFLINE", "NEEDS_MINING_POOL"} {
		t.Run(status, func(t *testing.T) {
			f := newFixture(t, 2)
			f.channel(t, Behavior{Method: MethodPilotThenContinue, PilotSize: 1, AutoContinue: true, StabilizationSeconds: 60,
				Thresholds: Thresholds{MaxHashrateDropPercent: ptr(10.0)}}, f.allMiners()...)
			started := f.apply(t, "fw-2")
			f.svc.EnforceTick(t.Context())
			f.finishUpdate(t, "miner-0", "2.0.0")
			f.svc.EnforceTick(t.Context())
			f.reportHashrate(t, "miner-0", 100)
			require.Equal(t, StageAwaitingReview, f.rollout(t, started.ID).Stage)
			f.setStatus(t, "miner-0", status)
			f.advanceClock(61 * time.Second)
			held := f.rollout(t, started.ID)
			require.Equal(t, PhaseDone, phaseOf(held, "miner-0"))
			require.False(t, held.Evidence.ReadyToAdvance)
			require.Contains(t, held.Evidence.HoldReason, "Waiting for")
			f.svc.EnforceTick(t.Context())
			require.Equal(t, StageAwaitingReview, f.rollout(t, started.ID).Stage)
			require.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers())

			f.setStatus(t, "miner-0", "ACTIVE")
			require.True(t, f.rollout(t, started.ID).Evidence.ReadyToAdvance)
			f.svc.EnforceTick(t.Context())
			require.Equal(t, StageRest, f.rollout(t, started.ID).Stage,
				"stabilization is a dwell period, not a newly restarted continuous-health timer")
		})
	}
}

func TestAutoContinuePreservesNonHashingBaselineException(t *testing.T) {
	f := newFixture(t, 2)
	f.setStatus(t, "miner-0", "NEEDS_MINING_POOL")
	f.channel(t, Behavior{Method: MethodPilotThenContinue, PilotSize: 1, AutoContinue: true}, f.allMiners()...)
	started := f.apply(t, "fw-2")
	f.svc.EnforceTick(t.Context())
	f.setReportedVersion(t, "miner-0", "2.0.0")
	f.svc.EnforceTick(t.Context())
	gated := f.rollout(t, started.ID)
	require.Equal(t, PhaseDone, phaseOf(gated, "miner-0"))
	require.Zero(t, gated.Evidence.Hashing)
	require.True(t, gated.Evidence.ReadyToAdvance, "an idle baseline does not require newly configured mining")
}

func TestReconciliationPreservesChannelPacing(t *testing.T) {
	for _, b := range []Behavior{
		{Method: MethodBatched, BatchSize: 1, WaitBetweenBatchesSeconds: 60},
		{Method: MethodPilotThenContinue, PilotSize: 1},
	} {
		t.Run(b.Method, func(t *testing.T) {
			f := newFixture(t, 3)
			f.channel(t, b)
			started, err := f.svc.ApplyFirmware(t.Context(), f.orgID, testActor, f.channelID, []Assignment{rigAssignment("fw-2")}, nil)
			require.NoError(t, err)
			require.Empty(t, started)
			_, err = f.svc.UpdateChannel(t.Context(), f.orgID, f.channelID, ChannelSpec{
				Name: "Test channel", Scope: Scope{DeviceIdentifiers: f.allMiners()}, Behavior: b,
			})
			require.NoError(t, err)
			f.svc.EnforceTick(t.Context())
			r := f.latestRollout(t)
			require.Equal(t, b.Method, r.Behavior.Method)
			require.Equal(t, StageBatch, r.Stage)
			require.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers())
			require.Empty(t, r.Evidence.HoldReason, "ordinary dispatch does not claim an actionable blocker")
			f.finishUpdate(t, "miner-0", "2.0.0")
			f.svc.EnforceTick(t.Context())
			if b.Method == MethodBatched {
				require.Equal(t, StageWaiting, f.rollout(t, r.ID).Stage)
			} else {
				require.Equal(t, StageAwaitingReview, f.rollout(t, r.ID).Stage)
			}
			require.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers())
		})
	}
}

func TestCompletedDepartedTargetCannotReacquireOfflineCapacity(t *testing.T) {
	f := newFixture(t, 2)
	b := Behavior{Method: MethodAllAtOnce, MaxConcurrentOffline: 1}
	f.channel(t, b, "miner-0")
	first := f.apply(t, "fw-2")
	f.svc.EnforceTick(t.Context())
	f.finishUpdate(t, "miner-0", "2.0.0")
	f.svc.EnforceTick(t.Context())
	require.Equal(t, StatusCompleted, f.rollout(t, first.ID).Status)
	f.svc.EnforceTick(t.Context())
	var reservations int
	require.NoError(t, f.conn.QueryRowContext(t.Context(), `SELECT count(*) FROM firmware_rollout_reservation`).Scan(&reservations))
	require.Zero(t, reservations)
	_, err := f.svc.UpdateChannel(t.Context(), f.orgID, f.channelID, ChannelSpec{
		Name: "Test channel", Scope: Scope{DeviceIdentifiers: []string{"miner-1"}}, Behavior: b,
	})
	require.NoError(t, err)
	f.setStatus(t, "miner-0", "OFFLINE")
	f.svc.EnforceTick(t.Context())
	next := f.latestRollout(t)
	require.NotEqual(t, first.ID, next.ID)
	require.Len(t, next.Devices, 1)
	require.Equal(t, "miner-1", next.Devices[0].DeviceIdentifier)
	require.Equal(t, []string{"miner-0", "miner-1"}, f.dispatcher.sentIdentifiers())
	require.Empty(t, next.Evidence.HoldReason)
}

func TestDispatchBlockersAreVisibleAndClearOnRecovery(t *testing.T) {
	t.Run("artifact", func(t *testing.T) {
		f := newFixture(t, 1)
		f.channel(t, allAtOnce, f.allMiners()...)
		started := f.apply(t, "fw-2")
		f.files.deleted["fw-2"] = true
		f.svc.EnforceTick(t.Context())
		held := f.rollout(t, started.ID)
		require.Equal(t, StatusActive, held.Status)
		require.Equal(t, holdArtifactMissing, held.Evidence.HoldReason)
		require.Empty(t, f.dispatcher.sentIdentifiers())
		delete(f.files.deleted, "fw-2")
		require.Empty(t, f.rollout(t, started.ID).Evidence.HoldReason)
		f.svc.EnforceTick(t.Context())
		require.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers())
	})
	t.Run("offline capacity", func(t *testing.T) {
		f := newFixture(t, 2)
		useQueuedBudgetDispatcher(f)
		f.channel(t, Behavior{Method: MethodAllAtOnce, MaxConcurrentOffline: 1}, f.allMiners()...)
		started := f.apply(t, "fw-2")
		f.svc.EnforceTick(t.Context())
		held := f.rollout(t, started.ID)
		require.Contains(t, held.Evidence.HoldReason, "Waiting for offline capacity: 1 of 1")
		f.finishQueuedBudgetCommand(t, "miner-0", "SUCCESS")
		f.finishUpdate(t, "miner-0", "2.0.0")
		f.svc.EnforceTick(t.Context())
		require.Equal(t, []string{"miner-0", "miner-1"}, f.dispatcher.sentIdentifiers())
		require.Empty(t, f.rollout(t, started.ID).Evidence.HoldReason)
	})
}
