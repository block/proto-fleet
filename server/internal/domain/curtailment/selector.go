package curtailment

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/modes"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

const (
	reasonSelectedLeastEfficientFirst = "least_efficient_first"
	reasonStale                       = "stale"
	reasonPairing                     = "pairing"
	reasonDeviceStatusUnavailable     = "device_status_unavailable"
	reasonUnreachableResidualLoad     = "unreachable_residual_load"
	reasonMaintenance                 = "maintenance"
	reasonCurtailFullUnsupported      = "curtail_full_unsupported"
	reasonPhantomLoadNoHash           = "phantom_load_no_hash"
	reasonPowerTelemetryUnreliable    = "power_telemetry_unreliable"
	reasonNotCurrentlyHashing         = "not_currently_hashing"
	reasonActiveCurtailment           = "active_curtailment"
	reasonCooldown                    = "cooldown"
)

type normalizedPreviewRequest struct {
	original                *pb.PreviewCurtailmentPlanRequest
	scopeType               string
	deviceSetIDs            []int64
	deviceIdentifiers       []string
	mode                    pb.CurtailmentMode
	strategy                pb.CurtailmentStrategy
	level                   pb.CurtailmentLevel
	priority                pb.CurtailmentPriority
	includeMaintenance      bool
	forceIncludeMaintenance bool
}

type Selector struct {
	candidateMinPowerW   float64
	capabilitiesProvider CapabilitiesProvider
}

type plan struct {
	selected           []modes.Candidate
	skipped            []*pb.SkippedCandidate
	remainingPowerKW   float64
	estimatedReduction float64
}

func normalizePreviewRequest(req *pb.PreviewCurtailmentPlanRequest) (normalizedPreviewRequest, error) {
	normalized := normalizedPreviewRequest{
		original:                req,
		mode:                    req.GetMode(),
		strategy:                req.GetStrategy(),
		level:                   req.GetLevel(),
		priority:                req.GetPriority(),
		includeMaintenance:      req.GetIncludeMaintenance(),
		forceIncludeMaintenance: req.GetForceIncludeMaintenance(),
	}

	if normalized.strategy == pb.CurtailmentStrategy_CURTAILMENT_STRATEGY_UNSPECIFIED {
		normalized.strategy = pb.CurtailmentStrategy_CURTAILMENT_STRATEGY_LEAST_EFFICIENT_FIRST
	}
	if normalized.level == pb.CurtailmentLevel_CURTAILMENT_LEVEL_UNSPECIFIED {
		normalized.level = pb.CurtailmentLevel_CURTAILMENT_LEVEL_FULL
	}
	if normalized.priority == pb.CurtailmentPriority_CURTAILMENT_PRIORITY_UNSPECIFIED {
		normalized.priority = pb.CurtailmentPriority_CURTAILMENT_PRIORITY_NORMAL
	}

	if normalized.mode != pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW {
		return normalized, fleeterror.NewInvalidArgumentError("mode must be FIXED_KW")
	}
	if normalized.strategy != pb.CurtailmentStrategy_CURTAILMENT_STRATEGY_LEAST_EFFICIENT_FIRST {
		return normalized, fleeterror.NewInvalidArgumentError("strategy must be LEAST_EFFICIENT_FIRST")
	}
	if normalized.level != pb.CurtailmentLevel_CURTAILMENT_LEVEL_FULL {
		return normalized, fleeterror.NewInvalidArgumentError("level must be FULL")
	}
	if normalized.priority != pb.CurtailmentPriority_CURTAILMENT_PRIORITY_NORMAL &&
		normalized.priority != pb.CurtailmentPriority_CURTAILMENT_PRIORITY_EMERGENCY {
		return normalized, fleeterror.NewInvalidArgumentError("priority must be NORMAL or EMERGENCY")
	}
	if normalized.includeMaintenance && !normalized.forceIncludeMaintenance {
		return normalized, fleeterror.NewInvalidArgumentError("force_include_maintenance must be true when include_maintenance is true")
	}
	if !normalized.includeMaintenance && normalized.forceIncludeMaintenance {
		return normalized, fleeterror.NewInvalidArgumentError("include_maintenance must be true when force_include_maintenance is true")
	}

	switch scope := req.GetScope().(type) {
	case *pb.PreviewCurtailmentPlanRequest_WholeOrg:
		normalized.scopeType = interfaces.CurtailmentScopeWholeOrg
	case *pb.PreviewCurtailmentPlanRequest_DeviceSetIds:
		ids, err := parseDeviceSetIDs(scope.DeviceSetIds.GetDeviceSetIds())
		if err != nil {
			return normalized, err
		}
		normalized.scopeType = interfaces.CurtailmentScopeDeviceSets
		normalized.deviceSetIDs = ids
	case *pb.PreviewCurtailmentPlanRequest_DeviceIdentifiers:
		ids, err := normalizeDeviceIdentifiers(scope.DeviceIdentifiers.GetDeviceIdentifiers())
		if err != nil {
			return normalized, err
		}
		normalized.scopeType = interfaces.CurtailmentScopeDeviceList
		normalized.deviceIdentifiers = ids
	default:
		return normalized, fleeterror.NewInvalidArgumentError("exactly one scope must be set")
	}

	if err := validateModeParams(req); err != nil {
		return normalized, err
	}

	return normalized, nil
}

func validateModeParams(req *pb.PreviewCurtailmentPlanRequest) error {
	if req.GetMode() == pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW && req.GetFixedKw() == nil {
		return fleeterror.NewInvalidArgumentError("fixed_kw params are required for FIXED_KW mode")
	}
	return nil
}

func parseDeviceSetIDs(rawIDs []string) ([]int64, error) {
	if len(rawIDs) == 0 {
		return nil, fleeterror.NewInvalidArgumentError("device_set_ids must not be empty")
	}
	ids := make([]int64, 0, len(rawIDs))
	for _, raw := range rawIDs {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			return nil, fleeterror.NewInvalidArgumentError("device_set_ids must not contain empty values")
		}
		id, err := strconv.ParseInt(trimmed, 10, 64)
		if err != nil || id <= 0 {
			return nil, fleeterror.NewInvalidArgumentErrorf("invalid device_set_id: %q", raw)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func normalizeDeviceIdentifiers(rawIDs []string) ([]string, error) {
	if len(rawIDs) == 0 {
		return nil, fleeterror.NewInvalidArgumentError("device_identifiers must not be empty")
	}
	seen := make(map[string]struct{}, len(rawIDs))
	ids := make([]string, 0, len(rawIDs))
	for _, raw := range rawIDs {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			return nil, fleeterror.NewInvalidArgumentError("device_identifiers must not contain empty values")
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		ids = append(ids, trimmed)
	}
	return ids, nil
}

func (r normalizedPreviewRequest) storeParams(orgID int64, cooldownSince time.Time) (interfaces.CurtailmentPreviewDeviceParams, []string, error) {
	if r.scopeType == "" {
		return interfaces.CurtailmentPreviewDeviceParams{}, nil, fleeterror.NewInvalidArgumentError("scope is required")
	}
	params := interfaces.CurtailmentPreviewDeviceParams{
		OrgID:             orgID,
		ScopeType:         r.scopeType,
		DeviceSetIDs:      r.deviceSetIDs,
		DeviceIdentifiers: r.deviceIdentifiers,
		CooldownSince:     cooldownSince,
	}
	if params.DeviceSetIDs == nil {
		params.DeviceSetIDs = []int64{}
	}
	if params.DeviceIdentifiers == nil {
		params.DeviceIdentifiers = []string{}
	}
	var requested []string
	if r.scopeType == interfaces.CurtailmentScopeDeviceList {
		requested = r.deviceIdentifiers
	}
	return params, requested, nil
}

func ensureExplicitDevicesResolved(requested []string, devices []interfaces.CurtailmentPreviewDevice) error {
	if len(requested) == 0 {
		return nil
	}
	found := make(map[string]struct{}, len(devices))
	for _, device := range devices {
		found[device.DeviceIdentifier] = struct{}{}
	}
	for _, id := range requested {
		if _, ok := found[id]; !ok {
			return fleeterror.NewInvalidArgumentErrorf("device %q is not in the caller organization or does not exist", id)
		}
	}
	return nil
}

func (s Selector) BuildPlan(ctx context.Context, req normalizedPreviewRequest, devices []interfaces.CurtailmentPreviewDevice) (plan, error) {
	candidates, skipped := s.filterCandidates(ctx, req, devices)
	sortCandidates(candidates)

	mode, err := req.modeSelector()
	if err != nil {
		return plan{}, err
	}
	selected, err := mode.Select(candidates)
	if err != nil {
		return plan{}, err
	}

	selectedIDs := make(map[string]struct{}, len(selected))
	var reductionKW float64
	for _, candidate := range selected {
		selectedIDs[candidate.DeviceIdentifier] = struct{}{}
		reductionKW += candidate.CurrentPowerW / 1000
	}

	var remainingKW float64
	for _, candidate := range candidates {
		if _, ok := selectedIDs[candidate.DeviceIdentifier]; ok {
			continue
		}
		remainingKW += candidate.CurrentPowerW / 1000
	}

	return plan{
		selected:           selected,
		skipped:            skipped,
		remainingPowerKW:   remainingKW,
		estimatedReduction: reductionKW,
	}, nil
}

func (r normalizedPreviewRequest) modeSelector() (modes.Mode, error) {
	switch r.mode { //nolint:exhaustive // normalizePreviewRequest restricts v1 preview to fixed kW mode.
	case pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW:
		return modes.NewFixedKW(r.original.GetFixedKw()), nil
	default:
		return nil, fleeterror.NewInvalidArgumentError("unsupported curtailment mode")
	}
}

func (s Selector) filterCandidates(ctx context.Context, req normalizedPreviewRequest, devices []interfaces.CurtailmentPreviewDevice) ([]modes.Candidate, []*pb.SkippedCandidate) {
	candidates := make([]modes.Candidate, 0, len(devices))
	skipped := make([]*pb.SkippedCandidate, 0)
	for _, device := range devices {
		if reason, description := s.skipReason(ctx, req, device); reason != "" {
			skipped = append(skipped, &pb.SkippedCandidate{
				DeviceIdentifier: device.DeviceIdentifier,
				Reason:           reason,
				Description:      description,
			})
			continue
		}

		candidates = append(candidates, modes.Candidate{
			DeviceIdentifier: device.DeviceIdentifier,
			CurrentPowerW:    *device.CurrentPowerW,
			EfficiencyJH:     device.EfficiencyJH,
			ReasonSelected:   reasonSelectedLeastEfficientFirst,
		})
	}
	return candidates, skipped
}

func (s Selector) skipReason(ctx context.Context, req normalizedPreviewRequest, device interfaces.CurtailmentPreviewDevice) (string, string) {
	if device.LatestMetricAt == nil || device.CurrentPowerW == nil || device.RecentPowerW == nil || device.RecentHashRateHS == nil {
		return reasonStale, "no complete telemetry sample in the freshness window"
	}
	if device.PairingStatus != "PAIRED" {
		return reasonPairing, fmt.Sprintf("pairing status is %s", device.PairingStatus)
	}
	if device.DeviceStatus == nil {
		return reasonUnreachableResidualLoad, "device status is unknown and treated as offline"
	}
	switch *device.DeviceStatus {
	case "UPDATING", "REBOOT_REQUIRED":
		return reasonDeviceStatusUnavailable, fmt.Sprintf("device status is %s", *device.DeviceStatus)
	case "OFFLINE":
		return reasonUnreachableResidualLoad, "device is offline and cannot be verified safely"
	case "UNKNOWN":
		return reasonUnreachableResidualLoad, "device status is unknown and treated as offline"
	case "MAINTENANCE":
		if !req.includeMaintenance || !req.forceIncludeMaintenance {
			return reasonMaintenance, "device is in maintenance"
		}
	}
	if device.InActiveCurtailment {
		return reasonActiveCurtailment, "device is already targeted by an active curtailment event"
	}
	if !s.supportsFullCurtailment(ctx, device) {
		return reasonCurtailFullUnsupported, "loaded plugin/model capabilities do not advertise full curtailment"
	}

	currentPowerPasses := *device.CurrentPowerW >= s.candidateMinPowerW
	recentPowerPasses := *device.RecentPowerW >= s.candidateMinPowerW
	hashPasses := *device.RecentHashRateHS > 0
	switch {
	case currentPowerPasses && recentPowerPasses && hashPasses:
	case currentPowerPasses && recentPowerPasses && !hashPasses:
		return reasonPhantomLoadNoHash, "power telemetry passes but hashrate is zero"
	case (!currentPowerPasses || !recentPowerPasses) && hashPasses:
		return reasonPowerTelemetryUnreliable, "hashrate passes but power telemetry is below the curtailable threshold"
	default:
		return reasonNotCurrentlyHashing, "power and hashrate are below the curtailable threshold"
	}

	if device.InCooldown && req.priority != pb.CurtailmentPriority_CURTAILMENT_PRIORITY_EMERGENCY {
		return reasonCooldown, "device was recently restored or restore-failed"
	}

	return "", ""
}

func (s Selector) supportsFullCurtailment(ctx context.Context, device interfaces.CurtailmentPreviewDevice) bool {
	if s.capabilitiesProvider == nil {
		return false
	}
	caps := s.capabilitiesProvider.GetMinerCapabilitiesForDevice(ctx, &pairingpb.Device{
		DeviceIdentifier: device.DeviceIdentifier,
		Manufacturer:     device.Manufacturer,
		Model:            device.Model,
		FirmwareVersion:  device.FirmwareVersion,
		DriverName:       device.DriverName,
	})
	return caps.GetCommands().GetCurtailFullSupported()
}

func sortCandidates(candidates []modes.Candidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		leftHasEfficiency := left.EfficiencyJH != nil
		rightHasEfficiency := right.EfficiencyJH != nil
		if leftHasEfficiency != rightHasEfficiency {
			return leftHasEfficiency
		}
		if leftHasEfficiency && *left.EfficiencyJH != *right.EfficiencyJH {
			return *left.EfficiencyJH > *right.EfficiencyJH
		}
		return left.DeviceIdentifier < right.DeviceIdentifier
	})
}

func (p plan) toResponse(req normalizedPreviewRequest) *pb.PreviewCurtailmentPlanResponse {
	candidates := make([]*pb.CurtailmentCandidate, 0, len(p.selected))
	for _, candidate := range p.selected {
		efficiencyJH := 0.0
		if candidate.EfficiencyJH != nil {
			efficiencyJH = *candidate.EfficiencyJH
		}
		candidates = append(candidates, &pb.CurtailmentCandidate{
			DeviceIdentifier: candidate.DeviceIdentifier,
			CurrentPowerW:    candidate.CurrentPowerW,
			EfficiencyJh:     efficiencyJH,
			ReasonSelected:   candidate.ReasonSelected,
		})
	}

	resp := &pb.PreviewCurtailmentPlanResponse{
		Candidates:                candidates,
		EstimatedReductionKw:      p.estimatedReduction,
		EstimatedRemainingPowerKw: p.remainingPowerKW,
		Mode:                      req.mode,
		SkippedCandidates:         p.skipped,
	}
	if req.mode == pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW {
		fixedKW := req.original.GetFixedKw()
		toleranceKW := fixedKW.GetToleranceKw()
		resp.ModeParams = &pb.PreviewCurtailmentPlanResponse_FixedKw{
			FixedKw: &pb.FixedKwParams{
				TargetKw:    fixedKW.GetTargetKw(),
				ToleranceKw: &toleranceKW,
			},
		}
	}
	return resp
}
