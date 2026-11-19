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

// guards against unknown Results: an unknown result must log + drop
// the sample, never panic and never reach the store.
func TestUnknownResultIsRejected(t *testing.T) {
	store := NewInMemoryStore()
	p := SetupWithStore(context.Background(), "test", Config{
		Enabled: true, BufferSize: 4, BatchSize: 2,
	}, store)
	p.EmitCommand(context.Background(), CommandLabels{Kind: "reboot", Result: "weird"})
	p.EmitTelemetryPoll(context.Background(), TelemetryPollLabels{Result: "weird"})
	require.NoError(t, p.Shutdown(context.Background()))
	require.Empty(t, store.Snapshot(), "unknown-result emits must not reach the store")
}
