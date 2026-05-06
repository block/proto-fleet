package modes

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func cands(powersW ...float64) []Candidate {
	out := make([]Candidate, len(powersW))
	for i, p := range powersW {
		out[i] = Candidate{
			DeviceIdentifier: deviceID(i),
			PowerW:           p,
		}
	}
	return out
}

func deviceID(i int) string {
	return "miner-" + string(rune('a'+i))
}

func TestFixedKw_TargetReached_StopsAfterFirstOvershoot(t *testing.T) {
	t.Parallel()

	// 5 miners @ 3 kW each. Target 10 kW: should select 4 (= 12 kW) and stop.
	m, err := NewFixedKw(10, 0, InsufficientLoadDetail{})
	require.NoError(t, err)

	r := m.Select(cands(3000, 3000, 3000, 3000, 3000))

	assert.Equal(t, OutcomeTargetReached, r.Outcome)
	assert.Len(t, r.Selected, 4)
	assert.InDelta(t, 12000.0, r.RealizedReductionW, 0.001)
	assert.Nil(t, r.InsufficientDetail)
}

func TestFixedKw_TargetReached_ExactBoundary(t *testing.T) {
	t.Parallel()

	// First two candidates sum exactly to target. Should stop at index 1.
	m, err := NewFixedKw(6, 0, InsufficientLoadDetail{})
	require.NoError(t, err)

	r := m.Select(cands(3000, 3000, 3000, 3000))

	assert.Equal(t, OutcomeTargetReached, r.Outcome)
	assert.Len(t, r.Selected, 2)
	assert.InDelta(t, 6000.0, r.RealizedReductionW, 0.001)
}

func TestFixedKw_StrictZeroTolerance_RejectsEvenSlightUndershoot(t *testing.T) {
	t.Parallel()

	// Total available 5.999 kW, target 6 kW, tolerance 0 → reject.
	m, err := NewFixedKw(6, 0, InsufficientLoadDetail{
		ExcludedOffline: 2,
	})
	require.NoError(t, err)

	r := m.Select(cands(2999, 1500, 1500))

	assert.Equal(t, OutcomeInsufficientLoad, r.Outcome)
	assert.Empty(t, r.Selected)
	require.NotNil(t, r.InsufficientDetail)
	assert.InDelta(t, 5.999, r.InsufficientDetail.AvailableKW, 0.001)
	assert.Equal(t, 6.0, r.InsufficientDetail.RequestedKW)
	assert.Equal(t, 0.0, r.InsufficientDetail.ToleranceKW)
	// Selector-supplied exclusion counts forward into the rejection detail.
	assert.Equal(t, int32(2), r.InsufficientDetail.ExcludedOffline)
}

func TestFixedKw_PositiveToleranceAcceptsNearMiss(t *testing.T) {
	t.Parallel()

	// Available 5.999 kW, target 6 kW, tolerance 0.5 kW → take all.
	m, err := NewFixedKw(6, 0.5, InsufficientLoadDetail{})
	require.NoError(t, err)

	r := m.Select(cands(2999, 1500, 1500))

	assert.Equal(t, OutcomeUndershootTolerated, r.Outcome)
	assert.Len(t, r.Selected, 3)
	assert.InDelta(t, 5999.0, r.RealizedReductionW, 0.001)
}

func TestFixedKw_PositiveToleranceStillRejectsBeyondBand(t *testing.T) {
	t.Parallel()

	// Available 4 kW, target 6 kW, tolerance 0.5 kW. 6 - 0.5 = 5.5; 4 < 5.5 → reject.
	m, err := NewFixedKw(6, 0.5, InsufficientLoadDetail{})
	require.NoError(t, err)

	r := m.Select(cands(2000, 2000))

	assert.Equal(t, OutcomeInsufficientLoad, r.Outcome)
	require.NotNil(t, r.InsufficientDetail)
	assert.InDelta(t, 4.0, r.InsufficientDetail.AvailableKW, 0.001)
	assert.Equal(t, 0.5, r.InsufficientDetail.ToleranceKW)
}

func TestFixedKw_EmptyCandidateList(t *testing.T) {
	t.Parallel()

	m, err := NewFixedKw(1, 0, InsufficientLoadDetail{})
	require.NoError(t, err)

	r := m.Select(nil)

	assert.Equal(t, OutcomeInsufficientLoad, r.Outcome)
	assert.Empty(t, r.Selected)
	require.NotNil(t, r.InsufficientDetail)
	assert.Equal(t, 0.0, r.InsufficientDetail.AvailableKW)
}

func TestNewFixedKw_RejectsNonPositiveTarget(t *testing.T) {
	t.Parallel()

	for _, target := range []float64{0, -1, -0.001} {
		_, err := NewFixedKw(target, 0, InsufficientLoadDetail{})
		assert.Error(t, err, "target=%v should be rejected", target)
	}
}

func TestNewFixedKw_RejectsNegativeTolerance(t *testing.T) {
	t.Parallel()

	_, err := NewFixedKw(10, -0.001, InsufficientLoadDetail{})
	assert.Error(t, err)
}

// TestFixedKw_FirstCandidatePushesPastTarget verifies the realized-kW band
// from the design: realized ∈ [target_kw, target_kw + last_added_miner.power_w].
// A 3 kW first candidate against a 1 kW target should return that single
// candidate at realized 3 kW (overshoot is unbounded; only undershoot is
// constrained by tolerance).
func TestFixedKw_FirstCandidatePushesPastTarget(t *testing.T) {
	t.Parallel()

	m, err := NewFixedKw(1, 0, InsufficientLoadDetail{})
	require.NoError(t, err)

	r := m.Select(cands(3000, 1500, 1500))

	assert.Equal(t, OutcomeTargetReached, r.Outcome)
	assert.Len(t, r.Selected, 1)
	assert.InDelta(t, 3000.0, r.RealizedReductionW, 0.001)
}

func TestFixedKw_PreservesRankingOrderInSelected(t *testing.T) {
	t.Parallel()

	m, err := NewFixedKw(5, 0, InsufficientLoadDetail{})
	require.NoError(t, err)

	r := m.Select(cands(1000, 2000, 3000, 4000))

	assert.Equal(t, OutcomeTargetReached, r.Outcome)
	require.Len(t, r.Selected, 3) // 1+2+3 = 6 kW reaches 5 kW
	assert.Equal(t, "miner-a", r.Selected[0].DeviceIdentifier)
	assert.Equal(t, "miner-b", r.Selected[1].DeviceIdentifier)
	assert.Equal(t, "miner-c", r.Selected[2].DeviceIdentifier)
}
