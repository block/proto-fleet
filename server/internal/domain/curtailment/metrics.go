package curtailment

import "time"

// Metrics records curtailment operational signals through the platform metrics
// provider in production. The heartbeat table independently records reconciler
// liveness; target-write and tick failures distinguish stalled work from a dead
// reconciler.
type Metrics interface {
	ObserveTickDuration(d time.Duration)
	IncTickFailure()
	// IncConfirmationPassFailure counts confirmation fast-path pulse passes
	// that could not make reliable progress (read error, widespread sampling
	// failure/omission, write error or timeout, or recovered panic). Mirrors
	// IncTickFailure for the pulse so a stuck fast path is visible on
	// dashboards even while the full tick stays healthy.
	IncConfirmationPassFailure()
	// IncCandidateExcluded adds count selector exclusions for a reason
	// (e.g. "phantom_load_no_hash", "stale_telemetry") in one sample.
	IncCandidateExcluded(reason string, count int)
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
	// IncAllPairedPendingStall counts reconciler ticks that observed an
	// all-paired policy event held in pending past the stall threshold with
	// nothing confirmed. The event owns its scope and blocks other starts
	// until confirmation, Stop, or finite-cap expiry. Only explicitly unbounded
	// events may hold indefinitely. Sustained stalls must be visible on
	// dashboards. Operator-actionable.
	IncAllPairedPendingStall()
}

// NoOpMetrics supports tests and assemblies without operational metrics.
type NoOpMetrics struct{}

func (NoOpMetrics) ObserveTickDuration(time.Duration) {}
func (NoOpMetrics) IncTickFailure()                   {}
func (NoOpMetrics) IncConfirmationPassFailure()       {}
func (NoOpMetrics) IncCandidateExcluded(string, int)  {}
func (NoOpMetrics) IncMaintenanceOverride()           {}
func (NoOpMetrics) IncEventStateRaceLoss()            {}
func (NoOpMetrics) IncTargetWriteFailure()            {}
func (NoOpMetrics) IncAuditWriteFailure(string)       {}
func (NoOpMetrics) IncAllPairedPendingStall()         {}
