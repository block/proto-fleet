package preflight

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testSV2URL = "stratum2+tcp://pool.example.com:3336/9bXiEd8boQVhq7WddEcERUL5tyyJVFYdU8th3HfbNXK3Yw6GRXh"

func TestPlan_MixedCapabilitiesKeepNativeDirectAndTranslateSV1(t *testing.T) {
	devices := []Device{
		{Identifier: "native", NativeStratumV2: true},
		{Identifier: "sv1-only"},
	}
	slots := []SlotAssignment{
		{URL: testSV2URL, Username: "account"},
		{URL: "stratum+tcp://backup.example.com:3333", Username: "account"},
	}

	plan, err := Plan(devices, slots)

	require.NoError(t, err)
	require.True(t, plan.TranslationRequired())
	require.Equal(t, 1, len(plan.TranslatorProfile.Upstreams))
	assert.Equal(t, testSV2URL, plan.TranslatorProfile.Upstreams[0].URL)
	assert.Equal(t, "account", plan.TranslatorProfile.Upstreams[0].Username)
	require.Equal(t, 2, len(plan.Devices))
	assert.Equal(t, []EffectiveSlot{
		{SourceIndex: 0},
		{SourceIndex: 1},
	}, plan.Devices[0].Slots)
	assert.Equal(t, []EffectiveSlot{
		{SourceIndex: 0, UsesTranslation: true},
		{SourceIndex: 1},
	}, plan.Devices[1].Slots)
}

func TestPlan_AdjacentSV2SlotsCollapseIntoOneTranslatedSlot(t *testing.T) {
	slots := []SlotAssignment{
		{URL: "stratum+tcp://default.example.com:3333"},
		{URL: testSV2URL, Username: "primary"},
		{URL: "stratum2+tcp://backup.example.com:3336/9bXiEd8boQVhq7WddEcERUL5tyyJVFYdU8th3HfbNXK3Yw6GRXh", Username: "backup"},
	}

	plan, err := Plan([]Device{{Identifier: "sv1-only"}}, slots)

	require.NoError(t, err)
	require.Equal(t, 2, len(plan.TranslatorProfile.Upstreams))
	assert.Equal(t, []EffectiveSlot{
		{SourceIndex: 0},
		{SourceIndex: 1, UsesTranslation: true},
	}, plan.Devices[0].Slots)
}

func TestPlan_NonContiguousSV2SlotsFailForSV1OnlyMiner(t *testing.T) {
	slots := []SlotAssignment{
		{URL: testSV2URL},
		{URL: "stratum+tcp://middle.example.com:3333"},
		{URL: testSV2URL},
	}

	_, err := Plan([]Device{{Identifier: "sv1-only"}}, slots)

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNonContiguousSV2Slots))
}

func TestPlan_AllNativeDoesNotActivateTranslation(t *testing.T) {
	plan, err := Plan(
		[]Device{{Identifier: "native", NativeStratumV2: true}},
		[]SlotAssignment{{URL: testSV2URL}},
	)

	require.NoError(t, err)
	assert.False(t, plan.TranslationRequired())
	assert.Equal(t, []EffectiveSlot{{SourceIndex: 0}}, plan.Devices[0].Slots)
}
