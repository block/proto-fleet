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
	"github.com/block/proto-fleet/server/internal/domain/session"
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

// toStartRequest converts the proto StartCurtailmentRequest to a service
// StartRequest, deriving source_actor_type from the authenticated session.
func toStartRequest(msg *pb.StartCurtailmentRequest, info *session.Info) (curtailment.StartRequest, error) {
	scope, err := toStartScope(msg)
	if err != nil {
		return curtailment.StartRequest{}, err
	}

	if msg.GetMode() != pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW &&
		msg.GetMode() != pb.CurtailmentMode_CURTAILMENT_MODE_UNSPECIFIED {
		return curtailment.StartRequest{}, fleeterror.NewInvalidArgumentErrorf(
			"mode %s is not supported; only FIXED_KW",
			msg.GetMode().String(),
		)
	}
	fixedKw := msg.GetFixedKw()
	if fixedKw == nil {
		return curtailment.StartRequest{}, fleeterror.NewInvalidArgumentError(
			"fixed_kw mode params required for FIXED_KW start",
		)
	}
	tolerance := 0.0
	if fixedKw.ToleranceKw != nil {
		tolerance = *fixedKw.ToleranceKw
	}

	preview := curtailment.PreviewRequest{
		OrgID:                   info.OrganizationID,
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
		// Proto validator caps below MaxInt32; defense-in-depth so non-Connect
		// callers can't bypass the bound.
		if *override > math.MaxInt32 {
			return curtailment.StartRequest{}, fleeterror.NewInvalidArgumentErrorf(
				"candidate_min_power_w_override exceeds int32 max: %d", *override,
			)
		}
		v := int32(*override) // #nosec G115 -- bounds-checked above
		preview.CandidateMinPowerWOverride = &v
	}

	out := curtailment.StartRequest{
		PreviewRequest:          preview,
		Reason:                  msg.GetReason(),
		RestoreBatchSize:        uint32ToInt32Saturating(msg.GetRestoreBatchSize()),
		RestoreBatchIntervalSec: uint32ToInt32Saturating(msg.GetRestoreBatchIntervalSec()),
		MinCurtailedDurationSec: uint32ToInt32Saturating(msg.GetMinCurtailedDurationSec()),
		AllowUnbounded:          msg.GetAllowUnbounded(),
		IdempotencyKey:          nonEmptyPtr(msg.GetIdempotencyKey()),
		ExternalSource:          nonEmptyPtr(msg.GetExternalSource()),
		ExternalReference:       nonEmptyPtr(msg.GetExternalReference()),
		SourceActorType:         deriveSourceActorType(info),
		SourceActorID:           deriveSourceActorID(info),
	}

	if !out.AllowUnbounded {
		// max_duration_seconds=0 is the proto sentinel for "use the org's
		// configured default"; leave MaxDurationSeconds nil and let
		// Service.Start normalize against curtailment_org_config. A non-zero
		// value is forwarded as-is for the validator's >0 bound to enforce.
		if raw := msg.GetMaxDurationSeconds(); raw > 0 {
			v := uint32ToInt32Saturating(raw)
			out.MaxDurationSeconds = &v
		}
	}

	return out, nil
}

// toStartScope mirrors toScope (Preview) for the StartCurtailmentRequest
// oneof. The two oneofs are structurally identical but typed separately by
// protoc-gen-go, so we can't share the switch.
func toStartScope(msg *pb.StartCurtailmentRequest) (curtailment.Scope, error) {
	switch s := msg.GetScope().(type) {
	case *pb.StartCurtailmentRequest_WholeOrg:
		return curtailment.Scope{Type: models.ScopeTypeWholeOrg}, nil
	case *pb.StartCurtailmentRequest_DeviceSetIds:
		return curtailment.Scope{
			Type:         models.ScopeTypeDeviceSets,
			DeviceSetIDs: s.DeviceSetIds.GetDeviceSetIds(),
		}, nil
	case *pb.StartCurtailmentRequest_DeviceIdentifiers:
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

// toStartResponse maps the service Plan + request into the
// StartCurtailmentResponse, populating the CurtailmentEvent shape with the
// values known at Start time. Per-state target rollups and started/ended
// timestamps come from later reconciler ticks; left zero/nil here.
func toStartResponse(plan *curtailment.Plan, req *pb.StartCurtailmentRequest) *pb.StartCurtailmentResponse {
	event := &pb.CurtailmentEvent{
		State:                   pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_PENDING,
		Mode:                    pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW,
		Strategy:                pb.CurtailmentStrategy_CURTAILMENT_STRATEGY_LEAST_EFFICIENT_FIRST,
		Level:                   pb.CurtailmentLevel_CURTAILMENT_LEVEL_FULL,
		Priority:                resolvePriority(req.GetPriority()),
		MaxDurationSeconds:      req.GetMaxDurationSeconds(),
		RestoreBatchSize:        req.GetRestoreBatchSize(),
		RestoreBatchIntervalSec: req.GetRestoreBatchIntervalSec(),
		MinCurtailedDurationSec: req.GetMinCurtailedDurationSec(),
		IncludeMaintenance:      req.GetIncludeMaintenance(),
		ForceIncludeMaintenance: req.GetForceIncludeMaintenance(),
		Reason:                  req.GetReason(),
		ExternalSource:          req.GetExternalSource(),
		ExternalReference:       req.GetExternalReference(),
		IdempotencyKey:          req.GetIdempotencyKey(),
	}
	if plan.EventUUID != nil {
		event.EventUuid = plan.EventUUID.String()
	}
	// Echo the original scope and mode params so callers don't need to
	// re-fetch the request.
	switch s := req.GetScope().(type) {
	case *pb.StartCurtailmentRequest_WholeOrg:
		event.Scope = &pb.CurtailmentEvent_WholeOrg{WholeOrg: s.WholeOrg}
	case *pb.StartCurtailmentRequest_DeviceSetIds:
		event.Scope = &pb.CurtailmentEvent_DeviceSetIds{DeviceSetIds: s.DeviceSetIds}
	case *pb.StartCurtailmentRequest_DeviceIdentifiers:
		event.Scope = &pb.CurtailmentEvent_DeviceIdentifiers{DeviceIdentifiers: s.DeviceIdentifiers}
	}
	if fk := req.GetFixedKw(); fk != nil {
		event.ModeParams = &pb.CurtailmentEvent_FixedKw{FixedKw: fk}
	}

	// Populate target rows from the persisted plan so callers see the
	// pending target set without an extra round trip.
	targets := make([]*pb.CurtailmentTarget, len(plan.Selected))
	for i, sel := range plan.Selected {
		t := &pb.CurtailmentTarget{
			DeviceIdentifier: sel.DeviceIdentifier,
			TargetType:       "miner",
			State:            pb.CurtailmentTargetState_CURTAILMENT_TARGET_STATE_PENDING,
			DesiredState:     pb.CurtailmentTargetDesiredState_CURTAILMENT_TARGET_DESIRED_STATE_CURTAILED,
		}
		if sel.PowerW > 0 {
			v := sel.PowerW
			t.BaselinePowerW = &v
		}
		targets[i] = t
	}
	event.Targets = targets
	rollup := lenToInt32Saturating(len(targets))
	event.TargetRollup = &pb.CurtailmentTargetRollup{
		Pending: rollup,
		Total:   rollup,
	}

	return &pb.StartCurtailmentResponse{Event: event}
}

// lenToInt32Saturating clamps a slice length to int32 max for proto rollup
// fields. Selector-produced target lists are bounded by candidate counts well
// below MaxInt32; this is the static-analysis-friendly cast.
func lenToInt32Saturating(n int) int32 {
	if n > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(n) // #nosec G115 -- bounds-checked above
}

// resolvePriority normalizes UNSPECIFIED to NORMAL for response echoing;
// other explicit values pass through.
func resolvePriority(p pb.CurtailmentPriority) pb.CurtailmentPriority {
	switch p {
	case pb.CurtailmentPriority_CURTAILMENT_PRIORITY_UNSPECIFIED:
		return pb.CurtailmentPriority_CURTAILMENT_PRIORITY_NORMAL
	case pb.CurtailmentPriority_CURTAILMENT_PRIORITY_NORMAL,
		pb.CurtailmentPriority_CURTAILMENT_PRIORITY_HIGH,
		pb.CurtailmentPriority_CURTAILMENT_PRIORITY_EMERGENCY:
		return p
	default:
		return p
	}
}

// uint32ToInt32Saturating clamps a proto-uint32 to int32 max so the service
// layer doesn't see an overflowed negative value. Proto validators cap
// reachable inputs well below MaxInt32; this is the non-Connect-caller
// backstop.
func uint32ToInt32Saturating(v uint32) int32 {
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(v) // #nosec G115 -- bounds-checked above
}

// nonEmptyPtr returns nil for an empty proto3 string, &s otherwise. Used so
// optional attribution fields land as SQL NULL rather than empty string and
// satisfy the migration's `length > 0` CHECK constraints.
func nonEmptyPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// deriveSourceActorType maps session.Info into the curtailment audit-actor
// vocabulary. Scheduler-synthesized sessions take priority over the auth
// method; otherwise session/api-key calls fan out to user / api_key
// respectively. Webhook callers route through ActorScheduler-equivalents in
// the schedule processor; direct webhook attribution lives in
// external_source / external_reference until a webhook auth surface lands.
func deriveSourceActorType(info *session.Info) models.SourceActorType {
	if info == nil {
		return models.SourceActorUser
	}
	if info.Actor == session.ActorScheduler {
		return models.SourceActorScheduler
	}
	if info.AuthMethod == session.AuthMethodAPIKey {
		return models.SourceActorAPIKey
	}
	return models.SourceActorUser
}

// deriveSourceActorID returns the credential identifier that pairs with the
// SourceActorType for audit attribution. Empty session.Info or scheduler
// sessions leave the column NULL — the scheduler is identified by its actor
// type alone.
func deriveSourceActorID(info *session.Info) *string {
	if info == nil || info.Actor == session.ActorScheduler {
		return nil
	}
	id := info.CredentialID()
	if id == "" {
		return nil
	}
	return &id
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
