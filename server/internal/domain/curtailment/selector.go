package curtailment

import (
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
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
	// HashRateHS is the latest hash_rate_hs sample; fixed-kW's dual-signal
	// filter requires > 0 to admit.
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
	// StartedAt is set by Service.Start for events inserted already active;
	// echoed in the Start response so it matches the stamped row.
	StartedAt *time.Time
	// EndedAt is set by Service.Start only when an event is persisted already
	// terminal (an empty FULL_FLEET start); echoed in the Start response so it
	// matches the stamped row. nil otherwise.
	EndedAt *time.Time
	// EffectiveMaxDurationSeconds is the persisted cap after Service.Start
	// resolves the "use org default" sentinel. nil when AllowUnbounded=true
	// or for Preview.
	EffectiveMaxDurationSeconds *int32
	// EffectiveRestoreBatchIntervalSec is the persisted inter-batch delay.
	// Zero means no delay.
	EffectiveRestoreBatchIntervalSec int32
	// EffectiveCurtailBatchSize is the persisted curtail batch size. nil means
	// all selected targets in scope. Zero for Preview.
	EffectiveCurtailBatchSize *int32
	// EffectiveCurtailBatchIntervalSec is the persisted curtail inter-batch
	// delay. Zero means no delay.
	EffectiveCurtailBatchIntervalSec int32
	// EffectiveBatchSize is the restore batch size stamped on the event row at
	// Start time. Zero for Preview or starts with no selected targets. Echoed
	// in the Start response; Stop and the reconciler read it from the
	// persisted event row, not from Plan.
	EffectiveBatchSize int32
	// ReplayEvent is set only for idempotent Start replays. The handler uses
	// the persisted row instead of rebuilding a response from the retry body.
	ReplayEvent            *models.Event
	ReplayTargets          []*models.Target
	PolicyTargetCount      int
	UnavailableTargetCount int
}

// SelectedDevice is a candidate the mode picked, carrying the snapshot
// stats the selector ranked against.
type SelectedDevice struct {
	DeviceIdentifier string
	PowerW           float64
	// HashRateHS is the latest hash sample at selection time. Baseline
	// persistence reads it: the min-power floor only applies to hashing
	// miners, where the hash-only confirm/restore fallback works.
	HashRateHS   float64
	EfficiencyJH float64
	TargetState  models.TargetState
	LastError    string
}

// BuildPlan runs the selection pipeline (mode-specific eligibility filter, rank
// by worst avg_efficiency first, hand off to the mode for the stop condition).
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
	if mode.RequiresDualSignalTelemetry() {
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
	} else {
		eligible = append(eligible, inputs...)
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

	// modes.Candidate doesn't carry hash; recover it for the selected set so
	// baseline persistence can tell hashing miners from idle ones.
	hashByDevice := make(map[string]float64, len(inputs))
	for _, c := range inputs {
		hashByDevice[c.DeviceIdentifier] = c.HashRateHS
	}

	selected := make([]SelectedDevice, len(res.Selected))
	for i, c := range res.Selected {
		selected[i] = SelectedDevice{
			DeviceIdentifier: c.DeviceIdentifier,
			PowerW:           c.PowerW,
			HashRateHS:       hashByDevice[c.DeviceIdentifier],
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
		PolicyTargetCount:         len(selected),
	}
}

const (
	allPairedUnavailableAuthenticationNeeded = "authentication_needed"
	allPairedUnavailableNoDriver             = "no_driver"
	allPairedUnavailableMissingStatus        = "missing_status"
	allPairedUnavailableOffline              = "offline"
	allPairedUnavailableUpdating             = "updating"
	allPairedUnavailableRebootRequired       = "reboot_required"
	allPairedUnavailableNonActionableStatus  = "non_actionable_status"
	allPairedUnavailableMaintenance          = "maintenance"
	// allPairedUnavailableStaleTelemetry parks a pool-less miner whose power
	// telemetry is missing or zero. Power-vs-baseline is the only signal that
	// can confirm curtail/restore for a never-hashing miner, so dispatching
	// without a positive power sample would confirm curtail vacuously and
	// never confirm restore. Reuses the stale_telemetry vocabulary the normal
	// classifier emits for the same condition.
	allPairedUnavailableStaleTelemetry = "stale_telemetry"
)

// deviceStatusNeedsMiningPool is the device_status_enum value for a reachable
// miner with no pool configured. Referenced by both admission classifiers and
// the status-authoritative hash override below.
const deviceStatusNeedsMiningPool = "NEEDS_MINING_POOL"

// statusAuthoritativeHashRateHS returns the hash sample to use for selection
// accounting and baseline-persistence decisions. Device status is
// authoritative over the raw sample for NEEDS_MINING_POOL: a pool-less miner
// cannot be mining, so a stale-positive or inconsistent hash sample must not
// let it count as curtailable mining load in fixed-kW selection, nor mark it
// "hashing" for the baseline min-power floor.
func statusAuthoritativeHashRateHS(c *models.Candidate) float64 {
	if c.DeviceStatus == deviceStatusNeedsMiningPool {
		return 0
	}
	if hasNonNegativeFiniteFloat(c.LatestHashRateHS) {
		return *c.LatestHashRateHS
	}
	return 0
}

// hasPositivePowerSample reports whether the candidate carries a usable
// positive power reading — the admission requirement for miners whose only
// confirmable signal is power.
func hasPositivePowerSample(c *models.Candidate) bool {
	return hasNonNegativeFiniteFloat(c.LatestPowerW) && *c.LatestPowerW > 0
}

// BuildAllPairedPolicyPlan creates a durable FULL_FLEET target list from every
// paired-like miner in scope. It separates ownership from dispatch readiness:
// unavailable targets are persisted but kept out of the dispatch queue until a
// later reconciler tick marks them pending.
func BuildAllPairedPolicyPlan(
	candidates []*models.Candidate,
	activeEventDevices map[string]struct{},
	includeMaintenance bool,
	minPowerW int32,
) Plan {
	selected := make([]SelectedDevice, 0, len(candidates))
	skipped := make([]SkippedDevice, 0, len(candidates))
	var estimatedReductionW float64
	unavailableCount := 0

	for _, c := range candidates {
		if c == nil {
			continue
		}
		if _, locked := activeEventDevices[c.DeviceIdentifier]; locked {
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipActiveEvent})
			continue
		}
		if !IsAllPairedPolicyPairingStatus(c.PairingStatus) {
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipPairing})
			continue
		}

		state, reason := AllPairedPolicyTargetState(c, includeMaintenance)
		powerW := 0.0
		if state != models.TargetStateUnavailable && hasNonNegativeFiniteFloat(c.LatestPowerW) {
			powerW = derefFloat(c.LatestPowerW)
			estimatedReductionW += powerW
		}
		avgEff := c.AvgEfficiencyJH
		if !isFiniteFloat(avgEff) {
			avgEff = nil
		}
		if state == models.TargetStateUnavailable {
			unavailableCount++
		}
		selected = append(selected, SelectedDevice{
			DeviceIdentifier: c.DeviceIdentifier,
			PowerW:           powerW,
			HashRateHS:       statusAuthoritativeHashRateHS(c),
			EfficiencyJH:     derefFloat(avgEff),
			TargetState:      state,
			LastError:        reason,
		})
	}

	return Plan{
		Selected:               selected,
		Skipped:                skipped,
		EstimatedReductionKW:   estimatedReductionW / 1000.0,
		Outcome:                modes.OutcomeTargetReached,
		PolicyTargetCount:      len(selected),
		UnavailableTargetCount: unavailableCount,
	}
}

// AllPairedPolicyTargetState maps a paired-like candidate to its initial
// policy target state. It deliberately diverges from classifyCandidates
// (service.go) on ERROR/UNKNOWN: normal selection admits them when telemetry
// is fresh, while this policy dispatches without telemetry gates and so holds
// every non-commandable status unavailable until it clears.
// NEEDS_MINING_POOL is pending in both (#663): the miner is reachable and
// draws idle power, so a sleep command lands regardless of mining status.
// INACTIVE stays parked: it means the miner is already sleeping, and
// restoring it would wake a miner someone deliberately put to sleep. When
// adding a device status, update both switches and the pinned matrix in
// TestDeviceStatusClassifierMatrix.
func AllPairedPolicyTargetState(c *models.Candidate, includeMaintenance bool) (models.TargetState, string) {
	if c == nil {
		return models.TargetStateUnavailable, allPairedUnavailableMissingStatus
	}
	if c.PairingStatus == "AUTHENTICATION_NEEDED" {
		return models.TargetStateUnavailable, allPairedUnavailableAuthenticationNeeded
	}
	if c.DriverName == nil || *c.DriverName == "" {
		return models.TargetStateUnavailable, allPairedUnavailableNoDriver
	}
	switch c.DeviceStatus {
	case "":
		return models.TargetStateUnavailable, allPairedUnavailableMissingStatus
	case "OFFLINE":
		return models.TargetStateUnavailable, allPairedUnavailableOffline
	case "UPDATING":
		return models.TargetStateUnavailable, allPairedUnavailableUpdating
	case "REBOOT_REQUIRED":
		return models.TargetStateUnavailable, allPairedUnavailableRebootRequired
	case "INACTIVE", "ERROR", "UNKNOWN":
		return models.TargetStateUnavailable, allPairedUnavailableNonActionableStatus
	case deviceStatusNeedsMiningPool:
		// Commandable (#663), but only dispatchable once a positive power
		// sample exists: power-vs-baseline is the sole signal that can
		// confirm curtail/restore for a never-hashing miner. Parked rows are
		// re-evaluated every reconciler tick and promote (with a baseline
		// backfill) once telemetry lands.
		if !hasPositivePowerSample(c) {
			return models.TargetStateUnavailable, allPairedUnavailableStaleTelemetry
		}
		return models.TargetStatePending, ""
	case "MAINTENANCE":
		if !includeMaintenance {
			return models.TargetStateUnavailable, allPairedUnavailableMaintenance
		}
		return models.TargetStatePending, ""
	default:
		return models.TargetStatePending, ""
	}
}

func IsAllPairedPolicyPairingStatus(status string) bool {
	switch status {
	case "PAIRED", "DEFAULT_PASSWORD", "AUTHENTICATION_NEEDED":
		return true
	default:
		return false
	}
}

// AllPairedPromotionBaselinePowerW returns the pre-curtail baseline to
// backfill when an unavailable policy row becomes dispatchable, or nil when
// current telemetry does not meet the same bar the insert path applies.
// Rows inserted while a miner was unavailable carry no baseline (power was
// unknown); without a backfill at promotion, drift/confirm checks degrade to
// the hash-only fallback for the row's whole lifetime.
func AllPairedPromotionBaselinePowerW(c *models.Candidate, minPowerW int32) *float64 {
	if c == nil || !hasNonNegativeFiniteFloat(c.LatestPowerW) {
		return nil
	}
	power := *c.LatestPowerW
	hashing := statusAuthoritativeHashRateHS(c) > 0
	if !shouldPersistBaselinePowerW(models.ModeFullFleet, power, minPowerW, hashing) {
		return nil
	}
	return &power
}
