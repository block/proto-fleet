package curtailment

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/modes"
)

// TestToInsufficientLoadError_IncludesAllNonZeroCounters pins the
// contract that every non-zero exclusion counter on InsufficientLoadDetail
// surfaces in the formatted error message. Without this, callers can't
// distinguish phantom-load vs dead-monitor vs below-threshold rejection.
func TestToInsufficientLoadError_IncludesAllNonZeroCounters(t *testing.T) {
	t.Parallel()

	detail := &modes.InsufficientLoadDetail{
		AvailableKW:            3.0,
		RequestedKW:            10.0,
		ToleranceKW:            1.0,
		CandidateMinPowerW:     1500,
		ExcludedBelowThreshold: 2,
		ExcludedPhantomLoad:    3,
		ExcludedDeadMonitor:    1,
		// Transient-status / data-quality counters: these were previously
		// uncounted in classifyCandidates so the message reported zero
		// exclusions during a fleet-wide firmware rollout. Pinned here.
		ExcludedUpdating:       5,
		ExcludedRebootRequired: 2,
		ExcludedStale:          7,
		ExcludedCapabilityMiss: 4,
		// Other counters intentionally zero.
	}

	err := toInsufficientLoadError(detail)
	require.Error(t, err)
	msg := err.Error()

	// Header carries the kW + min-power numbers.
	assert.Contains(t, msg, "3.000 kW available")
	assert.Contains(t, msg, "10.000 kW requested")
	assert.Contains(t, msg, "tolerance 1.000 kW")
	assert.Contains(t, msg, "candidate_min_power_w=1500W")

	// Every non-zero counter appears with name=value, using the canonical
	// SkipReason vocabulary so agents see one set of tokens across both
	// SkippedCandidate.reason (success path) and the InsufficientLoad
	// message (failure path).
	for _, want := range []string{
		"below_candidate_min_power_w=2",
		"phantom_load_no_hash=3",
		"power_telemetry_unreliable=1",
		"updating=5",
		"reboot_required=2",
		"stale_telemetry=7",
		"curtail_full_unsupported=4",
	} {
		assert.Contains(t, msg, want, "non-zero counter %q must appear in message", want)
	}

	// Zero counters are suppressed.
	for _, omit := range []string{
		"unreachable_residual_load=", "maintenance=", "pairing=", "cooldown=", "active_event=", "non_actionable_status=",
	} {
		assert.NotContains(t, msg, omit, "zero counter %q must not appear", omit)
	}
}

// TestToInsufficientLoadError_FormatIsByteStable pins the format-string
// contract: identical input must produce byte-identical output. Future
// callers (UI, automations) may regex-parse the message until Connect
// error details land; an unstable format would break them silently.
func TestToInsufficientLoadError_FormatIsByteStable(t *testing.T) {
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

	first := toInsufficientLoadError(detail).Error()
	for range 10 {
		repeat := toInsufficientLoadError(detail).Error()
		require.Equal(t, first, repeat, "toInsufficientLoadError must be byte-stable across calls")
	}

	// Counter order in the message is fixed at source: below_candidate_min_power_w
	// always precedes unreachable_residual_load, which always precedes maintenance.
	belowIdx := strings.Index(first, "below_candidate_min_power_w=")
	offlineIdx := strings.Index(first, "unreachable_residual_load=")
	maintIdx := strings.Index(first, "maintenance=")
	require.NotEqual(t, -1, belowIdx)
	require.NotEqual(t, -1, offlineIdx)
	require.NotEqual(t, -1, maintIdx)
	assert.Less(t, belowIdx, offlineIdx, "below_candidate_min_power_w must precede unreachable_residual_load")
	assert.Less(t, offlineIdx, maintIdx, "unreachable_residual_load must precede maintenance")
}

// TestToInsufficientLoadError_AllZeroCountersOmitsExcludedSection pins
// the "no excluded section" branch: when every counter is zero, the
// message reports the kW numbers only and omits the trailing "excluded:"
// clause entirely.
func TestToInsufficientLoadError_AllZeroCountersOmitsExcludedSection(t *testing.T) {
	t.Parallel()

	detail := &modes.InsufficientLoadDetail{
		AvailableKW:        0.5,
		RequestedKW:        10.0,
		ToleranceKW:        2.0,
		CandidateMinPowerW: 1500,
	}

	err := toInsufficientLoadError(detail)
	require.Error(t, err)
	msg := err.Error()

	assert.Contains(t, msg, "0.500 kW available")
	assert.NotContains(t, msg, "excluded:", "no excluded section when every counter is zero")
}

// TestToInsufficientLoadError_NilDetailFallsBackToBareMessage pins the
// safety branch: a nil detail returns a sensible bare message rather
// than panicking on a pointer dereference.
func TestToInsufficientLoadError_NilDetailFallsBackToBareMessage(t *testing.T) {
	t.Parallel()
	err := toInsufficientLoadError(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient curtailable load")
}
