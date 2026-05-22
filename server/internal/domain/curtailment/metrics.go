package curtailment

import "time"

// Metrics is the recorder surface the curtailment domain uses for operational
// signals: tick duration, tick failures, selector candidate exclusions, and
// maintenance overrides. The default is a no-op; a concrete implementation is
// wired at cmd/fleetd/main.go once the platform observability pipeline
// (OTel Meter, Prometheus exporter, or host metrics agent) is in place.
//
// The Postgres-based heartbeat staleness alert is intentionally not on this
// interface — it's a SQL check from the monitoring stack against the
// curtailment_reconciler_heartbeat row, not an application metric.
type Metrics interface {
	// ObserveTickDuration records how long a single reconciler tick body took.
	ObserveTickDuration(d time.Duration)
	// IncTickFailure records a tick that aborted before advancing the heartbeat.
	IncTickFailure()
	// IncCandidateExcluded records one selector candidate-exclusion by reason
	// (e.g. "phantom_load_no_hash", "power_telemetry_unreliable", "stale").
	IncCandidateExcluded(reason string)
	// IncMaintenanceOverride records one per-miner maintenance override
	// application at Start.
	IncMaintenanceOverride()
	// IncEventStateRaceLoss records a reconciler UpdateEventState call that
	// matched zero rows because a concurrent Stop/AdminTerminate advanced the
	// row first. The tick continues — this signal is informational and lets
	// operators trend race frequency.
	IncEventStateRaceLoss()
}

// NoOpMetrics is the default Metrics used until the platform observability
// path is in place. Every method discards its argument and returns.
type NoOpMetrics struct{}

func (NoOpMetrics) ObserveTickDuration(time.Duration) {}
func (NoOpMetrics) IncTickFailure()                   {}
func (NoOpMetrics) IncCandidateExcluded(string)       {}
func (NoOpMetrics) IncMaintenanceOverride()           {}
func (NoOpMetrics) IncEventStateRaceLoss()            {}
