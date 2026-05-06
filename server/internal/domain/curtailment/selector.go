package curtailment

import (
	"sort"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/modes"
)

// SkipReason is the canonical reason vocabulary surfaced in
// PreviewCurtailmentPlanResponse.skipped_candidates and stored in the
// decision_snapshot at Start time. The strings are stable contract values —
// downstream consumers (UI, BE-3 audit, BE-5 metrics) read them directly.
type SkipReason string

const (
	SkipBelowThreshold        SkipReason = "below_candidate_min_power_w"
	SkipPhantomLoadNoHash     SkipReason = "phantom_load_no_hash"
	SkipPowerTelemetryUnreliable SkipReason = "power_telemetry_unreliable"
	SkipStaleTelemetry        SkipReason = "stale_telemetry"
	SkipUnreachableResidualLoad SkipReason = "unreachable_residual_load"
	SkipUpdating              SkipReason = "updating"
	SkipRebootRequired        SkipReason = "reboot_required"
	SkipMaintenance           SkipReason = "maintenance"
	SkipPairing               SkipReason = "pairing"
	SkipCurtailFullUnsupported SkipReason = "curtail_full_unsupported"
	SkipCooldown              SkipReason = "cooldown"
	SkipActiveEvent           SkipReason = "active_event"
)

// CandidateInput is one device's pre-aggregated state at selection time:
// telemetry snapshot, lifecycle status, hourly efficiency for ranking. The
// service layer assembles these from the relevant stores; the selector
// applies filter / rank / mode purely against this input.
type CandidateInput struct {
	DeviceIdentifier string
	// PowerW is the latest device_metrics.power_w sample. Used for both the
	// dual-signal filter and the realized-kW accumulation.
	PowerW float64
	// HashRateHS is the latest device_metrics.hash_rate_hs sample. The
	// dual-signal filter requires hash_rate > 0 to admit a candidate.
	HashRateHS float64
	// AvgEfficiencyJH is the device_metrics_hourly continuous-aggregate
	// value (joules/hash) used for ranking. A nil pointer signals "unknown
	// efficiency" — the selector ranks unknowns last so they are not
	// silently treated as best-in-class via a COALESCE-to-zero artifact.
	AvgEfficiencyJH *float64
}

// SkippedDevice carries a per-device exclusion record. The selector returns
// these alongside the selected list so the Preview response can surface the
// full diagnostic picture without a second query.
type SkippedDevice struct {
	DeviceIdentifier string
	Reason           SkipReason
}

// Plan is the selector's output. The handler maps this to the proto response.
type Plan struct {
	Selected             []SelectedDevice
	Skipped              []SkippedDevice
	EstimatedReductionKW float64
	// EstimatedRemainingPowerKW is the total power_w of the not-selected
	// portion of the candidate set (sum of unselected eligible candidates).
	// Useful for the UI's "X kW selected, Y kW remaining" breakdown.
	EstimatedRemainingPowerKW float64
	// Outcome echoes the mode's outcome so the handler can distinguish
	// target-reached, undershoot-tolerated, and insufficient-load.
	Outcome modes.Outcome
	// InsufficientLoadDetail is set only when Outcome == OutcomeInsufficientLoad.
	InsufficientLoadDetail *modes.InsufficientLoadDetail
}

// SelectedDevice is a candidate the mode picked for curtailment. Carries
// the same telemetry the selector ranked against so the handler can echo
// per-device stats back to the caller without a re-query.
type SelectedDevice struct {
	DeviceIdentifier string
	PowerW           float64
	EfficiencyJH     float64
}

// BuildPlan applies the v1 selection pipeline against `inputs` (the per-device
// state pre-aggregated by the service layer):
//
//  1. Dual-signal filter: require power_w >= candidateMinPowerW AND hash_rate > 0.
//     Skip with phantom_load_no_hash / power_telemetry_unreliable / below_candidate_min_power_w
//     accordingly. (Status / pairing / cooldown / capability filters happen
//     upstream in the service layer; their skip reasons arrive in `preFiltered`.)
//  2. Rank by avg_efficiency descending — worst J/H first. Unknown efficiency
//     ranks LAST (not first via COALESCE-to-zero), so an unranked miner does
//     not silently get treated as best-in-class.
//  3. Apply the mode. The mode owns the stop condition; the selector just
//     passes the ranked candidate list through.
//
// `preFiltered` is the list of devices already-skipped before reaching the
// dual-signal filter (e.g., wrong device_status, unpaired, in cooldown). The
// selector forwards them into the Plan's Skipped list without re-evaluating.
//
// The function is pure: no time, no I/O, no shared state. All inputs flow
// through the parameters; all outputs through the return value.
func BuildPlan(
	inputs []CandidateInput,
	preFiltered []SkippedDevice,
	candidateMinPowerW int32,
	mode modes.Mode,
) Plan {
	const wPerKW = 1000.0

	skipped := make([]SkippedDevice, 0, len(preFiltered)+len(inputs))
	skipped = append(skipped, preFiltered...)

	eligible := make([]CandidateInput, 0, len(inputs))
	for _, c := range inputs {
		switch {
		case c.PowerW < float64(candidateMinPowerW) && c.HashRateHS <= 0:
			// Both signals fail — most likely a fully-idle/dead miner.
			// Skip below_threshold which carries the most actionable
			// diagnostic for ops (lower the floor for S9/S15 fleets).
			skipped = append(skipped, SkippedDevice{
				DeviceIdentifier: c.DeviceIdentifier,
				Reason:           SkipBelowThreshold,
			})
		case c.PowerW < float64(candidateMinPowerW):
			// Hashing but drawing too little power — unreliable power
			// telemetry (broken sensor, etc.). Curtailing succeeds but
			// reconciler can't verify.
			skipped = append(skipped, SkippedDevice{
				DeviceIdentifier: c.DeviceIdentifier,
				Reason:           SkipPowerTelemetryUnreliable,
			})
		case c.HashRateHS <= 0:
			// Drawing power but not hashing — phantom load. Curtailing
			// records a fictional kW reduction with no real hashrate
			// to lose.
			skipped = append(skipped, SkippedDevice{
				DeviceIdentifier: c.DeviceIdentifier,
				Reason:           SkipPhantomLoadNoHash,
			})
		default:
			eligible = append(eligible, c)
		}
	}

	// Stable rank: known efficiency first (descending — worse first), then
	// unknown efficiency at the bottom. Stable preserves input order for
	// equal-efficiency miners so the plan is reproducible across calls.
	sort.SliceStable(eligible, func(i, j int) bool {
		ei, ej := eligible[i].AvgEfficiencyJH, eligible[j].AvgEfficiencyJH
		switch {
		case ei == nil && ej == nil:
			return false
		case ei == nil:
			return false // i (unknown) goes after j (known)
		case ej == nil:
			return true // i (known) goes before j (unknown)
		default:
			return *ei > *ej // worst-J/H first
		}
	})

	ranked := make([]modes.Candidate, len(eligible))
	for i, c := range eligible {
		eff := 0.0
		if c.AvgEfficiencyJH != nil {
			eff = *c.AvgEfficiencyJH
		}
		ranked[i] = modes.Candidate{
			DeviceIdentifier: c.DeviceIdentifier,
			PowerW:           c.PowerW,
			EfficiencyJH:     eff,
		}
	}

	res := mode.Select(ranked)

	// Map the mode's selected list back to SelectedDevice carrying the
	// snapshot stats the UI renders.
	selected := make([]SelectedDevice, len(res.Selected))
	for i, c := range res.Selected {
		selected[i] = SelectedDevice{
			DeviceIdentifier: c.DeviceIdentifier,
			PowerW:           c.PowerW,
			EfficiencyJH:     c.EfficiencyJH,
		}
	}

	// Compute remaining power: total eligible minus the selected slice.
	totalEligibleW := 0.0
	for _, c := range ranked {
		totalEligibleW += c.PowerW
	}
	remainingW := totalEligibleW - res.RealizedReductionW

	return Plan{
		Selected:                  selected,
		Skipped:                   skipped,
		EstimatedReductionKW:      res.RealizedReductionW / wPerKW,
		EstimatedRemainingPowerKW: remainingW / wPerKW,
		Outcome:                   res.Outcome,
		InsufficientLoadDetail:    res.InsufficientDetail,
	}
}
