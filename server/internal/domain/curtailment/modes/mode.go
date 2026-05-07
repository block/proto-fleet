// Package modes provides pluggable mode-specific selection. Closed-loop
// modes plug into Mode without touching the selector.
package modes

// Candidate is the per-device input handed to a mode after ranking.
// PowerW doubles as accumulation input and (via the selector) baseline_power_w.
type Candidate struct {
	DeviceIdentifier string
	PowerW           float64
	EfficiencyJH     float64
}

// Outcome categorizes a mode result. TargetReached and UndershootTolerated
// produce a non-empty Selected; InsufficientLoad produces empty Selected
// plus a structured detail.
type Outcome int

const (
	// OutcomeTargetReached: realized kW lands in
	// [target_kw, target_kw + last_added.power_w]. A small overshoot is
	// unavoidable since miners are atomic.
	OutcomeTargetReached Outcome = iota

	// OutcomeUndershootTolerated: sum < target_kw but >= target_kw - tolerance_kw.
	// Only fires when the operator passes a positive tolerance.
	OutcomeUndershootTolerated

	// OutcomeInsufficientLoad: sum < target_kw - tolerance_kw. Empty
	// selection plus a structured detail.
	OutcomeInsufficientLoad
)

// InsufficientLoadDetail carries the diagnostic numbers the handler echoes
// back on the rejection branch (available/requested/tolerance kW, candidate
// floor, per-reason exclusion counts).
type InsufficientLoadDetail struct {
	AvailableKW            float64
	RequestedKW            float64
	ToleranceKW            float64
	CandidateMinPowerW     int32
	ExcludedBelowThreshold int32
	ExcludedOffline        int32
	ExcludedPhantomLoad    int32
	ExcludedDeadMonitor    int32
	ExcludedMaintenance    int32
	// ExcludedUpdating, ExcludedRebootRequired, ExcludedStale count the
	// transient-status skip branches in the selector pre-filter so an
	// insufficient-load response surfaces the real cause when a fleet-wide
	// firmware rollout (UPDATING) or telemetry outage drops every miner
	// out of eligibility.
	ExcludedUpdating       int32
	ExcludedRebootRequired int32
	ExcludedStale          int32
	ExcludedNonActionable  int32
	ExcludedPairing        int32
	ExcludedCooldown       int32
	ExcludedCapabilityMiss int32
	ExcludedActiveEvent    int32
}

// Result is the mode's output. Selected is the chosen set in dispatch order;
// RealizedReductionW is the accumulated power_w of Selected. On
// OutcomeInsufficientLoad, Selected is empty and InsufficientDetail is set.
type Result struct {
	Outcome            Outcome
	Selected           []Candidate
	RealizedReductionW float64
	InsufficientDetail *InsufficientLoadDetail
}

// Mode applies mode-specific selection to a ranked candidate list.
// Implementations MUST be pure: no I/O, no time, no shared state.
type Mode interface {
	Select(ranked []Candidate) Result
}
