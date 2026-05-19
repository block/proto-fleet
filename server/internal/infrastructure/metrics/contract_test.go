package metrics

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// every metric in the contract starts with the fleet_ prefix
func TestAllMetricNamesUseFleetNamespace(t *testing.T) {
	for _, name := range AllMetricNames {
		require.True(t,
			strings.HasPrefix(name, Namespace),
			"metric %q must start with %q to stay in the contract namespace", name, Namespace,
		)
	}
}

// guards against duplicates in AllMetricNames.
func TestAllMetricNamesAreUnique(t *testing.T) {
	seen := make(map[string]struct{}, len(AllMetricNames))
	for _, name := range AllMetricNames {
		_, dup := seen[name]
		require.False(t, dup, "metric %q is listed twice in AllMetricNames", name)
		seen[name] = struct{}{}
	}
}

// guards against duplicates in AllLabelKeys.
func TestAllLabelKeysAreUnique(t *testing.T) {
	seen := make(map[string]struct{}, len(AllLabelKeys))
	for _, key := range AllLabelKeys {
		_, dup := seen[key]
		require.False(t, dup, "label key %q is listed twice in AllLabelKeys", key)
		seen[key] = struct{}{}
	}
}

// checks IsKnownLabel and IsKnownMetric
func TestKnownLabelHelpers(t *testing.T) {
	for _, m := range AllMetricNames {
		require.True(t, IsKnownMetric(m), "%q must be reported as known", m)
	}
	require.False(t, IsKnownMetric("node_cpu_seconds_total"))
	require.False(t, IsKnownMetric(""))

	for _, l := range AllLabelKeys {
		require.True(t, IsKnownLabel(l), "%q must be reported as known", l)
	}
	require.False(t, IsKnownLabel("__name__"))
	require.False(t, IsKnownLabel("instance"))
}

// checks ResultSuccess / ResultFailure values are closed.
func TestResultEnum(t *testing.T) {
	require.ElementsMatch(t, []string{"success", "failure"}, AllResults)
	require.True(t, IsKnownResult(ResultSuccess))
	require.True(t, IsKnownResult(ResultFailure))
	require.False(t, IsKnownResult("ok"))
	require.False(t, IsKnownResult("error"))
}

// checks TestSensorKindEnum is closed.
func TestSensorKindEnum(t *testing.T) {
	require.ElementsMatch(t,
		[]string{"board", "chip", "inlet", "outlet", "ambient", "hotspot"},
		AllSensorKinds)
}

// the only allowed labels are the ones declared in the contract
func TestValidateLabelKey(t *testing.T) {
	for _, key := range AllLabelKeys {
		require.NoError(t, validateLabelKey(key))
	}
	require.Error(t, validateLabelKey("user_supplied_label"))
	require.Error(t, validateLabelKey(""))
}

// guards against unknown Results
func TestUnknownResultIsRejected(t *testing.T) {
	p := newDisabledProvider(Config{})
	require.NotNil(t, p.insts)
	// These must not panic — the slog.Error is the user-visible.
	p.EmitCommand(context.Background(), CommandLabels{Kind: "reboot", Result: "weird"})
	p.EmitTelemetryPoll(context.Background(), TelemetryPollLabels{Result: "weird"})
}
