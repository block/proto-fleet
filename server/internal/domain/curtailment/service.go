package curtailment

import (
	"context"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/modes"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// Scope expresses how a curtailment request expressed its target set. v1
// supports whole-org and explicit device-list; device-set scope is deferred
// because the resolver lives outside the curtailment domain (DeviceSetStore
// wiring lands in a follow-up).
type Scope struct {
	Type              models.ScopeType
	DeviceSetIDs      []string
	DeviceIdentifiers []string
}

// PreviewRequest is the service-level shape of a Preview call. Decoupled
// from proto types so tests can drive the service without constructing
// PreviewCurtailmentPlanRequest messages.
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

// Service orchestrates Preview: load org config, resolve scope, build the
// candidate set with skip-reason attribution, hand to the selector + mode.
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

	orgConfig, err := s.store.GetOrgConfig(ctx, req.OrgID)
	if err != nil {
		return nil, err
	}

	// Effective candidate floor: per-org default, optionally overridden by
	// the admin-gated request field. The handler is responsible for the
	// admin role check (via requireAdminFromContext); the service trusts
	// that the override has cleared that gate.
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

	// Cross-org ownership guard for explicit device-list scope: the SQL
	// already filters by org_id, so any device_identifier the caller listed
	// that belongs to another org (or doesn't exist) is silently dropped.
	// Explicit miner-list scope must validate org ownership at this layer
	// before any persistence or dispatch path consumes the result; without
	// this check the caller sees a confusing InsufficientLoad instead of
	// "you don't own these IDs."
	if len(deviceFilter) > 0 {
		if missing := missingDeviceIdentifiers(deviceFilter, candidates); len(missing) > 0 {
			return nil, fleeterror.NewNotFoundErrorf(
				"device_identifiers not found in caller's org: %v", missing,
			)
		}
	}

	// TODO: extend the capability gate. classifyCandidates already skips
	// devices with no driver_name (defense-in-depth for a missing
	// discovered_device row), but the full check — does the loaded plugin
	// advertise curtail_full for this device's model? — needs the plugin
	// registry that is not yet wired in. Until then, devices with a known
	// driver but an unsupported model can slip through; the candidate query
	// already returns driver_name + model so the registry-driven check can
	// layer on without a schema change.

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
	if req.TargetKW <= 0 {
		return fleeterror.NewInvalidArgumentErrorf("target_kw must be > 0, got %v", req.TargetKW)
	}
	if req.ToleranceKW < 0 {
		return fleeterror.NewInvalidArgumentErrorf("tolerance_kw must be >= 0, got %v", req.ToleranceKW)
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

// classifyCandidates partitions the cross-table candidate rows into the
// pre-selector skipped list (with skip-reason attribution) and the selector
// inputs (devices that pass status / pairing / freshness / maintenance /
// cooldown / active-event filters and are ready for the dual-signal pass).
//
// The summary InsufficientLoadDetail is incremented in lockstep with the
// skips so the rejection branch can echo per-reason counts back to the
// caller without a re-walk.
func classifyCandidates(cands []*models.Candidate, opts classifyOpts) ([]CandidateInput, []SkippedDevice, modes.InsufficientLoadDetail) {
	eligible := make([]CandidateInput, 0, len(cands))
	skipped := make([]SkippedDevice, 0)
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
		// Partial capability gate (defense-in-depth, not a full check):
		// a device with no driver metadata cannot be curtailed because we
		// don't know which plugin would handle the dispatch. Skipping here
		// prevents the selector from picking a device whose Curtail call
		// would have nowhere to land. The full plugin-registry-driven
		// curtail_full check is follow-up work; this guard catches the
		// "discovered_device row missing" edge today.
		if c.DriverName == nil || *c.DriverName == "" {
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipCurtailFullUnsupported})
			summary.ExcludedCapabilityMiss++
			continue
		}
		switch c.DeviceStatus {
		case "":
			// Empty string is the COALESCE sentinel for a missing
			// device_status row (no status agent has reported yet, or the
			// device is brand-new). Treat as stale: we cannot prove the
			// device is curtail-safe without a recent status, and the
			// reconciler would have nothing to verify against.
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipStaleTelemetry})
			continue
		case "UPDATING":
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipUpdating})
			continue
		case "REBOOT_REQUIRED":
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipRebootRequired})
			continue
		case "OFFLINE":
			// "Unreachable residual load" — flagged in the skipped list so
			// the UI can render the residual-power detail; counted in the
			// rejection summary because it represents fleet load the
			// system cannot address.
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipUnreachableResidualLoad})
			summary.ExcludedOffline++
			continue
		case "MAINTENANCE":
			if !opts.IncludeMaintenance {
				skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipMaintenance})
				summary.ExcludedMaintenance++
				continue
			}
			// Maintenance miner explicitly admitted by the override pair.
			// Fall through to the freshness check.
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
