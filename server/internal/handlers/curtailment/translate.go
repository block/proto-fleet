package curtailment

import (
	"fmt"
	"math"
	"strings"

	pb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	"github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/modes"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// toPreviewRequest converts the proto request to a service PreviewRequest.
func toPreviewRequest(msg *pb.PreviewCurtailmentPlanRequest, orgID int64) (curtailment.PreviewRequest, error) {
	scope, err := toScope(msg)
	if err != nil {
		return curtailment.PreviewRequest{}, err
	}

	if msg.GetMode() != pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW &&
		msg.GetMode() != pb.CurtailmentMode_CURTAILMENT_MODE_UNSPECIFIED {
		return curtailment.PreviewRequest{}, fleeterror.NewInvalidArgumentErrorf(
			"mode %s is not supported; only FIXED_KW",
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
		Mode:                    models.ModeFixedKw,
		Strategy:                strategyName(msg.GetStrategy()),
		Level:                   levelName(msg.GetLevel()),
		Priority:                priorityName(msg.GetPriority()),
		TargetKW:                fixedKw.GetTargetKw(),
		ToleranceKW:             tolerance,
		IncludeMaintenance:      msg.GetIncludeMaintenance(),
		ForceIncludeMaintenance: msg.GetForceIncludeMaintenance(),
	}
	if override := msg.CandidateMinPowerWOverride; override != nil {
		// Defense-in-depth: proto validator already caps below MaxInt32,
		// but reject loudly if interceptor wiring is ever bypassed.
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

func toScope(msg *pb.PreviewCurtailmentPlanRequest) (curtailment.Scope, error) {
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

// toPreviewResponse maps the service Plan to the proto response.
func toPreviewResponse(plan *curtailment.Plan, req *pb.PreviewCurtailmentPlanRequest) *pb.PreviewCurtailmentPlanResponse {
	// strategyReasonLabel forces a future strategy enum addition to touch
	// this surface (compile-time exhaustive switch).
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
	// Echo FIXED_KW params so the UI can render the undershoot delta
	// without re-fetching the request.
	if fk := req.GetFixedKw(); fk != nil {
		resp.ModeParams = &pb.PreviewCurtailmentPlanResponse_FixedKw{FixedKw: fk}
	}
	return resp
}

func strategyName(s pb.CurtailmentStrategy) models.Strategy {
	if s == pb.CurtailmentStrategy_CURTAILMENT_STRATEGY_UNSPECIFIED {
		return models.StrategyLeastEfficientFirst
	}
	// Other proto values pass through verbatim so the service validator
	// can reject them with a clear message naming the offending value.
	return models.Strategy(s.String())
}

// strategyReasonLabel renders reason_selected for the response. Exhaustive
// switch forces a future strategy enum addition to update this surface in
// lockstep with the selector's ranking implementation.
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

func levelName(l pb.CurtailmentLevel) models.Level {
	// Service matches on LevelFull directly; UNSPECIFIED defaults to FULL,
	// other values pass through their proto names so the service rejects them.
	if l == pb.CurtailmentLevel_CURTAILMENT_LEVEL_UNSPECIFIED ||
		l == pb.CurtailmentLevel_CURTAILMENT_LEVEL_FULL {
		return models.LevelFull
	}
	return models.Level(l.String())
}

func priorityName(p pb.CurtailmentPriority) models.Priority {
	switch p {
	case pb.CurtailmentPriority_CURTAILMENT_PRIORITY_EMERGENCY:
		return models.PriorityEmergency
	case pb.CurtailmentPriority_CURTAILMENT_PRIORITY_UNSPECIFIED,
		pb.CurtailmentPriority_CURTAILMENT_PRIORITY_NORMAL:
		return models.PriorityNormal
	case pb.CurtailmentPriority_CURTAILMENT_PRIORITY_HIGH:
		// Pass through so the service validator can reject it.
		return models.PriorityHigh
	default:
		// Future enum addition surfaces as a clear validator rejection
		// rather than silent NORMAL coercion.
		return models.Priority(p.String())
	}
}

// toInsufficientLoadError returns InvalidArgument with the kW numbers
// and every non-zero exclusion counter (zero counters omitted; counter
// order fixed at source for byte-stable output until Connect error-detail
// propagation lands).
func toInsufficientLoadError(detail *modes.InsufficientLoadDetail) error {
	if detail == nil {
		return fleeterror.NewInvalidArgumentError("insufficient curtailable load")
	}
	exclusions := formatExclusionCounters(detail)
	header := fmt.Sprintf(
		"insufficient curtailable load: %.3f kW available, %.3f kW requested, tolerance %.3f kW, candidate_min_power_w=%dW",
		detail.AvailableKW, detail.RequestedKW, detail.ToleranceKW, detail.CandidateMinPowerW,
	)
	if exclusions == "" {
		return fleeterror.NewInvalidArgumentError(header)
	}
	return fleeterror.NewInvalidArgumentErrorf("%s; excluded: %s", header, exclusions)
}

// formatExclusionCounters renders non-zero ExcludedX fields. Order is
// source-fixed (not map-derived) so output is byte-stable. Names use the
// canonical SkipReason vocabulary so the success-path SkippedCandidate.reason
// and the failure-path counters share one set of tokens.
func formatExclusionCounters(d *modes.InsufficientLoadDetail) string {
	type counter struct {
		name string
		val  int32
	}
	all := []counter{
		{string(curtailment.SkipBelowThreshold), d.ExcludedBelowThreshold},
		{string(curtailment.SkipPhantomLoadNoHash), d.ExcludedPhantomLoad},
		{string(curtailment.SkipPowerTelemetryUnreliable), d.ExcludedDeadMonitor},
		{string(curtailment.SkipUnreachableResidualLoad), d.ExcludedOffline},
		{string(curtailment.SkipMaintenance), d.ExcludedMaintenance},
		// Transient-status / data-quality skips. Inserted after maintenance
		// (preserves the byte-stable test's below→offline→maintenance order)
		// and before pairing so the message groups status-driven exclusions
		// together.
		{string(curtailment.SkipUpdating), d.ExcludedUpdating},
		{string(curtailment.SkipRebootRequired), d.ExcludedRebootRequired},
		{string(curtailment.SkipStaleTelemetry), d.ExcludedStale},
		{string(curtailment.SkipNonActionableStatus), d.ExcludedNonActionable},
		{string(curtailment.SkipPairing), d.ExcludedPairing},
		{string(curtailment.SkipCooldown), d.ExcludedCooldown},
		{string(curtailment.SkipActiveEvent), d.ExcludedActiveEvent},
		{string(curtailment.SkipCurtailFullUnsupported), d.ExcludedCapabilityMiss},
	}
	var parts []string
	for _, c := range all {
		if c.val > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", c.name, c.val))
		}
	}
	return strings.Join(parts, ", ")
}
