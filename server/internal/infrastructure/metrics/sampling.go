package metrics

import (
	"sync"
	"time"
)

// gaugeSeriesKey identifies one persisted gauge series; Labels is a flat
// struct of strings, so the key is directly comparable.
type gaugeSeriesKey struct {
	metric string
	labels Labels
}

type gaugeSeriesState struct {
	value float64
	// Carries the monotonic clock (time.Now() without UTC()), so interval
	// math survives wall-clock steps; only Sample.Time is wall-clock UTC.
	persisted time.Time
}

// gaugeThrottle suppresses per-poll re-emits of unchanged device gauges: a
// sample persists when new, on a 0/1 state change, or once per interval.
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
	// Negative elapsed = caller's clock moved backwards; fail open rather
	// than suppressing heartbeats until the clock catches up.
	heartbeatDue := sinceLast >= t.interval || sinceLast < 0

	if seen && !stateChanged && !heartbeatDue {
		return false
	}
	t.series[key] = gaugeSeriesState{value: value, persisted: now}
	return true
}

// invalidate forgets series state for samples dropped after admission, so
// the next emit re-persists instead of serving stale state for an interval.
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

// sweep drops series that stopped emitting (device removed). Runs on the
// flush loop only — a full map scan under this mutex would stall emitters.
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

// pollAggregator collapses fleet_telemetry_poll_total to one row per
// (org, site, result) per window with value = poll count; sum(value) holds.
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
