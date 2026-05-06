package curtailment

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/modes"
)

// TestTranslateInsufficientLoad_IncludesAllNonZeroCounters pins the
// contract that every non-zero exclusion counter on InsufficientLoadDetail
// surfaces in the formatted error message. Without this, callers can't
// distinguish phantom-load vs dead-monitor vs below-threshold rejection.
func TestTranslateInsufficientLoad_IncludesAllNonZeroCounters(t *testing.T) {
	t.Parallel()

	detail := &modes.InsufficientLoadDetail{
		AvailableKW:            3.0,
		RequestedKW:            10.0,
		ToleranceKW:            1.0,
		CandidateMinPowerW:     1500,
		ExcludedBelowThreshold: 2,
		ExcludedPhantomLoad:    3,
		ExcludedDeadMonitor:    1,
		ExcludedCapabilityMiss: 4,
		// Other counters intentionally zero.
	}

	err := translateInsufficientLoad(detail)
	require.Error(t, err)
	msg := err.Error()

	// Header carries the kW + min-power numbers.
	assert.Contains(t, msg, "3.000 kW available")
	assert.Contains(t, msg, "10.000 kW requested")
	assert.Contains(t, msg, "tolerance 1.000 kW")
	assert.Contains(t, msg, "candidate_min_power_w=1500W")

	// Every non-zero counter appears with name=value.
	for _, want := range []string{
		"below_threshold=2",
		"phantom_load=3",
		"dead_monitor=1",
		"capability_miss=4",
	} {
		assert.Contains(t, msg, want, "non-zero counter %q must appear in message", want)
	}

	// Zero counters are suppressed.
	for _, omit := range []string{
		"offline=", "maintenance=", "pairing=", "cooldown=", "active_event=",
	} {
		assert.NotContains(t, msg, omit, "zero counter %q must not appear", omit)
	}
}

// TestTranslateInsufficientLoad_FormatIsByteStable pins the format-string
// contract: identical input must produce byte-identical output. Future
// callers (UI, automations) may regex-parse the message until Connect
// error details land; an unstable format would break them silently.
func TestTranslateInsufficientLoad_FormatIsByteStable(t *testing.T) {
	t.Parallel()

	detail := &modes.InsufficientLoadDetail{
		AvailableKW:            5.5,
		RequestedKW:            20.0,
		ToleranceKW:            2.0,
		CandidateMinPowerW:     1500,
		ExcludedOffline:        3,
		ExcludedMaintenance:    1,
		ExcludedBelowThreshold: 2,
	}

	first := translateInsufficientLoad(detail).Error()
	for range 10 {
		repeat := translateInsufficientLoad(detail).Error()
		require.Equal(t, first, repeat, "translateInsufficientLoad must be byte-stable across calls")
	}

	// Counter order in the message is fixed at source: below_threshold
	// always precedes offline, offline always precedes maintenance, etc.
	belowIdx := strings.Index(first, "below_threshold=")
	offlineIdx := strings.Index(first, "offline=")
	maintIdx := strings.Index(first, "maintenance=")
	require.NotEqual(t, -1, belowIdx)
	require.NotEqual(t, -1, offlineIdx)
	require.NotEqual(t, -1, maintIdx)
	assert.Less(t, belowIdx, offlineIdx, "below_threshold must precede offline in the formatted output")
	assert.Less(t, offlineIdx, maintIdx, "offline must precede maintenance in the formatted output")
}

// TestTranslateInsufficientLoad_AllZeroCountersOmitsExcludedSection pins
// the "no excluded section" branch: when every counter is zero, the
// message reports the kW numbers only and omits the trailing "excluded:"
// clause entirely.
func TestTranslateInsufficientLoad_AllZeroCountersOmitsExcludedSection(t *testing.T) {
	t.Parallel()

	detail := &modes.InsufficientLoadDetail{
		AvailableKW:        0.5,
		RequestedKW:        10.0,
		ToleranceKW:        2.0,
		CandidateMinPowerW: 1500,
	}

	err := translateInsufficientLoad(detail)
	require.Error(t, err)
	msg := err.Error()

	assert.Contains(t, msg, "0.500 kW available")
	assert.NotContains(t, msg, "excluded:", "no excluded section when every counter is zero")
}

// TestTranslateInsufficientLoad_NilDetailFallsBackToBareMessage pins the
// safety branch: a nil detail returns a sensible bare message rather
// than panicking on a pointer dereference.
func TestTranslateInsufficientLoad_NilDetailFallsBackToBareMessage(t *testing.T) {
	t.Parallel()
	err := translateInsufficientLoad(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient curtailable load")
}
