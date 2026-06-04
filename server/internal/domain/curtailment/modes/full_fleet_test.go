package modes

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFullFleet_SelectsAllInOrder(t *testing.T) {
	t.Parallel()

	ranked := []Candidate{
		{DeviceIdentifier: "a", PowerW: 3000},
		{DeviceIdentifier: "b", PowerW: 3500},
		{DeviceIdentifier: "c", PowerW: 3200},
	}
	got := FullFleet{}.Select(ranked)

	assert.Equal(t, OutcomeTargetReached, got.Outcome)
	assert.Equal(t, ranked, got.Selected, "every candidate is selected in dispatch order")
	assert.InDelta(t, 9700.0, got.RealizedReductionW, 1e-9)
	assert.Nil(t, got.InsufficientDetail)
}

// The defining difference from FIXED_KW: an empty eligible set is a success
// (vacuously off), never OutcomeInsufficientLoad.
func TestFullFleet_EmptyIsSuccessNotInsufficient(t *testing.T) {
	t.Parallel()

	got := FullFleet{}.Select(nil)

	assert.Equal(t, OutcomeTargetReached, got.Outcome, "empty input must succeed, not be InsufficientLoad")
	assert.Empty(t, got.Selected)
	assert.Zero(t, got.RealizedReductionW)
	assert.Nil(t, got.InsufficientDetail)
}

// Select must copy, not alias, the caller's slice.
func TestFullFleet_DoesNotAliasInput(t *testing.T) {
	t.Parallel()

	ranked := []Candidate{{DeviceIdentifier: "a", PowerW: 1000}}
	got := FullFleet{}.Select(ranked)
	got.Selected[0].PowerW = 9999

	assert.InDelta(t, 1000.0, ranked[0].PowerW, 1e-9, "input slice must be untouched")
}
