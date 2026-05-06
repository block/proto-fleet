// Package modes encapsulates the mode-specific selection logic that turns a
// ranked candidate list into a plan. v1 ships FIXED_KW; closed-loop modes
// (SITE_POWER_CAP, THERMAL_LIMIT, etc.) plug into the same Mode interface
// without touching the selector or the reconciler tick body.
package modes

// Candidate is the per-device input the selector hands to a mode after
// ranking. power_w is the telemetry snapshot used both for accumulation and
// (separately, by the selector) as the persisted baseline_power_w.
type Candidate struct {
	DeviceIdentifier string
	PowerW           float64
	EfficiencyJH     float64
}

// Outcome categorizes the result of applying a mode to a ranked candidate
// list. TargetReached and UndershootTolerated produce a non-empty Selected
// list; InsufficientLoad produces an empty Selected list and a structured
// detail the handler can surface to the caller.
type Outcome int

const (
	// OutcomeTargetReached: walking the ranking hit a candidate whose
	// inclusion brought the running sum to or past target. Realized kW is
	// in [target_kw, target_kw + last_added.power_w] — a small overshoot
	// is unavoidable since miners are atomic.
	OutcomeTargetReached Outcome = iota

	// OutcomeUndershootTolerated: the entire ranked list summed below
	// target_kw, but at least target_kw - tolerance_kw. Operator
	// explicitly accepted the near-miss via positive tolerance_kw.
	OutcomeUndershootTolerated

	// OutcomeInsufficientLoad: even with all candidates selected, the
	// sum is below target_kw - tolerance_kw. The plan rejects with a
	// structured InsufficientLoadDetail; selected list is empty.
	OutcomeInsufficientLoad
)

// InsufficientLoadDetail is what the handler echoes back to the caller when
// a request fails for lack of curtailable load. Each field maps to a
// diagnostic field in the structured Connect error detail so the UI can
// render "max X kW available, requested Y kW; N miners excluded by ..." .
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
	ExcludedPairing        int32
	ExcludedCooldown       int32
	ExcludedCapabilityMiss int32
	ExcludedActiveEvent    int32
}

// Result is the mode's output. Selected is the chosen set in dispatch order;
// RealizedReductionW is the accumulated power_w of Selected. When Outcome is
// OutcomeInsufficientLoad, Selected is empty and InsufficientDetail is set.
type Result struct {
	Outcome            Outcome
	Selected           []Candidate
	RealizedReductionW float64
	InsufficientDetail *InsufficientLoadDetail
}

// Mode applies mode-specific selection to a ranked candidate list. v1 has
// one implementor (FixedKw); closed-loop modes will add their own.
type Mode interface {
	// Select walks ranked candidates and returns the chosen subset plus
	// outcome. Implementations MUST be pure: no I/O, no time, no shared
	// state. The selector handles candidate construction; the mode handles
	// only the stop condition.
	Select(ranked []Candidate) Result
}
