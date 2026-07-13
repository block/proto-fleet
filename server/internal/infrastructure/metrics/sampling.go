package metrics

import (
	"sync"
	"time"
)

// gaugeSeriesKey identifies one persisted gauge series: a metric name plus
// its full label set. Labels is a flat struct of strings, so the key is
// directly comparable.
type gaugeSeriesKey struct {
	metric string
	labels Labels
}

type gaugeSeriesState struct {
	value float64
	// persisted carries Go's monotonic clock reading (callers pass
	// time.Now() without UTC()), so interval math is immune to wall-clock
	// steps; only Sample.Time uses wall-clock UTC.
	persisted time.Time
}

// gaugeThrottle decides which per-device gauge samples are worth persisting.
// Device gauges are re-emitted on every telemetry poll (~15s) even when
// nothing changed; at fleet scale that cadence, not the data, dominated the
// notification_metric_sample hypertable. A sample is persisted when the
// series is new, when its value changed on a change-sensitive (0/1 state)
// gauge, or when interval has elapsed since the last persisted sample — the
// heartbeat that keeps the Grafana rules' freshness gates satisfied.
type gaugeThrottle struct {
	interval time.Duration

	mu     sync.Mutex
	series map[gaugeSeriesKey]gaugeSeriesState
}

func newGaugeThrottle(interval time.Duration) *gaugeThrottle {
	return &gaugeThrottle{
		interval: interval,
		series:   make(map[gaugeSeriesKey]gaugeSeriesState),
	}
}

// shouldPersist reports whether this sample must land in the store, and
// records it as the series' latest persisted state when it does.
func (t *gaugeThrottle) shouldPersist(key gaugeSeriesKey, value float64, now time.Time, persistOnChange bool) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	st, seen := t.series[key]
	stateChanged := persistOnChange && value != st.value
	sinceLast := now.Sub(st.persisted)
	// A negative elapsed reading means the caller's clock moved backwards
	// (only possible for wall-clock times); fail open and persist rather
	// than suppressing heartbeats until the clock catches up.
	heartbeatDue := sinceLast >= t.interval || sinceLast < 0

	if seen && !stateChanged && !heartbeatDue {
		return false
	}
	t.series[key] = gaugeSeriesState{value: value, persisted: now}
	return true
}

// invalidate forgets the given samples' series state so their next emit
// persists unconditionally. Called for samples known to have been dropped
// after shouldPersist admitted them but before they reached the store —
// without this, a dropped 0/1 state transition would be suppressed as
// already-persisted until the heartbeat interval elapses.
func (t *gaugeThrottle) invalidate(samples ...Sample) {
	if t == nil || len(samples) == 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, s := range samples {
		delete(t.series, gaugeSeriesKey{metric: s.Metric, labels: s.Labels})
	}
}

// sweep drops series that stopped emitting (device removed from the fleet)
// so the map tracks the live fleet, not every device ever seen. It runs on
// the provider's background flush loop, never on the emit hot path — a full
// map scan under this mutex would otherwise stall every concurrent emitter.
func (t *gaugeThrottle) sweep(now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	cutoff := now.Add(-4 * t.interval)
	for key, st := range t.series {
		if st.persisted.Before(cutoff) {
			delete(t.series, key)
		}
	}
}

// pollAggregator accumulates fleet_telemetry_poll_total increments in
// process so one row per (organization, site, result) lands per flush window
// instead of one row per poll attempt per device. Each flushed row carries
// value = number of polls in the window, so the failure-rate rule's
// sum(value) over its evaluation window is unchanged, and the 1-minute
// fleet_telemetry_poll_heartbeat buckets stay populated for every org that
// polled. Per-device poll rows are intentionally gone — device_id is an
// optional label in the contract, and no rule reads it on this metric.
type pollAggregator struct {
	mu     sync.Mutex
	counts map[Labels]float64
}

func newPollAggregator() *pollAggregator {
	return &pollAggregator{counts: make(map[Labels]float64)}
}

func (a *pollAggregator) add(labels Labels) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.counts[labels]++
}

// drain returns the accumulated counts and resets the aggregator.
func (a *pollAggregator) drain() map[Labels]float64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.counts) == 0 {
		return nil
	}
	out := a.counts
	a.counts = make(map[Labels]float64)
	return out
}
