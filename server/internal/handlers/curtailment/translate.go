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

// toStartRequest converts the proto request to a service StartRequest,
// deriving source_actor_type and CreatedByUserID from session.Info.
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
		// Proto validator already bounds this; backstop for non-Connect callers.
		if *override > math.MaxInt32 {
			return curtailment.StartRequest{}, fleeterror.NewInvalidArgumentErrorf(
				"candidate_min_power_w_override exceeds int32 max: %d", *override,
			)
		}
		v := int32(*override) // #nosec G115 -- bounds-checked above
		preview.CandidateMinPowerWOverride = &v
	}

	restoreBatchSize, err := uint32ToInt32Strict("restore_batch_size", msg.GetRestoreBatchSize())
	if err != nil {
		return curtailment.StartRequest{}, err
	}
	restoreBatchIntervalSec, err := uint32ToInt32Strict("restore_batch_interval_sec", msg.GetRestoreBatchIntervalSec())
	if err != nil {
		return curtailment.StartRequest{}, err
	}
	minCurtailedDurationSec, err := uint32ToInt32Strict("min_curtailed_duration_sec", msg.GetMinCurtailedDurationSec())
	if err != nil {
		return curtailment.StartRequest{}, err
	}

	out := curtailment.StartRequest{
		PreviewRequest:          preview,
		Reason:                  msg.GetReason(),
		RestoreBatchSize:        restoreBatchSize,
		RestoreBatchIntervalSec: restoreBatchIntervalSec,
		MinCurtailedDurationSec: minCurtailedDurationSec,
		AllowUnbounded:          msg.GetAllowUnbounded(),
		IdempotencyKey:          nonEmptyPtr(msg.GetIdempotencyKey()),
		ExternalSource:          nonEmptyPtr(msg.GetExternalSource()),
		ExternalReference:       nonEmptyPtr(msg.GetExternalReference()),
		SourceActorType:         deriveSourceActorType(info),
		SourceActorID:           deriveSourceActorID(info),
		CreatedByUserID:         info.UserID,
	}

	// max_duration_seconds=0 is the "use org default" sentinel when
	// allow_unbounded=false, and "no cap" when allow_unbounded=true. Both
	// leave MaxDurationSeconds nil. Non-zero values are parsed regardless
	// of allow_unbounded so the service-level mutual-exclusion check can
	// fire (a non-zero cap with allow_unbounded=true is a behavioral
	// mismatch the operator must see, not a silent drop).
	if raw := msg.GetMaxDurationSeconds(); raw > 0 {
		v, err := uint32ToInt32Strict("max_duration_seconds", raw)
		if err != nil {
			return curtailment.StartRequest{}, err
		}
		out.MaxDurationSeconds = &v
	}

	return out, nil
}

// toStartScope mirrors toScope. The two oneofs are structurally identical
// but typed separately by protoc-gen-go, so the switches can't merge.
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
// StartCurtailmentResponse. Describes the request that was just persisted;
// idempotent-retry lookup is a follow-up. Duplicate keys surface as Internal
// until then.
func toStartResponse(plan *curtailment.Plan, req *pb.StartCurtailmentRequest) *pb.StartCurtailmentResponse {
	event := &pb.CurtailmentEvent{
		State:                   pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_PENDING,
		Mode:                    pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW,
		Strategy:                pb.CurtailmentStrategy_CURTAILMENT_STRATEGY_LEAST_EFFICIENT_FIRST,
		Level:                   pb.CurtailmentLevel_CURTAILMENT_LEVEL_FULL,
		Priority:                resolvePriority(req.GetPriority()),
		MaxDurationSeconds:      effectiveMaxDurationSeconds(plan, req),
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

	// All targets are PENDING at Start; reconciler updates them in-place.
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
// fields. Selector lists are well below MaxInt32; this is for static-analysis.
func lenToInt32Saturating(n int) int32 {
	if n > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(n) // #nosec G115 -- bounds-checked above
}

// effectiveMaxDurationSeconds prefers the persisted value (Service.Start
// resolves "use org default") so the response reflects the cap, not the
// raw request zero. Falls back to the request when Plan has no resolved
// value (Preview path); nil plan field — allow_unbounded — renders as 0.
func effectiveMaxDurationSeconds(plan *curtailment.Plan, req *pb.StartCurtailmentRequest) uint32 {
	if plan == nil || plan.EffectiveMaxDurationSeconds == nil {
		return req.GetMaxDurationSeconds()
	}
	v := *plan.EffectiveMaxDurationSeconds
	if v < 0 {
		return 0
	}
	return uint32(v) // #nosec G115 -- bounds-checked above
}

// resolvePriority normalizes UNSPECIFIED to NORMAL for response echoing;
// other explicit values pass through.
func resolvePriority(p pb.CurtailmentPriority) pb.CurtailmentPriority {
	if p == pb.CurtailmentPriority_CURTAILMENT_PRIORITY_UNSPECIFIED {
		return pb.CurtailmentPriority_CURTAILMENT_PRIORITY_NORMAL
	}
	return p
}

// uint32ToInt32Strict converts proto uint32 → int32, rejecting overflow with
// InvalidArgument. Silent saturation would mangle valid proto inputs above
// MaxInt32; surface a clear error instead.
func uint32ToInt32Strict(field string, v uint32) (int32, error) {
	if v > math.MaxInt32 {
		return 0, fleeterror.NewInvalidArgumentErrorf(
			"%s exceeds int32 max: %d", field, v,
		)
	}
	return int32(v), nil // #nosec G115 -- bounds-checked above
}

// nonEmptyPtr returns nil for "", &s otherwise. Maps proto3 empty-string
// to SQL NULL so the migration's `length > 0` CHECK constraints hold.
func nonEmptyPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// deriveSourceActorType maps session.Info into the audit-actor vocabulary.
// Scheduler-synthesized sessions win over auth method; otherwise
// session/api-key route to user / api_key. Webhook attribution lives in
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

// deriveSourceActorID pairs with SourceActorType for audit attribution.
// nil session or scheduler → NULL (scheduler is identified by actor type).
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
	// Echo FIXED_KW params so the UI can render the undershoot delta.
	if fk := req.GetFixedKw(); fk != nil {
		resp.ModeParams = &pb.PreviewCurtailmentPlanResponse_FixedKw{FixedKw: fk}
	}
	return resp
}

func strategyName(s pb.CurtailmentStrategy) models.Strategy {
	if s == pb.CurtailmentStrategy_CURTAILMENT_STRATEGY_UNSPECIFIED {
		return models.StrategyLeastEfficientFirst
	}
	// Pass other values through; service validator rejects them by name.
	return models.Strategy(s.String())
}

// strategyReasonLabel renders reason_selected. UNSPECIFIED and
// LEAST_EFFICIENT_FIRST render as the canonical lowercase form; other values
// pass through as their proto names (rejected upstream by the validator).
func strategyReasonLabel(s pb.CurtailmentStrategy) string {
	if s == pb.CurtailmentStrategy_CURTAILMENT_STRATEGY_UNSPECIFIED ||
		s == pb.CurtailmentStrategy_CURTAILMENT_STRATEGY_LEAST_EFFICIENT_FIRST {
		return "least_efficient_first"
	}
	return s.String()
}

func levelName(l pb.CurtailmentLevel) models.Level {
	// UNSPECIFIED defaults to FULL; other values pass through their proto
	// names so the service validator can reject them.
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
		// Pass through; service validator rejects it.
		return models.PriorityHigh
	default:
		// New enum values surface as a validator rejection, not silent NORMAL.
		return models.Priority(p.String())
	}
}

// toInsufficientLoadError returns InvalidArgument with kW numbers and
// non-zero exclusion counters. Counter order is source-fixed for byte-stable
// output until Connect error-detail propagation lands.
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

// formatExclusionCounters renders non-zero ExcludedX fields. Source-fixed
// order keeps output byte-stable; names use the canonical SkipReason
// vocabulary so success and failure paths share one token set.
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
		// Transient-status / data-quality skips, grouped between maintenance
		// and pairing in the byte-stable order.
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
