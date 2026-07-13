package metrics

import (
	"os"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// invalidate must force the next emit to persist even when unchanged inside
// the interval — the recovery path for samples dropped after admission.
func TestGaugeThrottleInvalidateForcesRepersist(t *testing.T) {
	th := newGaugeThrottle(time.Hour)
	labels := Labels{OrganizationID: "org-1", DeviceID: "device-1"}
	key := gaugeSeriesKey{metric: MetricDeviceOnline, labels: labels}
	now := time.Now()

	require.True(t, th.shouldPersist(key, 0, now, true))
	require.False(t, th.shouldPersist(key, 0, now.Add(time.Second), true),
		"unchanged state inside the interval must be suppressed")

	th.invalidate(Sample{Metric: MetricDeviceOnline, Labels: labels, Value: 0})
	require.True(t, th.shouldPersist(key, 0, now.Add(2*time.Second), true),
		"an invalidated series must re-persist on the next emit")
}

// A backward-stepping clock must not suppress heartbeats: a negative
// elapsed reading fails open and persists.
func TestGaugeThrottleBackwardClockFailsOpen(t *testing.T) {
	th := newGaugeThrottle(time.Minute)
	key := gaugeSeriesKey{metric: MetricDeviceHashing, labels: Labels{DeviceID: "d"}}
	now := time.Now()

	require.True(t, th.shouldPersist(key, 0.9, now, false))
	require.True(t, th.shouldPersist(key, 0.9, now.Add(-30*time.Second), false),
		"a sample timestamped before the last persist must not be throttled")
}

// sweep drops series that stopped emitting, and only those.
func TestGaugeThrottleSweepDropsIdleSeries(t *testing.T) {
	th := newGaugeThrottle(time.Minute)
	now := time.Now()
	idle := gaugeSeriesKey{metric: MetricDeviceOnline, labels: Labels{DeviceID: "idle"}}
	live := gaugeSeriesKey{metric: MetricDeviceOnline, labels: Labels{DeviceID: "live"}}

	require.True(t, th.shouldPersist(idle, 1, now.Add(-5*time.Minute), true))
	require.True(t, th.shouldPersist(live, 1, now.Add(-time.Second), true))

	th.sweep(now)
	require.NotContains(t, th.series, idle, "idle series must be swept after 4 intervals")
	require.Contains(t, th.series, live, "live series must survive the sweep")
}

// The throttle ceiling and the temperature rule's freshness gate span two
// artifacts; pin the invariant so tightening either side alone fails loudly.
func TestFreshnessGateCoversGaugeThrottleCeiling(t *testing.T) {
	raw, err := os.ReadFile("../../../monitoring/grafana/provisioning/alerting/proto-fleet-rules.yaml")
	require.NoError(t, err)

	m := regexp.MustCompile(`last_sample_time > NOW\(\) - INTERVAL '(\d+) minutes?'`).
		FindStringSubmatch(string(raw))
	require.NotNil(t, m, "temperature freshness gate not found in proto-fleet-rules.yaml — update this test alongside the rule")
	minutes, err := strconv.Atoi(m[1])
	require.NoError(t, err)
	gate := time.Duration(minutes) * time.Minute

	// Worst-case age = ceiling + poll spacing + flush; the gate must also
	// absorb a missed poll cycle so a flaky device doesn't flap the alert.
	const pollAndFlushSlack = 45 * time.Second
	require.GreaterOrEqual(t, gate, maxGaugeThrottleInterval+2*pollAndFlushSlack,
		"freshness gate too tight for the gauge throttle ceiling: unchanged hot devices would de-gate and reset the alert's pending timer")

	cfg := applyDefaults(Config{})
	require.LessOrEqual(t, cfg.GaugeThrottleInterval, maxGaugeThrottleInterval)
	// One-minute heartbeat buckets must always be populated while polling.
	require.LessOrEqual(t, cfg.PollAggregationInterval, time.Minute)
	require.LessOrEqual(t, maxPollAggregationInterval, time.Minute)
}
