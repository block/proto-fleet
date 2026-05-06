package curtailment

import (
	pb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	"github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/modes"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// translatePreviewRequest converts the proto request into the service-level
// PreviewRequest. Decoupling lets the service be testable without proto
// dependencies; the translation is the only place proto types appear in the
// curtailment-handler call path.
func translatePreviewRequest(msg *pb.PreviewCurtailmentPlanRequest, orgID int64) (curtailment.PreviewRequest, error) {
	scope, err := translateScope(msg)
	if err != nil {
		return curtailment.PreviewRequest{}, err
	}

	if msg.GetMode() != pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW &&
		msg.GetMode() != pb.CurtailmentMode_CURTAILMENT_MODE_UNSPECIFIED {
		return curtailment.PreviewRequest{}, fleeterror.NewInvalidArgumentErrorf(
			"mode %s is not supported in v1; only FIXED_KW",
			msg.GetMode().String(),
		)
	}
	fixedKw := msg.GetFixedKw()
	if fixedKw == nil {
		return curtailment.PreviewRequest{}, fleeterror.NewInvalidArgumentError(
			"fixed_kw mode params required for FIXED_KW preview",
		)
	}
	tolerance := 0.0
	if fixedKw.ToleranceKw != nil {
		tolerance = *fixedKw.ToleranceKw
	}

	out := curtailment.PreviewRequest{
		OrgID:                   orgID,
		Scope:                   scope,
		Mode:                    "FIXED_KW",
		Strategy:                strategyName(msg.GetStrategy()),
		Level:                   levelName(msg.GetLevel()),
		Priority:                priorityName(msg.GetPriority()),
		TargetKW:                fixedKw.GetTargetKw(),
		ToleranceKW:             tolerance,
		IncludeMaintenance:      msg.GetIncludeMaintenance(),
		ForceIncludeMaintenance: msg.GetForceIncludeMaintenance(),
	}
	if override := msg.CandidateMinPowerWOverride; override != nil {
		v := int32(*override)
		out.CandidateMinPowerWOverride = &v
	}
	return out, nil
}

func translateScope(msg *pb.PreviewCurtailmentPlanRequest) (curtailment.Scope, error) {
	switch s := msg.GetScope().(type) {
	case *pb.PreviewCurtailmentPlanRequest_WholeOrg:
		return curtailment.Scope{Type: models.ScopeTypeWholeOrg}, nil
	case *pb.PreviewCurtailmentPlanRequest_DeviceSetIds:
		return curtailment.Scope{
			Type:         models.ScopeTypeDeviceSets,
			DeviceSetIDs: s.DeviceSetIds.GetDeviceSetIds(),
		}, nil
	case *pb.PreviewCurtailmentPlanRequest_DeviceIdentifiers:
		return curtailment.Scope{
			Type:              models.ScopeTypeDeviceList,
			DeviceIdentifiers: s.DeviceIdentifiers.GetDeviceIdentifiers(),
		}, nil
	default:
		return curtailment.Scope{}, fleeterror.NewInvalidArgumentError(
			"scope is required: set whole_org, device_set_ids, or device_identifiers",
		)
	}
}

// translatePreviewResponse maps the service-level Plan to the proto response.
// Selected candidates carry their telemetry snapshot so the UI can render
// per-device stats without a re-query; skipped candidates carry their
// canonical reason from the SkipReason vocabulary.
func translatePreviewResponse(plan *curtailment.Plan, req *pb.PreviewCurtailmentPlanRequest) *pb.PreviewCurtailmentPlanResponse {
	candidates := make([]*pb.CurtailmentCandidate, len(plan.Selected))
	for i, c := range plan.Selected {
		candidates[i] = &pb.CurtailmentCandidate{
			DeviceIdentifier: c.DeviceIdentifier,
			CurrentPowerW:    c.PowerW,
			EfficiencyJh:     c.EfficiencyJH,
			ReasonSelected:   "least_efficient_first",
		}
	}
	skipped := make([]*pb.SkippedCandidate, len(plan.Skipped))
	for i, s := range plan.Skipped {
		skipped[i] = &pb.SkippedCandidate{
			DeviceIdentifier: s.DeviceIdentifier,
			Reason:           string(s.Reason),
		}
	}
	resp := &pb.PreviewCurtailmentPlanResponse{
		Candidates:                candidates,
		EstimatedReductionKw:      plan.EstimatedReductionKW,
		EstimatedRemainingPowerKw: plan.EstimatedRemainingPowerKW,
		Mode:                      pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW,
		SkippedCandidates:         skipped,
	}
	// Echo back the FIXED_KW params so the UI can render the undershoot
	// delta (target_kw - estimated_reduction_kw, clamped to 0) without
	// re-fetching the request.
	if fk := req.GetFixedKw(); fk != nil {
		resp.ModeParams = &pb.PreviewCurtailmentPlanResponse_FixedKw{FixedKw: fk}
	}
	return resp
}

func strategyName(s pb.CurtailmentStrategy) string {
	if s == pb.CurtailmentStrategy_CURTAILMENT_STRATEGY_UNSPECIFIED {
		return "LEAST_EFFICIENT_FIRST"
	}
	return s.String()
}

func levelName(l pb.CurtailmentLevel) string {
	if l == pb.CurtailmentLevel_CURTAILMENT_LEVEL_UNSPECIFIED {
		return "FULL"
	}
	// Generated enum names are CURTAILMENT_LEVEL_FULL etc.; the service
	// matches on "FULL" directly so map the v1 case explicitly and pass
	// the raw name otherwise (the service rejects unsupported values).
	if l == pb.CurtailmentLevel_CURTAILMENT_LEVEL_FULL {
		return "FULL"
	}
	return l.String()
}

func priorityName(p pb.CurtailmentPriority) string {
	switch p {
	case pb.CurtailmentPriority_CURTAILMENT_PRIORITY_EMERGENCY:
		return "EMERGENCY"
	default:
		return "NORMAL"
	}
}

// translateInsufficientLoad maps the OutcomeInsufficientLoad branch to a
// fleeterror InvalidArgument with a structured detail message. Connect-RPC
// error-detail propagation is a future enhancement; v1 returns the key
// numbers in the message body so the UI can render them directly.
func translateInsufficientLoad(detail *modes.InsufficientLoadDetail) error {
	if detail == nil {
		return fleeterror.NewInvalidArgumentError("insufficient curtailable load")
	}
	return fleeterror.NewInvalidArgumentErrorf(
		"insufficient curtailable load: %.3f kW available, %.3f kW requested, tolerance %.3f kW; %d offline, %d maintenance, %d cooldown, %d active-event, %d unpaired",
		detail.AvailableKW, detail.RequestedKW, detail.ToleranceKW,
		detail.ExcludedOffline, detail.ExcludedMaintenance, detail.ExcludedCooldown,
		detail.ExcludedActiveEvent, detail.ExcludedPairing,
	)
}
