package curtailment

import (
	"context"
	"math"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/modes"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// Scope identifies the target set: whole-org or explicit device-list in v1;
// device-sets are deferred (resolver lives outside the curtailment domain).
type Scope struct {
	Type              models.ScopeType
	DeviceSetIDs      []string
	DeviceIdentifiers []string
}

// PreviewRequest is the service-level shape of a Preview call.
type PreviewRequest struct {
	OrgID                      int64
	Scope                      Scope
	Mode                       string // v1: must be "FIXED_KW"
	Strategy                   string // v1: default LEAST_EFFICIENT_FIRST
	Level                      string // v1: must be "FULL"
	Priority                   string // "NORMAL" or "EMERGENCY" (cooldown bypass)
	TargetKW                   float64
	ToleranceKW                float64
	IncludeMaintenance         bool
	ForceIncludeMaintenance    bool
	CandidateMinPowerWOverride *int32 // nil = use org default; admin-gated by handler
}

// Service orchestrates Preview through the config / scope / candidate / selector pipeline.
type Service struct {
	store interfaces.CurtailmentStore
}

func NewService(store interfaces.CurtailmentStore) *Service {
	return &Service{store: store}
}

// Preview computes a curtailment plan without persisting any rows. Returns
// fleeterror typed errors the handler maps to Connect codes.
func (s *Service) Preview(ctx context.Context, req PreviewRequest) (*Plan, error) {
	if err := validatePreviewRequest(req); err != nil {
		return nil, err
	}

	deviceFilter, err := resolveScope(req.Scope)
	if err != nil {
		return nil, err
	}
	// Normalize empty-but-non-nil slice to nil; the candidate query's
	// `IS NULL` check would otherwise match-nothing on an empty array.
	if len(deviceFilter) == 0 {
		deviceFilter = nil
	}

	orgConfig, err := s.store.GetOrgConfig(ctx, req.OrgID)
	if err != nil {
		return nil, err
	}

	// Effective candidate floor: per-org default, optionally overridden by
	// the admin-gated request field. Handler enforces the admin role gate.
	minPowerW := orgConfig.CandidateMinPowerW
	if req.CandidateMinPowerWOverride != nil {
		minPowerW = *req.CandidateMinPowerWOverride
	}

	// Cooldown bypass: EMERGENCY priority skips post_event_cooldown_sec.
	bypassCooldown := req.Priority == "EMERGENCY"

	activeDevices, err := s.store.ListActiveCurtailedDevices(ctx, req.OrgID)
	if err != nil {
		return nil, err
	}
	activeSet := toStringSet(activeDevices)

	cooldownSet := map[string]struct{}{}
	if !bypassCooldown {
		cd, err := s.store.ListRecentlyResolvedCurtailedDevices(ctx, req.OrgID, orgConfig.PostEventCooldownSec)
		if err != nil {
			return nil, err
		}
		cooldownSet = toStringSet(cd)
	}

	candidates, err := s.store.ListCandidates(ctx, req.OrgID, deviceFilter)
	if err != nil {
		return nil, err
	}

	// Org-ownership guard: cross-org ids are silently dropped by the SQL
	// org_id filter; surface them as NotFound so the caller sees the real
	// error instead of a misleading InsufficientLoad.
	if len(deviceFilter) > 0 {
		if missing := missingDeviceIdentifiers(deviceFilter, candidates); len(missing) > 0 {
			return nil, fleeterror.NewNotFoundErrorf(
				"device_identifiers not found in caller's org: %v", missing,
			)
		}
	}

	// TODO: registry-driven curtail_full capability check. classifyCandidates
	// already skips devices missing driver metadata as defense-in-depth.

	eligible, preFiltered, summary := classifyCandidates(candidates, classifyOpts{
		IncludeMaintenance: req.IncludeMaintenance && req.ForceIncludeMaintenance,
		ActiveEventDevices: activeSet,
		CooldownDevices:    cooldownSet,
		CandidateMinPowerW: minPowerW,
	})

	mode, err := modes.NewFixedKw(req.TargetKW, req.ToleranceKW, summary)
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("invalid FIXED_KW params: %v", err)
	}

	plan := BuildPlan(eligible, preFiltered, minPowerW, mode)
	return &plan, nil
}

func validatePreviewRequest(req PreviewRequest) error {
	if req.Mode != "" && req.Mode != "FIXED_KW" {
		return fleeterror.NewInvalidArgumentErrorf("mode %q is not supported in v1; only FIXED_KW", req.Mode)
	}
	if req.Level != "" && req.Level != "FULL" {
		return fleeterror.NewInvalidArgumentErrorf("level %q is not supported in v1; only FULL", req.Level)
	}
	if req.Strategy != "" && req.Strategy != "LEAST_EFFICIENT_FIRST" {
		return fleeterror.NewInvalidArgumentErrorf(
			"strategy %q is not supported in v1; only LEAST_EFFICIENT_FIRST", req.Strategy,
		)
	}
	// HIGH is proto-reserved but undesigned in v1; reject explicitly.
	if req.Priority != "" && req.Priority != "NORMAL" && req.Priority != "EMERGENCY" {
		return fleeterror.NewInvalidArgumentErrorf(
			"priority %q is not supported in v1; use NORMAL or EMERGENCY", req.Priority,
		)
	}
	// NaN / +/-Inf must be rejected explicitly because every comparison with
	// NaN evaluates false, which would slip past the > 0 / >= 0 guards
	// below and propagate through the running sum in FixedKw.
	if math.IsNaN(req.TargetKW) || math.IsInf(req.TargetKW, 0) {
		return fleeterror.NewInvalidArgumentErrorf("target_kw must be a finite number, got %v", req.TargetKW)
	}
	if math.IsNaN(req.ToleranceKW) || math.IsInf(req.ToleranceKW, 0) {
		return fleeterror.NewInvalidArgumentErrorf("tolerance_kw must be a finite number, got %v", req.ToleranceKW)
	}
	if req.TargetKW <= 0 {
		return fleeterror.NewInvalidArgumentErrorf("target_kw must be > 0, got %v", req.TargetKW)
	}
	if req.ToleranceKW < 0 {
		return fleeterror.NewInvalidArgumentErrorf("tolerance_kw must be >= 0, got %v", req.ToleranceKW)
	}
	// tolerance_kw >= target_kw makes the undershoot branch trivially pass
	// even when the candidate sum is zero, producing an empty plan that
	// looks like a successful preview. Reject so the caller sees the real
	// reason (insufficient load) rather than a no-op selection.
	if req.ToleranceKW >= req.TargetKW {
		return fleeterror.NewInvalidArgumentErrorf(
			"tolerance_kw must be < target_kw, got tolerance=%v target=%v",
			req.ToleranceKW, req.TargetKW,
		)
	}
	// candidate_min_power_w_override = 0 effectively disables the dual-signal
	// floor (every powered miner becomes a candidate). The proto declares the
	// field uint32 with documented bounds [1, 10_000_000]; this guard is the
	// service-level backstop for callers that bypass the proto validator
	// (internal CLIs, tests, future non-Connect surfaces).
	if req.CandidateMinPowerWOverride != nil && *req.CandidateMinPowerWOverride < 1 {
		return fleeterror.NewInvalidArgumentErrorf("candidate_min_power_w_override must be >= 1, got %d", *req.CandidateMinPowerWOverride)
	}
	// Maintenance override pair is both-or-neither at the API boundary;
	// the DB CHECK constraint is the defense-in-depth backstop at Start time.
	if req.IncludeMaintenance != req.ForceIncludeMaintenance {
		return fleeterror.NewInvalidArgumentError(
			"include_maintenance and force_include_maintenance must be set together",
		)
	}
	return nil
}

func resolveScope(s Scope) ([]string, error) {
	switch s.Type {
	case models.ScopeTypeWholeOrg, "":
		return nil, nil
	case models.ScopeTypeDeviceList:
		if len(s.DeviceIdentifiers) == 0 {
			return nil, fleeterror.NewInvalidArgumentError("device_identifiers must be non-empty for device-list scope")
		}
		return s.DeviceIdentifiers, nil
	case models.ScopeTypeDeviceSets:
		// Deferred: device-set resolution requires DeviceSetStore wiring
		// outside the curtailment domain. Whole-org and device-list cover
		// the v1 critical paths.
		return nil, fleeterror.NewUnimplementedErrorf("device-set scope is not implemented in v1; use whole_org or device_list")
	default:
		return nil, fleeterror.NewInvalidArgumentErrorf("unrecognized scope type: %q", s.Type)
	}
}

type classifyOpts struct {
	IncludeMaintenance bool
	ActiveEventDevices map[string]struct{}
	CooldownDevices    map[string]struct{}
	CandidateMinPowerW int32
}

// classifyCandidates partitions candidates into selector inputs and a
// pre-selector skipped list with reasons; summary counts are incremented in
// lockstep so the rejection branch can echo per-reason totals without a re-walk.
func classifyCandidates(cands []*models.Candidate, opts classifyOpts) ([]CandidateInput, []SkippedDevice, modes.InsufficientLoadDetail) {
	eligible := make([]CandidateInput, 0, len(cands))
	skipped := make([]SkippedDevice, 0, len(cands))
	summary := modes.InsufficientLoadDetail{
		CandidateMinPowerW: opts.CandidateMinPowerW,
	}

	for _, c := range cands {
		if _, locked := opts.ActiveEventDevices[c.DeviceIdentifier]; locked {
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipActiveEvent})
			summary.ExcludedActiveEvent++
			continue
		}
		if c.PairingStatus != "PAIRED" {
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipPairing})
			summary.ExcludedPairing++
			continue
		}
		// Partial capability gate: skip devices with no driver metadata so
		// the selector can't pick a Curtail target with no plugin to dispatch.
		// Full registry-driven curtail_full check is follow-up work.
		if c.DriverName == nil || *c.DriverName == "" {
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipCurtailFullUnsupported})
			summary.ExcludedCapabilityMiss++
			continue
		}
		switch c.DeviceStatus {
		case "":
			// COALESCE sentinel for a missing device_status row: treat as
			// stale, since we can't prove the device is curtail-safe.
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipStaleTelemetry})
			continue
		case "UPDATING":
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipUpdating})
			continue
		case "REBOOT_REQUIRED":
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipRebootRequired})
			continue
		case "OFFLINE":
			// Unreachable residual load: counted in the rejection summary
			// since it's fleet load the system can't address.
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipUnreachableResidualLoad})
			summary.ExcludedOffline++
			continue
		case "INACTIVE", "NEEDS_MINING_POOL":
			// Non-actionable per the project's nonActionableStatuses set
			// (sqlstores/device_query_fragments.go): the device isn't a
			// curtailment candidate even when telemetry is fresh.
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipNonActionableStatus})
			summary.ExcludedNonActionable++
			continue
		case "MAINTENANCE":
			if !opts.IncludeMaintenance {
				skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipMaintenance})
				summary.ExcludedMaintenance++
				continue
			}
			// Admitted by override pair; fall through to freshness.
		}
		if c.LatestMetricsAt == nil {
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipStaleTelemetry})
			continue
		}
		if _, cooled := opts.CooldownDevices[c.DeviceIdentifier]; cooled {
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipCooldown})
			summary.ExcludedCooldown++
			continue
		}
		eligible = append(eligible, CandidateInput{
			DeviceIdentifier: c.DeviceIdentifier,
			PowerW:           derefFloat(c.LatestPowerW),
			HashRateHS:       derefFloat(c.LatestHashRateHS),
			AvgEfficiencyJH:  c.AvgEfficiencyJH,
		})
	}
	return eligible, skipped, summary
}

// missingDeviceIdentifiers returns identifiers from `requested` that the org-
// scoped candidate listing did not surface. An empty result means every
// requested device belongs to the caller's org (or has been soft-deleted —
// soft-deleted devices are out of scope by design).
func missingDeviceIdentifiers(requested []string, candidates []*models.Candidate) []string {
	if len(requested) == 0 {
		return nil
	}
	have := make(map[string]struct{}, len(candidates))
	for _, c := range candidates {
		have[c.DeviceIdentifier] = struct{}{}
	}
	var missing []string
	for _, id := range requested {
		if _, ok := have[id]; !ok {
			missing = append(missing, id)
		}
	}
	return missing
}

func toStringSet(s []string) map[string]struct{} {
	set := make(map[string]struct{}, len(s))
	for _, v := range s {
		set[v] = struct{}{}
	}
	return set
}

func derefFloat(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}
