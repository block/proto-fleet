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
// (BE-2 acceptance lists it but the resolver lives outside the curtailment
// domain — defer to a follow-up that wires DeviceSetStore).
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
	// admin role check (via requireAdminFromContext from BE-1.x); the
	// service trusts that the override has cleared that gate.
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
		switch c.DeviceStatus {
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
