package curtailment

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/modes"
)

func eff(v float64) *float64 { return &v }

// fakeMode is a Mode that records the ranked input it received and returns
// a configurable Result. Lets the selector tests assert on rank order without
// depending on FixedKw's specific stop logic.
type fakeMode struct {
	captured []modes.Candidate
	result   modes.Result
}

func (f *fakeMode) Select(ranked []modes.Candidate) modes.Result {
	f.captured = make([]modes.Candidate, len(ranked))
	copy(f.captured, ranked)
	return f.result
}

func TestBuildPlan_DualSignalFilter_PhantomLoad(t *testing.T) {
	t.Parallel()

	inputs := []CandidateInput{
		{DeviceIdentifier: "phantom", PowerW: 2000, HashRateHS: 0, AvgEfficiencyJH: eff(40)},
	}
	mode := &fakeMode{result: modes.Result{Outcome: modes.OutcomeInsufficientLoad}}

	plan := BuildPlan(inputs, nil, 1500, mode)

	require.Len(t, plan.Skipped, 1)
	assert.Equal(t, SkipPhantomLoadNoHash, plan.Skipped[0].Reason)
	assert.Empty(t, mode.captured, "phantom-load device must not reach mode")
}

func TestBuildPlan_DualSignalFilter_DeadPowerMonitor(t *testing.T) {
	t.Parallel()

	// Hashing but drawing virtually no power — broken power sensor.
	inputs := []CandidateInput{
		{DeviceIdentifier: "dead-monitor", PowerW: 5, HashRateHS: 100e12, AvgEfficiencyJH: eff(40)},
	}
	mode := &fakeMode{result: modes.Result{}}

	plan := BuildPlan(inputs, nil, 1500, mode)

	require.Len(t, plan.Skipped, 1)
	assert.Equal(t, SkipPowerTelemetryUnreliable, plan.Skipped[0].Reason)
}

func TestBuildPlan_DualSignalFilter_BelowThreshold(t *testing.T) {
	t.Parallel()

	// Both signals fail: not hashing AND below floor — most likely fully idle.
	inputs := []CandidateInput{
		{DeviceIdentifier: "idle", PowerW: 5, HashRateHS: 0, AvgEfficiencyJH: nil},
	}
	mode := &fakeMode{result: modes.Result{}}

	plan := BuildPlan(inputs, nil, 1500, mode)

	require.Len(t, plan.Skipped, 1)
	assert.Equal(t, SkipBelowThreshold, plan.Skipped[0].Reason)
}

func TestBuildPlan_PreFilteredSkippedAreForwarded(t *testing.T) {
	t.Parallel()

	pre := []SkippedDevice{
		{DeviceIdentifier: "off1", Reason: SkipUnreachableResidualLoad},
		{DeviceIdentifier: "off2", Reason: SkipUnreachableResidualLoad},
		{DeviceIdentifier: "maint", Reason: SkipMaintenance},
	}
	mode := &fakeMode{result: modes.Result{}}

	plan := BuildPlan(nil, pre, 1500, mode)

	require.Len(t, plan.Skipped, 3)
	// Order preserved.
	assert.Equal(t, "off1", plan.Skipped[0].DeviceIdentifier)
	assert.Equal(t, SkipUnreachableResidualLoad, plan.Skipped[0].Reason)
	assert.Equal(t, "maint", plan.Skipped[2].DeviceIdentifier)
}

func TestBuildPlan_RanksWorstEfficiencyFirst(t *testing.T) {
	t.Parallel()

	inputs := []CandidateInput{
		{DeviceIdentifier: "best", PowerW: 3000, HashRateHS: 100, AvgEfficiencyJH: eff(20)},
		{DeviceIdentifier: "mid", PowerW: 3000, HashRateHS: 100, AvgEfficiencyJH: eff(35)},
		{DeviceIdentifier: "worst", PowerW: 3000, HashRateHS: 100, AvgEfficiencyJH: eff(50)},
	}
	mode := &fakeMode{result: modes.Result{}}

	BuildPlan(inputs, nil, 1500, mode)

	require.Len(t, mode.captured, 3)
	assert.Equal(t, "worst", mode.captured[0].DeviceIdentifier)
	assert.Equal(t, "mid", mode.captured[1].DeviceIdentifier)
	assert.Equal(t, "best", mode.captured[2].DeviceIdentifier)
}

func TestBuildPlan_UnknownEfficiencyRanksLast(t *testing.T) {
	t.Parallel()

	inputs := []CandidateInput{
		{DeviceIdentifier: "unknown1", PowerW: 3000, HashRateHS: 100, AvgEfficiencyJH: nil},
		{DeviceIdentifier: "known", PowerW: 3000, HashRateHS: 100, AvgEfficiencyJH: eff(40)},
		{DeviceIdentifier: "unknown2", PowerW: 3000, HashRateHS: 100, AvgEfficiencyJH: nil},
	}
	mode := &fakeMode{result: modes.Result{}}

	BuildPlan(inputs, nil, 1500, mode)

	require.Len(t, mode.captured, 3)
	assert.Equal(t, "known", mode.captured[0].DeviceIdentifier,
		"known efficiency must rank above unknowns; not silently 0-COALESCED to first")
}

func TestBuildPlan_StableForEqualEfficiency(t *testing.T) {
	t.Parallel()

	// Three equal-efficiency miners — stable sort preserves input order.
	inputs := []CandidateInput{
		{DeviceIdentifier: "a", PowerW: 3000, HashRateHS: 100, AvgEfficiencyJH: eff(30)},
		{DeviceIdentifier: "b", PowerW: 3000, HashRateHS: 100, AvgEfficiencyJH: eff(30)},
		{DeviceIdentifier: "c", PowerW: 3000, HashRateHS: 100, AvgEfficiencyJH: eff(30)},
	}
	mode := &fakeMode{result: modes.Result{}}

	BuildPlan(inputs, nil, 1500, mode)

	require.Len(t, mode.captured, 3)
	assert.Equal(t, "a", mode.captured[0].DeviceIdentifier)
	assert.Equal(t, "b", mode.captured[1].DeviceIdentifier)
	assert.Equal(t, "c", mode.captured[2].DeviceIdentifier)
}

func TestBuildPlan_ReturnsRealizedAndRemainingKW(t *testing.T) {
	t.Parallel()

	// 3 eligible miners @ 3 kW each = 9 kW total.
	// Mode picks first two (6 kW realized); 3 kW remaining.
	inputs := []CandidateInput{
		{DeviceIdentifier: "a", PowerW: 3000, HashRateHS: 100, AvgEfficiencyJH: eff(30)},
		{DeviceIdentifier: "b", PowerW: 3000, HashRateHS: 100, AvgEfficiencyJH: eff(30)},
		{DeviceIdentifier: "c", PowerW: 3000, HashRateHS: 100, AvgEfficiencyJH: eff(30)},
	}
	mode := &fakeMode{
		result: modes.Result{
			Outcome: modes.OutcomeTargetReached,
			Selected: []modes.Candidate{
				{DeviceIdentifier: "a", PowerW: 3000},
				{DeviceIdentifier: "b", PowerW: 3000},
			},
			RealizedReductionW: 6000,
		},
	}

	plan := BuildPlan(inputs, nil, 1500, mode)

	assert.InDelta(t, 6.0, plan.EstimatedReductionKW, 0.001)
	assert.InDelta(t, 3.0, plan.EstimatedRemainingPowerKW, 0.001)
	assert.Equal(t, modes.OutcomeTargetReached, plan.Outcome)
	require.Len(t, plan.Selected, 2)
	assert.Equal(t, "a", plan.Selected[0].DeviceIdentifier)
}

// TestBuildPlan_FixedKwEndToEnd exercises the selector against a real FixedKw
// mode (not a fake) to verify the integration: dual-signal filter +
// efficiency rank + accumulate-until-target produce the expected plan.
func TestBuildPlan_FixedKwEndToEnd(t *testing.T) {
	t.Parallel()

	inputs := []CandidateInput{
		// Eligible miners, mixed efficiency.
		{DeviceIdentifier: "best",  PowerW: 3000, HashRateHS: 100, AvgEfficiencyJH: eff(20)},
		{DeviceIdentifier: "mid",   PowerW: 3000, HashRateHS: 100, AvgEfficiencyJH: eff(30)},
		{DeviceIdentifier: "worst", PowerW: 3000, HashRateHS: 100, AvgEfficiencyJH: eff(40)},
		// Phantom-load miner — drawing power above threshold but not hashing.
		{DeviceIdentifier: "phantom", PowerW: 2000, HashRateHS: 0, AvgEfficiencyJH: nil},
	}
	mode, err := modes.NewFixedKw(5, 0, modes.InsufficientLoadDetail{})
	require.NoError(t, err)

	plan := BuildPlan(inputs, nil, 1500, mode)

	// Worst-first ranking: worst + mid = 6 kW reaches the 5 kW target.
	assert.Equal(t, modes.OutcomeTargetReached, plan.Outcome)
	require.Len(t, plan.Selected, 2)
	assert.Equal(t, "worst", plan.Selected[0].DeviceIdentifier)
	assert.Equal(t, "mid", plan.Selected[1].DeviceIdentifier)
	assert.InDelta(t, 6.0, plan.EstimatedReductionKW, 0.001)
	assert.InDelta(t, 3.0, plan.EstimatedRemainingPowerKW, 0.001)

	require.Len(t, plan.Skipped, 1)
	assert.Equal(t, SkipPhantomLoadNoHash, plan.Skipped[0].Reason)
}

func TestBuildPlan_InsufficientLoad_ForwardsDetail(t *testing.T) {
	t.Parallel()

	// Two eligible miners @ 1.5 kW each. 3 kW available, target 10 kW, no tolerance.
	inputs := []CandidateInput{
		{DeviceIdentifier: "a", PowerW: 1500, HashRateHS: 100, AvgEfficiencyJH: eff(40)},
		{DeviceIdentifier: "b", PowerW: 1500, HashRateHS: 100, AvgEfficiencyJH: eff(40)},
	}
	summary := modes.InsufficientLoadDetail{
		CandidateMinPowerW: 1500,
		ExcludedOffline:    3,
	}
	mode, err := modes.NewFixedKw(10, 0, summary)
	require.NoError(t, err)

	plan := BuildPlan(inputs, nil, 1500, mode)

	assert.Equal(t, modes.OutcomeInsufficientLoad, plan.Outcome)
	assert.Empty(t, plan.Selected)
	require.NotNil(t, plan.InsufficientLoadDetail)
	assert.InDelta(t, 3.0, plan.InsufficientLoadDetail.AvailableKW, 0.001)
	assert.Equal(t, 10.0, plan.InsufficientLoadDetail.RequestedKW)
	// Selector-supplied counts forward through the mode unchanged.
	assert.Equal(t, int32(1500), plan.InsufficientLoadDetail.CandidateMinPowerW)
	assert.Equal(t, int32(3), plan.InsufficientLoadDetail.ExcludedOffline)
}
