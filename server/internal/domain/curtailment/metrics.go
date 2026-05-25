package curtailment

import "time"

// Metrics records curtailment operational signals. Default is no-op; the
// real recorder is wired at cmd/fleetd/main.go. Heartbeat staleness is a
// separate SQL check against curtailment_reconciler_heartbeat —
// IncTargetWriteFailure pairs with it to catch "heartbeat fresh but
// events stalled" outages.
type Metrics interface {
	ObserveTickDuration(d time.Duration)
	IncTickFailure()
	// IncCandidateExcluded counts selector exclusions by reason
	// (e.g. "phantom_load_no_hash", "stale_telemetry").
	IncCandidateExcluded(reason string)
	IncMaintenanceOverride()
	// IncEventStateRaceLoss counts UpdateEventState or UpdateTargetState
	// writes that matched zero rows because a concurrent Stop/AdminTerminate
	// advanced the parent event first.
	IncEventStateRaceLoss()
	// IncTargetWriteFailure counts non-race-loss target-write failures
	// (transient DB error, deadline exceeded). Operator-actionable.
	IncTargetWriteFailure()
	// IncAuditWriteFailure counts activity_log persistence failures.
	// Audit emits never roll back the curtailment action; this counter
	// is the only signal that a row was silently dropped.
	IncAuditWriteFailure(activityType string)
}

// NoOpMetrics is the default until the platform observability path lands.
type NoOpMetrics struct{}

func (NoOpMetrics) ObserveTickDuration(time.Duration) {}
func (NoOpMetrics) IncTickFailure()                   {}
func (NoOpMetrics) IncCandidateExcluded(string)       {}
func (NoOpMetrics) IncMaintenanceOverride()           {}
func (NoOpMetrics) IncEventStateRaceLoss()            {}
func (NoOpMetrics) IncTargetWriteFailure()            {}
func (NoOpMetrics) IncAuditWriteFailure(string)       {}
