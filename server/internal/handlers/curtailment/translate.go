package curtailment

import (
	"math"

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
		// Defense-in-depth bounds check: the proto validator caps the
		// override well below int32's max, but if interceptor wiring is
		// ever bypassed, reject loudly rather than wrap silently.
		if *override > math.MaxInt32 {
			return curtailment.PreviewRequest{}, fleeterror.NewInvalidArgumentErrorf(
				"candidate_min_power_w_override exceeds int32 max: %d", *override,
			)
		}
		v := int32(*override) // #nosec G115 -- bounds-checked above
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
	// Derive the reason_selected label from the request's strategy so a
	// future strategy enum addition forces this surface to be touched.
	// Today only LEAST_EFFICIENT_FIRST exists; the helper resolves the
	// UNSPECIFIED → LEAST_EFFICIENT_FIRST default identical to service.go.
	reasonSelected := strategyReasonLabel(req.GetStrategy())
	candidates := make([]*pb.CurtailmentCandidate, len(plan.Selected))
	for i, c := range plan.Selected {
		candidates[i] = &pb.CurtailmentCandidate{
			DeviceIdentifier: c.DeviceIdentifier,
			CurrentPowerW:    c.PowerW,
			EfficiencyJh:     c.EfficiencyJH,
			ReasonSelected:   reasonSelected,
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

// strategyReasonLabel maps the request strategy to the per-candidate
// reason_selected label echoed back to the UI. Adding a new strategy enum
// requires a new case here so the response surface is forced to update in
// lockstep with the selector's ranking implementation. v1 only implements
// LEAST_EFFICIENT_FIRST; other strategies fall through to their proto name
// because the service layer already rejects them as unsupported.
func strategyReasonLabel(s pb.CurtailmentStrategy) string {
	switch s {
	case pb.CurtailmentStrategy_CURTAILMENT_STRATEGY_UNSPECIFIED,
		pb.CurtailmentStrategy_CURTAILMENT_STRATEGY_LEAST_EFFICIENT_FIRST:
		return "least_efficient_first"
	case pb.CurtailmentStrategy_CURTAILMENT_STRATEGY_MOST_POWER_FIRST,
		pb.CurtailmentStrategy_CURTAILMENT_STRATEGY_OLDEST_HARDWARE_FIRST,
		pb.CurtailmentStrategy_CURTAILMENT_STRATEGY_UNSTABLE_MINERS_FIRST,
		pb.CurtailmentStrategy_CURTAILMENT_STRATEGY_RACK_GRANULAR:
		return s.String()
	default:
		return s.String()
	}
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
	case pb.CurtailmentPriority_CURTAILMENT_PRIORITY_UNSPECIFIED,
		pb.CurtailmentPriority_CURTAILMENT_PRIORITY_NORMAL,
		pb.CurtailmentPriority_CURTAILMENT_PRIORITY_HIGH:
		// HIGH is reserved-but-undesigned in v1; the proto validator
		// rejects it before this function runs. UNSPECIFIED and NORMAL
		// both map to NORMAL since the service treats absent priority as
		// the default.
		return "NORMAL"
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
