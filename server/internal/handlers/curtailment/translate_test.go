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
		ExcludedBelowThreshold: 2,
		ExcludedPhantomLoad:    1,
		ExcludedDeadMonitor:    1,
		ExcludedOffline:        3,
		ExcludedMaintenance:    1,
		ExcludedUpdating:       4,
		ExcludedRebootRequired: 1,
		ExcludedStale:          2,
	}

	first := toInsufficientLoadError(detail).Error()
	for range 10 {
		repeat := toInsufficientLoadError(detail).Error()
		require.Equal(t, first, repeat, "toInsufficientLoadError must be byte-stable across calls")
	}

	// Counter order is fixed at source. Each adjacent pair is asserted so
	// reordering any entry in formatExclusionCounters fails the test.
	indexOf := func(token string) int {
		i := strings.Index(first, token)
		require.NotEqual(t, -1, i, "expected %q in message", token)
		return i
	}
	belowIdx := indexOf("below_candidate_min_power_w=")
	phantomIdx := indexOf("phantom_load_no_hash=")
	deadMonitorIdx := indexOf("power_telemetry_unreliable=")
	offlineIdx := indexOf("unreachable_residual_load=")
	maintIdx := indexOf("maintenance=")
	updatingIdx := indexOf("updating=")
	rebootIdx := indexOf("reboot_required=")
	staleIdx := indexOf("stale_telemetry=")

	assert.Less(t, belowIdx, phantomIdx, "below_candidate_min_power_w must precede phantom_load_no_hash")
	assert.Less(t, phantomIdx, deadMonitorIdx, "phantom_load_no_hash must precede power_telemetry_unreliable")
	assert.Less(t, deadMonitorIdx, offlineIdx, "power_telemetry_unreliable must precede unreachable_residual_load")
	assert.Less(t, offlineIdx, maintIdx, "unreachable_residual_load must precede maintenance")
	assert.Less(t, maintIdx, updatingIdx, "maintenance must precede updating")
	assert.Less(t, updatingIdx, rebootIdx, "updating must precede reboot_required")
	assert.Less(t, rebootIdx, staleIdx, "reboot_required must precede stale_telemetry")
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
