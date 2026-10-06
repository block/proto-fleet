package rollout

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func evidenceGroupChannel(t *testing.T, f *fixture, behavior Behavior, miners ...string) int64 {
	t.Helper()
	group := f.addGroup(t, "Evidence miners")
	for _, miner := range miners {
		f.placeInSet(t, group, "group", miner)
	}
	ch, err := f.svc.CreateChannel(t.Context(), f.orgID, 1, ChannelSpec{
		Name: "Evidence channel", Scope: Scope{GroupIDs: []int64{group}}, Behavior: behavior,
	})
	require.NoError(t, err)
	f.channelID = ch.ID
	return group
}

func TestAllAtOnceEvidenceKeepsExcludedTargets(t *testing.T) {
	f := newFixture(t, 3)
	ctx := t.Context()
	group := evidenceGroupChannel(t, f, allAtOnce, f.allMiners()...)
	started := f.apply(t, "fw-2")
	f.svc.EnforceTick(ctx)
	f.finishUpdate(t, "miner-0", "2.0.0")
	f.svc.EnforceTick(ctx)
	require.Equal(t, PhaseDone, phaseOf(f.rollout(t, started.ID), "miner-0"))

	// A previously verified miner remains in the target count after leaving,
	// but its old verification and samples no longer count as current evidence.
	f.removeFromSet(t, group, "miner-0")
	f.svc.EnforceTick(ctx)
	listed := f.rollout(t, started.ID)
	got, err := f.svc.GetRollout(ctx, f.orgID, started.ID)
	require.NoError(t, err)
	for _, r := range []Rollout{listed, *got} {
		require.NotNil(t, r.Evidence)
		assert.Equal(t, int32(3), r.Evidence.DevicesTotal)
		assert.Equal(t, int32(1), r.Evidence.Excluded)
		assert.Equal(t, int32(1), r.DeviceCounts.Excluded)
		assert.Zero(t, r.Evidence.Verified)
		assert.Zero(t, r.Evidence.HashRateHs.SampledDevices)
		assert.Equal(t, int32(2), r.Evidence.Online)
		assert.Equal(t, StatusActive, r.Status)
	}

	f.finishUpdate(t, "miner-1", "2.0.0")
	f.finishUpdate(t, "miner-2", "2.0.0")
	f.svc.EnforceTick(ctx)
	assert.Equal(t, StatusCompleted, f.rollout(t, started.ID).Status,
		"departed targets must not hold operational completion")
}

func TestBatchEvidenceTreatsExcludedTargetsAsNeutral(t *testing.T) {
	for _, behavior := range []Behavior{
		{Method: MethodPilotThenContinue, PilotSize: 2, AutoContinue: true},
		{Method: MethodBatched, BatchSize: 2, ReviewAfterEachBatch: true, AutoContinue: true},
	} {
		t.Run(behavior.Method, func(t *testing.T) {
			f := newFixture(t, 4)
			ctx := t.Context()
			behavior.Thresholds.MaxHashrateDropPercent = ptr(10.0)
			group := evidenceGroupChannel(t, f, behavior, f.allMiners()...)
			started := f.apply(t, "fw-2")
			f.svc.EnforceTick(ctx)
			f.finishUpdate(t, "miner-0", "2.0.0")
			f.svc.EnforceTick(ctx)
			require.Equal(t, PhaseDone, phaseOf(f.rollout(t, started.ID), "miner-0"))

			f.removeFromSet(t, group, "miner-0")
			f.setStatus(t, "miner-0", "OFFLINE")
			f.reportHashrate(t, "miner-0", 0)
			f.finishUpdate(t, "miner-1", "2.0.0")
			f.svc.EnforceTick(ctx)
			f.reportHashrate(t, "miner-1", 100)
			gated := f.rollout(t, started.ID)
			require.Equal(t, StageAwaitingReview, gated.Stage)
			require.NotNil(t, gated.Evidence)
			assert.Equal(t, int32(2), gated.Evidence.DevicesTotal)
			assert.Equal(t, int32(1), gated.Evidence.Excluded)
			assert.Equal(t, gated.CurrentBatchCounts.Excluded, gated.Evidence.Excluded)
			assert.Equal(t, int32(1), gated.Evidence.Verified)
			assert.Equal(t, int32(1), gated.Evidence.HashRateHs.SampledDevices)
			require.NotNil(t, gated.Evidence.HashRateHs.Current)
			assert.Equal(t, 100.0, *gated.Evidence.HashRateHs.Current)
			assert.True(t, gated.Evidence.ReadyToAdvance,
				"excluded miners contribute neither missing coverage nor degraded samples")
			assert.Empty(t, gated.Evidence.HoldReason)

			f.svc.EnforceTick(ctx)
			assert.NotEqual(t, StageAwaitingReview, f.rollout(t, started.ID).Stage,
				"reported readiness must agree with auto-continue")
		})
	}
}

func TestBatchedRestEvidenceCoversOnlyRestTargets(t *testing.T) {
	for _, behavior := range []Behavior{
		{Method: MethodPilotThenContinue, PilotSize: 1},
		{Method: MethodBatched, BatchSize: 1, ReviewAfterEachBatch: true},
	} {
		t.Run(behavior.Method, func(t *testing.T) {
			f := newFixture(t, 3)
			ctx := t.Context()
			initial := f.allMiners()
			if behavior.Method == MethodBatched {
				initial = []string{"miner-0"}
			}
			group := evidenceGroupChannel(t, f, behavior, initial...)
			started := f.apply(t, "fw-2")
			f.svc.EnforceTick(ctx)
			f.finishUpdate(t, "miner-0", "2.0.0")
			f.svc.EnforceTick(ctx)
			require.Equal(t, StageAwaitingReview, f.rollout(t, started.ID).Stage)
			if behavior.Method == MethodBatched {
				// Late joiners are rest targets even when the initial rollout
				// assigned every original target to a batch.
				f.placeInSet(t, group, "group", "miner-1")
				f.placeInSet(t, group, "group", "miner-2")
				f.svc.EnforceTick(ctx)
			}
			f.removeFromSet(t, group, "miner-0")
			f.removeFromSet(t, group, "miner-1")
			f.svc.EnforceTick(ctx)

			rest, err := f.svc.ContinueRollout(ctx, f.orgID, started.ID, byOperator)
			require.NoError(t, err)
			require.Equal(t, StageRest, rest.Stage)
			require.NotNil(t, rest.Evidence)
			assert.Len(t, rest.Devices, 3)
			assert.Equal(t, int32(2), rest.DeviceCounts.Excluded)
			assert.Equal(t, int32(2), rest.Evidence.DevicesTotal,
				"earlier batches are outside rest evidence")
			assert.Equal(t, int32(1), rest.Evidence.Excluded)
			assert.Zero(t, rest.Evidence.Verified)

			f.svc.EnforceTick(ctx)
			f.finishUpdate(t, "miner-2", "2.0.0")
			f.svc.EnforceTick(ctx)
			assert.Equal(t, StatusCompleted, f.rollout(t, started.ID).Status)
		})
	}
}
