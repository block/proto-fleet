package curtailment

import (
	"sort"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/modes"
)

// SkipReason is the canonical reason vocabulary surfaced in
// PreviewCurtailmentPlanResponse.skipped_candidates and stored in the
// decision_snapshot at Start time. The strings are stable contract values —
// downstream consumers (UI, audit, metrics) read them directly.
type SkipReason string

const (
	SkipBelowThreshold           SkipReason = "below_candidate_min_power_w"
	SkipPhantomLoadNoHash        SkipReason = "phantom_load_no_hash"
	SkipPowerTelemetryUnreliable SkipReason = "power_telemetry_unreliable"
	SkipStaleTelemetry           SkipReason = "stale_telemetry"
	SkipUnreachableResidualLoad  SkipReason = "unreachable_residual_load"
	SkipUpdating                 SkipReason = "updating"
	SkipRebootRequired           SkipReason = "reboot_required"
	SkipMaintenance              SkipReason = "maintenance"
	SkipNonActionableStatus      SkipReason = "non_actionable_status"
	SkipPairing                  SkipReason = "pairing"
	// Reserved for full capability gating: candidates whose loaded plugin
	// or model does not advertise curtail_full are skipped with this reason.
	// Kept in the SkipReason vocabulary so downstream consumers (UI, audit)
	// can treat the value as stable contract before the registry-driven
	// producer is wired in.
	SkipCurtailFullUnsupported SkipReason = "curtail_full_unsupported"
	SkipCooldown               SkipReason = "cooldown"
	SkipActiveEvent            SkipReason = "active_event"
)

// CandidateInput is one device's pre-aggregated state at selection time.
type CandidateInput struct {
	DeviceIdentifier string
	// PowerW is the latest power_w sample; used by both the dual-signal
	// filter and realized-kW accumulation.
	PowerW float64
	// HashRateHS is the latest hash_rate_hs sample; dual-signal filter
	// requires > 0 to admit.
	HashRateHS float64
	// AvgEfficiencyJH is the hourly j/h aggregate used for ranking. nil =
	// unknown efficiency, ranked last (avoids COALESCE-to-zero artifact).
	AvgEfficiencyJH *float64
}

// SkippedDevice is a per-device exclusion record returned alongside the
// selected list so the Preview response carries the full diagnostic.
type SkippedDevice struct {
	DeviceIdentifier string
	Reason           SkipReason
}

// Plan is the selector's output; the handler maps it to the proto response.
type Plan struct {
	Selected             []SelectedDevice
	Skipped              []SkippedDevice
	EstimatedReductionKW float64
	// EstimatedRemainingPowerKW is the unselected eligible power_w sum,
	// for the UI's "X kW selected, Y kW remaining" breakdown.
	EstimatedRemainingPowerKW float64
	Outcome                   modes.Outcome
	// InsufficientLoadDetail is set only on OutcomeInsufficientLoad.
	InsufficientLoadDetail *modes.InsufficientLoadDetail
	// EventUUID is set by Service.Start after persisting; nil for Preview.
	EventUUID *uuid.UUID
	// EffectiveMaxDurationSeconds is the persisted cap after Service.Start
	// resolves the "use org default" sentinel. nil when AllowUnbounded=true
	// or for Preview.
	EffectiveMaxDurationSeconds *int32
}

// SelectedDevice is a candidate the mode picked, carrying the snapshot
// stats the selector ranked against.
type SelectedDevice struct {
	DeviceIdentifier string
	PowerW           float64
	EfficiencyJH     float64
}

// BuildPlan runs the selection pipeline (dual-signal filter, rank by
// worst avg_efficiency first, hand off to the mode for the stop condition).
// `preFiltered` carries upstream skips (status/pairing/cooldown/capability)
// through to the Plan's Skipped list unchanged.
//
// Pure: no time, no I/O, no shared state.
func BuildPlan(
	inputs []CandidateInput,
	preFiltered []SkippedDevice,
	candidateMinPowerW int32,
	mode modes.Mode,
) Plan {
	const wPerKW = 1000.0

	skipped := make([]SkippedDevice, 0, len(preFiltered)+len(inputs))
	skipped = append(skipped, preFiltered...)

	// Track dual-signal counts locally; merged into the mode's rejection
	// detail post-Select so the mode interface stays oblivious.
	var dualSignalCounts struct {
		belowThreshold int32
		phantomLoad    int32
		deadMonitor    int32
	}

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
			dualSignalCounts.belowThreshold++
		case c.PowerW < float64(candidateMinPowerW):
			// Hashing but power reads near zero: dead/broken AC monitor.
			// Curtailing succeeds but reconciler can't verify.
			skipped = append(skipped, SkippedDevice{
				DeviceIdentifier: c.DeviceIdentifier,
				Reason:           SkipPowerTelemetryUnreliable,
			})
			dualSignalCounts.deadMonitor++
		case c.HashRateHS <= 0:
			// Drawing power but not hashing: phantom load — no real
			// hashrate to lose, fictional kW reduction.
			skipped = append(skipped, SkippedDevice{
				DeviceIdentifier: c.DeviceIdentifier,
				Reason:           SkipPhantomLoadNoHash,
			})
			dualSignalCounts.phantomLoad++
		default:
			eligible = append(eligible, c)
		}
	}

	// Stable worst-J/H-first rank; unknowns last; equal-efficiency input
	// order preserved so plans are reproducible.
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

	// Merge dual-signal counts into the rejection detail; pre-selector
	// summary covers status/pairing/cooldown, dual-signal pass runs here.
	if res.InsufficientDetail != nil {
		res.InsufficientDetail.ExcludedBelowThreshold += dualSignalCounts.belowThreshold
		res.InsufficientDetail.ExcludedPhantomLoad += dualSignalCounts.phantomLoad
		res.InsufficientDetail.ExcludedDeadMonitor += dualSignalCounts.deadMonitor
	}

	selected := make([]SelectedDevice, len(res.Selected))
	for i, c := range res.Selected {
		selected[i] = SelectedDevice{
			DeviceIdentifier: c.DeviceIdentifier,
			PowerW:           c.PowerW,
			EfficiencyJH:     c.EfficiencyJH,
		}
	}

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
