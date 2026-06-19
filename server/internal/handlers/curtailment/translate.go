package curtailment

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	"github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/modes"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

// toRequestMode validates the proto mode and returns the domain mode plus the
// FIXED_KW params (nil for FULL_FLEET). FIXED_KW (and the unspecified default)
// require fixed_kw params; FULL_FLEET takes none.
func toRequestMode(m pb.CurtailmentMode, fixedKw *pb.FixedKwParams, hasModeParams bool) (models.Mode, *pb.FixedKwParams, error) {
	if m == pb.CurtailmentMode_CURTAILMENT_MODE_FULL_FLEET {
		// FULL_FLEET takes no params; reject any set mode_params oneof rather
		// than silently dropping a reserved fixed_count / site_power_cap.
		if hasModeParams {
			return "", nil, fleeterror.NewInvalidArgumentError(
				"FULL_FLEET takes no mode params")
		}
		return models.ModeFullFleet, nil, nil
	}
	if m != pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW &&
		m != pb.CurtailmentMode_CURTAILMENT_MODE_UNSPECIFIED {
		return "", nil, fleeterror.NewInvalidArgumentErrorf(
			"mode %s is not supported; only FIXED_KW and FULL_FLEET", m.String())
	}
	// FIXED_KW (and the unspecified default) require fixed_kw params.
	if fixedKw == nil {
		return "", nil, fleeterror.NewInvalidArgumentError("fixed_kw mode params required for FIXED_KW")
	}
	return models.ModeFixedKw, fixedKw, nil
}

// requestModeProto normalizes a request's mode for echoing in responses: the
// unspecified default is FIXED_KW.
func requestModeProto(m pb.CurtailmentMode) pb.CurtailmentMode {
	if m == pb.CurtailmentMode_CURTAILMENT_MODE_UNSPECIFIED {
		return pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW
	}
	return m
}

// toPreviewRequest converts the proto request to a service PreviewRequest.
func toPreviewRequest(msg *pb.PreviewCurtailmentPlanRequest, orgID int64) (curtailment.PreviewRequest, error) {
	scope, err := toScope(msg)
	if err != nil {
		return curtailment.PreviewRequest{}, err
	}

	mode, fixedKw, err := toRequestMode(msg.GetMode(), msg.GetFixedKw(), msg.GetModeParams() != nil)
	if err != nil {
		return curtailment.PreviewRequest{}, err
	}
	tolerance := 0.0
	if fixedKw != nil && fixedKw.ToleranceKw != nil {
		tolerance = *fixedKw.ToleranceKw
	}

	out := curtailment.PreviewRequest{
		OrgID:                   orgID,
		Scope:                   scope,
		Mode:                    mode,
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
	case *pb.PreviewCurtailmentPlanRequest_Site:
		return curtailment.Scope{
			Type:   models.ScopeTypeSite,
			SiteID: s.Site.GetSiteId(),
		}, nil
	default:
		return curtailment.Scope{}, fleeterror.NewInvalidArgumentError(
			"scope is required: set whole_org, site, device_set_ids, or device_identifiers",
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

	mode, fixedKw, err := toRequestMode(msg.GetMode(), msg.GetFixedKw(), msg.GetModeParams() != nil)
	if err != nil {
		return curtailment.StartRequest{}, err
	}
	tolerance := 0.0
	if fixedKw != nil && fixedKw.ToleranceKw != nil {
		tolerance = *fixedKw.ToleranceKw
	}

	preview := curtailment.PreviewRequest{
		OrgID:                   info.OrganizationID,
		Scope:                   scope,
		Mode:                    mode,
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
		CanUseAdminControls:     canUseAdminControls(info),
	}

	// max_duration_seconds=0 is the sentinel: "use org default" when
	// !allow_unbounded, "no cap" when allow_unbounded. Both leave the
	// pointer nil. Non-zero values pass through regardless so the
	// service-level mutual-exclusion check surfaces a (non-zero cap,
	// allow_unbounded=true) mismatch instead of silently dropping it.
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
	case *pb.StartCurtailmentRequest_Site:
		return curtailment.Scope{
			Type:   models.ScopeTypeSite,
			SiteID: s.Site.GetSiteId(),
		}, nil
	default:
		return curtailment.Scope{}, fleeterror.NewInvalidArgumentError(
			"scope is required: set whole_org, site, device_set_ids, or device_identifiers",
		)
	}
}

// startResponseState mirrors the persisted state for the synchronous Start response.
func startResponseState(req *pb.StartCurtailmentRequest, selected int) pb.CurtailmentEventState {
	if isClosedLoopFullFleetStartResponse(req) {
		return pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_ACTIVE
	}
	if req.GetMode() == pb.CurtailmentMode_CURTAILMENT_MODE_FULL_FLEET && selected == 0 {
		return pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_COMPLETED
	}
	return pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_PENDING
}

func isClosedLoopFullFleetStartResponse(req *pb.StartCurtailmentRequest) bool {
	if req.GetMode() != pb.CurtailmentMode_CURTAILMENT_MODE_FULL_FLEET {
		return false
	}
	switch req.GetScope().(type) {
	case *pb.StartCurtailmentRequest_WholeOrg, *pb.StartCurtailmentRequest_Site:
		return true
	default:
		return false
	}
}

// toStartResponse renders a newly persisted Plan + request as the
// response. Idempotent replays render from the persisted event row.
func toStartResponse(plan *curtailment.Plan, req *pb.StartCurtailmentRequest) *pb.StartCurtailmentResponse {
	event := &pb.CurtailmentEvent{
		State:                   startResponseState(req, len(plan.Selected)),
		Mode:                    requestModeProto(req.GetMode()),
		Strategy:                pb.CurtailmentStrategy_CURTAILMENT_STRATEGY_LEAST_EFFICIENT_FIRST,
		Level:                   pb.CurtailmentLevel_CURTAILMENT_LEVEL_FULL,
		Priority:                resolvePriority(req.GetPriority()),
		MaxDurationSeconds:      effectiveMaxDurationSeconds(plan, req),
		CurtailBatchSize:        int32PtrToUint32Ptr(plan.EffectiveCurtailBatchSize),
		CurtailBatchIntervalSec: uint32Saturating(plan.EffectiveCurtailBatchIntervalSec),
		RestoreBatchSize:        req.GetRestoreBatchSize(),
		RestoreBatchIntervalSec: effectiveRestoreBatchIntervalSec(plan, req),
		MinCurtailedDurationSec: req.GetMinCurtailedDurationSec(),
		IncludeMaintenance:      req.GetIncludeMaintenance(),
		ForceIncludeMaintenance: req.GetForceIncludeMaintenance(),
		Reason:                  req.GetReason(),
		ExternalSource:          req.GetExternalSource(),
		ExternalReference:       req.GetExternalReference(),
		IdempotencyKey:          req.GetIdempotencyKey(),
		EffectiveBatchSize:      uint32Saturating(plan.EffectiveBatchSize),
	}
	if plan.EventUUID != nil {
		event.EventUuid = plan.EventUUID.String()
	}
	if plan.StartedAt != nil {
		event.StartedAt = timestamppb.New(*plan.StartedAt)
	}
	if plan.EndedAt != nil {
		event.EndedAt = timestamppb.New(*plan.EndedAt)
	}
	switch s := req.GetScope().(type) {
	case *pb.StartCurtailmentRequest_WholeOrg:
		event.Scope = &pb.CurtailmentEvent_WholeOrg{WholeOrg: s.WholeOrg}
	case *pb.StartCurtailmentRequest_DeviceSetIds:
		event.Scope = &pb.CurtailmentEvent_DeviceSetIds{DeviceSetIds: s.DeviceSetIds}
	case *pb.StartCurtailmentRequest_DeviceIdentifiers:
		event.Scope = &pb.CurtailmentEvent_DeviceIdentifiers{DeviceIdentifiers: s.DeviceIdentifiers}
	case *pb.StartCurtailmentRequest_Site:
		event.Scope = &pb.CurtailmentEvent_Site{Site: s.Site}
	}
	if req.GetMode() == pb.CurtailmentMode_CURTAILMENT_MODE_FULL_FLEET {
		event.ModeParams = &pb.CurtailmentEvent_FullFleet{FullFleet: &pb.FullFleetParams{}}
	} else if fk := req.GetFixedKw(); fk != nil {
		event.ModeParams = &pb.CurtailmentEvent_FixedKw{FixedKw: fk}
	}

	var targets []*pb.CurtailmentTarget
	if !isClosedLoopFullFleetStartResponse(req) {
		// Open-loop starts persist targets immediately; reconciler updates them in-place.
		targets = make([]*pb.CurtailmentTarget, len(plan.Selected))
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
	}
	event.Targets = targets
	rollup := lenToInt32Saturating(len(targets))
	event.TargetRollup = &pb.CurtailmentTargetRollup{
		Pending: rollup,
		Total:   rollup,
	}

	return &pb.StartCurtailmentResponse{Event: event}
}

func effectiveRestoreBatchIntervalSec(plan *curtailment.Plan, req *pb.StartCurtailmentRequest) uint32 {
	if plan == nil || plan.EffectiveRestoreBatchIntervalSec <= 0 {
		return req.GetRestoreBatchIntervalSec()
	}
	return uint32Saturating(plan.EffectiveRestoreBatchIntervalSec)
}

// lenToInt32Saturating clamps a length to int32 max for proto rollups
// (selector lists are well below the cap; this satisfies static analysis).
func lenToInt32Saturating(n int) int32 {
	if n > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(n) // #nosec G115 -- bounds-checked above
}

func int64ToInt32Saturating(n int64) int32 {
	if n < 0 {
		return 0
	}
	if n > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(n) // #nosec G115 -- bounds-checked above
}

// effectiveMaxDurationSeconds prefers the persisted value (Service.Start
// resolves "use org default") so the response reflects the resolved cap;
// nil → allow_unbounded → 0.
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

// uint32ToInt32Strict converts proto uint32 → int32, rejecting overflow.
func uint32ToInt32Strict(field string, v uint32) (int32, error) {
	if v > math.MaxInt32 {
		return 0, fleeterror.NewInvalidArgumentErrorf(
			"%s exceeds int32 max: %d", field, v,
		)
	}
	return int32(v), nil // #nosec G115 -- bounds-checked above
}

// nonEmptyPtr maps proto3 "" → nil so SQL collapses to NULL and the
// migration's length>0 CHECKs hold.
func nonEmptyPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// deriveSourceActorType maps session.Info to the audit actor vocabulary;
// scheduler actor wins over auth method.
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

// deriveSourceActorID returns nil for nil / scheduler sessions (scheduler
// identity rides on actor_type).
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
		Mode:                      requestModeProto(req.GetMode()),
		SkippedCandidates:         skipped,
	}
	// Echo FIXED_KW params so the UI can render the undershoot delta.
	if fk := req.GetFixedKw(); fk != nil {
		resp.ModeParams = &pb.PreviewCurtailmentPlanResponse_FixedKw{FixedKw: fk}
	}
	return resp
}

func strategyName(s pb.CurtailmentStrategy) models.Strategy {
	if s == pb.CurtailmentStrategy_CURTAILMENT_STRATEGY_UNSPECIFIED ||
		s == pb.CurtailmentStrategy_CURTAILMENT_STRATEGY_LEAST_EFFICIENT_FIRST {
		return models.StrategyLeastEfficientFirst
	}
	// Other values pass through for service-side rejection.
	return models.Strategy(s.String())
}

// strategyReasonLabel renders reason_selected; UNSPECIFIED and
// LEAST_EFFICIENT_FIRST collapse to the canonical lowercase form.
func strategyReasonLabel(s pb.CurtailmentStrategy) string {
	if s == pb.CurtailmentStrategy_CURTAILMENT_STRATEGY_UNSPECIFIED ||
		s == pb.CurtailmentStrategy_CURTAILMENT_STRATEGY_LEAST_EFFICIENT_FIRST {
		return "least_efficient_first"
	}
	return s.String()
}

func levelName(l pb.CurtailmentLevel) models.Level {
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
		// Service validator rejects HIGH.
		return models.PriorityHigh
	default:
		// Surface unknown values for validator rejection instead of NORMAL.
		return models.Priority(p.String())
	}
}

// toInsufficientLoadError renders InvalidArgument with kW numbers and
// non-zero exclusion counters; order is source-fixed for byte-stable output.
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

// formatExclusionCounters renders non-zero ExcludedX fields in
// source-fixed order using the canonical SkipReason vocabulary.
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

// toStopRequest converts the proto request to a service StopRequest;
// Force is admin-gated at the handler.
func toStopRequest(msg *pb.StopCurtailmentRequest, orgID int64) (curtailment.StopRequest, error) {
	eventUUID, err := uuid.Parse(msg.GetEventUuid())
	if err != nil {
		return curtailment.StopRequest{}, fleeterror.NewInvalidArgumentErrorf(
			"event_uuid must be a valid UUID: %v", err,
		)
	}
	return curtailment.StopRequest{
		OrgID:     orgID,
		EventUUID: eventUUID,
		Force:     msg.GetForce(),
	}, nil
}

// toAdminTerminateRequest maps the proto request to the service-layer
// shape; the service re-checks target_state.
func toAdminTerminateRequest(msg *pb.AdminTerminateEventRequest, info *session.Info) (curtailment.AdminTerminateRequest, error) {
	if info == nil || info.OrganizationID <= 0 {
		return curtailment.AdminTerminateRequest{}, fleeterror.NewUnauthenticatedError("authentication required")
	}
	eventUUID, err := uuid.Parse(msg.GetEventUuid())
	if err != nil {
		return curtailment.AdminTerminateRequest{}, fleeterror.NewInvalidArgumentErrorf(
			"event_uuid must be a valid UUID: %v", err,
		)
	}
	return curtailment.AdminTerminateRequest{
		OrgID:       info.OrganizationID,
		EventUUID:   eventUUID,
		TargetState: eventStateFromProto(msg.GetTargetState()),
		Reason:      msg.GetReason(),
	}, nil
}

// toUpdateRequest maps the proto request to the service-layer shape.
// Optional proto fields preserve "set vs absent" semantics; the service
// handles all bounds validation.
func toUpdateRequest(msg *pb.UpdateCurtailmentEventRequest, info *session.Info) (curtailment.UpdateRequest, error) {
	if info == nil || info.OrganizationID <= 0 {
		return curtailment.UpdateRequest{}, fleeterror.NewUnauthenticatedError("authentication required")
	}
	eventUUID, err := uuid.Parse(msg.GetEventUuid())
	if err != nil {
		return curtailment.UpdateRequest{}, fleeterror.NewInvalidArgumentErrorf(
			"event_uuid must be a valid UUID: %v", err,
		)
	}
	out := curtailment.UpdateRequest{
		OrgID:     info.OrganizationID,
		EventUUID: eventUUID,
	}
	if msg.Reason != nil {
		v := msg.GetReason()
		out.Reason = &v
	}
	if msg.RestoreBatchSize != nil {
		v, err := uint32ToInt32Strict("restore_batch_size", msg.GetRestoreBatchSize())
		if err != nil {
			return curtailment.UpdateRequest{}, err
		}
		out.RestoreBatchSize = &v
	}
	if msg.RestoreBatchIntervalSec != nil {
		v, err := uint32ToInt32Strict("restore_batch_interval_sec", msg.GetRestoreBatchIntervalSec())
		if err != nil {
			return curtailment.UpdateRequest{}, err
		}
		out.RestoreBatchIntervalSec = &v
	}
	if msg.MaxDurationSeconds != nil {
		v, err := uint32ToInt32Strict("max_duration_seconds", msg.GetMaxDurationSeconds())
		if err != nil {
			return curtailment.UpdateRequest{}, err
		}
		out.MaxDurationSeconds = &v
	}
	return out, nil
}

// toListEventsRequest maps the proto request to the service-layer shape.
// The repeated state_filters field takes precedence over the legacy singular
// state_filter field. UNSPECIFIED filters collapse to "all states".
func toListEventsRequest(msg *pb.ListCurtailmentEventsRequest, orgID int64) (curtailment.ListEventsRequest, error) {
	if orgID <= 0 {
		return curtailment.ListEventsRequest{}, fleeterror.NewUnauthenticatedError("authentication required")
	}
	return curtailment.ListEventsRequest{
		OrgID:        orgID,
		PageSize:     msg.GetPageSize(),
		PageToken:    msg.GetPageToken(),
		StateFilters: eventStatesFromListEventsRequestProto(msg),
	}, nil
}

// toListEventsResponse builds the wire response; per-target rows are
// omitted to keep 10K-miner pages bounded.
func toListEventsResponse(events []*models.Event, nextPageToken string) *pb.ListCurtailmentEventsResponse {
	out := &pb.ListCurtailmentEventsResponse{
		Events:        make([]*pb.CurtailmentEvent, len(events)),
		NextPageToken: nextPageToken,
	}
	for i, ev := range events {
		out.Events[i] = toEventProtoListItem(ev)
	}
	return out
}

// toListActiveCurtailmentsResponse builds the active-events response: event
// metadata + scope, no per-device targets or decision snapshot (use
// GetCurtailmentEvent for detail). Replay handles are scrubbed as in the
// history list — a list view doesn't expose webhook trigger metadata.
func toListActiveCurtailmentsResponse(events []*models.Event) *pb.ListActiveCurtailmentsResponse {
	out := &pb.ListActiveCurtailmentsResponse{
		Events: make([]*pb.CurtailmentEvent, len(events)),
	}
	for i, ev := range events {
		e := toEventProto(ev)
		populateEventScope(e, ev)
		populateEventModeParams(e, ev)
		scrubListSensitiveFields(e)
		out.Events[i] = e
	}
	return out
}

// toEventProtoListItem populates the list-view shape (no targets).
// The decision snapshot arrives pre-trimmed at the SQL boundary; see
// ListCurtailmentEventsForOrg in queries/curtailment.sql.
func toEventProtoListItem(event *models.Event) *pb.CurtailmentEvent {
	out := toEventProto(event)
	scrubListSensitiveFields(out)
	populateEventScope(out, event)
	populateEventModeParams(out, event)
	populateEventDecisionSnapshot(out, event)
	return out
}

// scrubListSensitiveFields drops webhook trigger metadata from list rows.
func scrubListSensitiveFields(out *pb.CurtailmentEvent) {
	out.ExternalSource = ""
	out.ExternalReference = ""
	out.IdempotencyKey = ""
}

// toEventProto maps persisted event metadata to the wire CurtailmentEvent;
// use toEventProtoWithTargets for the full shape including targets.
func toEventProto(event *models.Event) *pb.CurtailmentEvent {
	out := &pb.CurtailmentEvent{
		EventUuid:               event.EventUUID.String(),
		State:                   eventStateProto(event.State),
		Mode:                    modeProto(event.Mode),
		Strategy:                strategyProto(event.Strategy),
		Level:                   levelProto(event.Level),
		Priority:                priorityProto(event.Priority),
		CurtailBatchSize:        int32PtrToUint32Ptr(event.CurtailBatchSize),
		CurtailBatchIntervalSec: uint32Saturating(event.CurtailBatchIntervalSec),
		RestoreBatchSize:        uint32Saturating(event.RestoreBatchSize),
		RestoreBatchIntervalSec: uint32Saturating(event.RestoreBatchIntervalSec),
		MinCurtailedDurationSec: uint32Saturating(event.MinCurtailedDurationSec),
		IncludeMaintenance:      event.IncludeMaintenance,
		ForceIncludeMaintenance: event.ForceIncludeMaintenance,
		Reason:                  event.Reason,
	}
	if event.MaxDurationSeconds != nil {
		out.MaxDurationSeconds = uint32Saturating(*event.MaxDurationSeconds)
	}
	if event.EffectiveBatchSize != nil {
		out.EffectiveBatchSize = uint32Saturating(*event.EffectiveBatchSize)
	}
	if event.ExternalSource != nil {
		out.ExternalSource = *event.ExternalSource
	}
	if event.ExternalReference != nil {
		out.ExternalReference = *event.ExternalReference
	}
	if event.IdempotencyKey != nil {
		out.IdempotencyKey = *event.IdempotencyKey
	}
	if event.ScheduledStartAt != nil {
		out.ScheduledStartAt = timestamppb.New(*event.ScheduledStartAt)
	}
	if event.StartedAt != nil {
		out.StartedAt = timestamppb.New(*event.StartedAt)
	}
	if event.EndedAt != nil {
		out.EndedAt = timestamppb.New(*event.EndedAt)
	}
	out.CreatedAt = timestamppb.New(event.CreatedAt)
	out.UpdatedAt = timestamppb.New(event.UpdatedAt)
	return out
}

func toEventProtoWithTargets(event *models.Event, targets []*models.Target) *pb.CurtailmentEvent {
	out := toEventProto(event)
	populateEventScope(out, event)
	populateEventModeParams(out, event)
	populateEventDecisionSnapshot(out, event)
	populateEventTargets(out, targets)
	populateEventTargetRollup(out, event)
	return out
}

func populateEventScope(out *pb.CurtailmentEvent, event *models.Event) {
	switch event.ScopeType {
	case models.ScopeTypeWholeOrg, "":
		out.Scope = &pb.CurtailmentEvent_WholeOrg{WholeOrg: &pb.ScopeWholeOrg{}}
	case models.ScopeTypeSite:
		var payload struct {
			SiteID int64 `json:"site_id"`
		}
		if err := json.Unmarshal(event.ScopeJSON, &payload); err == nil {
			out.Scope = &pb.CurtailmentEvent_Site{
				Site: &pb.ScopeSite{SiteId: payload.SiteID},
			}
		}
	case models.ScopeTypeDeviceList:
		var payload struct {
			DeviceIdentifiers []string `json:"device_identifiers"`
		}
		if err := json.Unmarshal(event.ScopeJSON, &payload); err == nil {
			out.Scope = &pb.CurtailmentEvent_DeviceIdentifiers{
				DeviceIdentifiers: &pb.ScopeDeviceList{DeviceIdentifiers: payload.DeviceIdentifiers},
			}
		}
	case models.ScopeTypeDeviceSets:
		var payload struct {
			DeviceSetIDs []string `json:"device_set_ids"`
		}
		if err := json.Unmarshal(event.ScopeJSON, &payload); err == nil {
			out.Scope = &pb.CurtailmentEvent_DeviceSetIds{
				DeviceSetIds: &pb.ScopeDeviceSets{DeviceSetIds: payload.DeviceSetIDs},
			}
		}
	}
}

func populateEventModeParams(out *pb.CurtailmentEvent, event *models.Event) {
	switch event.Mode {
	case models.ModeFullFleet:
		out.ModeParams = &pb.CurtailmentEvent_FullFleet{FullFleet: &pb.FullFleetParams{}}
	case models.ModeFixedKw:
		var payload struct {
			TargetKW    float64 `json:"target_kw"`
			ToleranceKW float64 `json:"tolerance_kw"`
		}
		if err := json.Unmarshal(event.ModeParamsJSON, &payload); err != nil {
			return
		}
		fk := &pb.FixedKwParams{TargetKw: payload.TargetKW}
		if payload.ToleranceKW > 0 {
			fk.ToleranceKw = &payload.ToleranceKW
		}
		out.ModeParams = &pb.CurtailmentEvent_FixedKw{FixedKw: fk}
	}
}

func populateEventDecisionSnapshot(out *pb.CurtailmentEvent, event *models.Event) {
	if len(event.DecisionSnapshotJSON) == 0 {
		return
	}
	var payload map[string]any
	if err := json.Unmarshal(event.DecisionSnapshotJSON, &payload); err != nil {
		return
	}
	s, err := structpb.NewStruct(payload)
	if err != nil {
		return
	}
	out.DecisionSnapshot = s
}

func populateEventTargets(out *pb.CurtailmentEvent, targets []*models.Target) {
	out.Targets = make([]*pb.CurtailmentTarget, 0, len(targets))
	pageRollup := &models.TargetRollup{}
	for _, target := range targets {
		out.Targets = append(out.Targets, toTargetProto(target))
		switch target.State {
		case models.TargetStatePending:
			pageRollup.Pending++
		case models.TargetStateDispatching, models.TargetStateDispatched:
			// Operator rollups conflate the pre-command transient with DISPATCHED.
			pageRollup.Dispatched++
		case models.TargetStateConfirmed:
			pageRollup.Confirmed++
		case models.TargetStateDrifted:
			pageRollup.Drifted++
		case models.TargetStateResolved:
			pageRollup.Resolved++
		case models.TargetStateReleased:
			pageRollup.Released++
		case models.TargetStateRestoreFailed:
			pageRollup.RestoreFailed++
		}
	}
	pageRollup.Total = int64(len(targets))
	out.TargetRollup = targetRollupProto(pageRollup)
}

func populateEventTargetRollup(out *pb.CurtailmentEvent, event *models.Event) {
	if event.TargetRollup == nil {
		return
	}
	out.TargetRollup = targetRollupProto(event.TargetRollup)
}

func targetRollupProto(rollup *models.TargetRollup) *pb.CurtailmentTargetRollup {
	return &pb.CurtailmentTargetRollup{
		Pending:       int64ToInt32Saturating(rollup.Pending),
		Dispatched:    int64ToInt32Saturating(rollup.Dispatched),
		Confirmed:     int64ToInt32Saturating(rollup.Confirmed),
		Drifted:       int64ToInt32Saturating(rollup.Drifted),
		Resolved:      int64ToInt32Saturating(rollup.Resolved),
		Released:      int64ToInt32Saturating(rollup.Released),
		RestoreFailed: int64ToInt32Saturating(rollup.RestoreFailed),
		Total:         int64ToInt32Saturating(rollup.Total),
	}
}

func toTargetProto(target *models.Target) *pb.CurtailmentTarget {
	out := &pb.CurtailmentTarget{
		DeviceIdentifier: target.DeviceIdentifier,
		TargetType:       target.TargetType,
		State:            targetStateProto(target.State),
		DesiredState:     desiredStateProto(target.DesiredState),
		RetryCount:       uint32Saturating(target.RetryCount),
	}
	if out.TargetType == "" {
		out.TargetType = "miner"
	}
	if target.BaselinePowerW != nil {
		out.BaselinePowerW = target.BaselinePowerW
	}
	if target.ObservedPowerW != nil {
		out.ObservedPowerW = target.ObservedPowerW
	}
	if !target.AddedAt.IsZero() {
		out.AddedAt = timestamppb.New(target.AddedAt)
	}
	if target.ReleasedAt != nil {
		out.ReleasedAt = timestamppb.New(*target.ReleasedAt)
	}
	if target.LastDispatchedAt != nil {
		out.LastDispatchedAt = timestamppb.New(*target.LastDispatchedAt)
	}
	if target.LastBatchUUID != nil {
		out.LastBatchUuid = *target.LastBatchUUID
	}
	if target.ObservedAt != nil {
		out.ObservedAt = timestamppb.New(*target.ObservedAt)
	}
	if target.ConfirmedAt != nil {
		out.ConfirmedAt = timestamppb.New(*target.ConfirmedAt)
	}
	if target.LastError != nil {
		out.LastError = *target.LastError
	}
	if phase := toTargetPhaseSummaryProto(target.CurtailPhase); phase != nil {
		out.CurtailPhase = phase
	}
	if target.RestorePhase != nil {
		out.RestorePhase = toTargetPhaseSummaryProto(*target.RestorePhase)
	}
	return out
}

func toTargetPhaseSummaryProto(summary models.TargetPhaseSummary) *pb.CurtailmentTargetPhaseSummary {
	if summary.Phase == "" && summary.State == "" {
		return nil
	}
	out := &pb.CurtailmentTargetPhaseSummary{
		Phase:        targetPhaseProto(summary.Phase),
		State:        targetStateProto(summary.State),
		RetryCount:   uint32Saturating(summary.RetryCount),
		FailureCount: uint32Saturating(summary.FailureCount),
	}
	if summary.StartedAt != nil {
		out.StartedAt = timestamppb.New(*summary.StartedAt)
	}
	if summary.DispatchedAt != nil {
		out.DispatchedAt = timestamppb.New(*summary.DispatchedAt)
	}
	if summary.BatchUUID != nil {
		out.BatchUuid = *summary.BatchUUID
	}
	if summary.CompletedAt != nil {
		out.CompletedAt = timestamppb.New(*summary.CompletedAt)
	}
	if summary.LastError != nil {
		out.LastError = *summary.LastError
	}
	return out
}

// uint32Saturating clamps a non-negative int32 to uint32, surfacing negative
// values as 0. Defensive; the source columns are >= 0 by DB CHECK.
func uint32Saturating(v int32) uint32 {
	if v < 0 {
		return 0
	}
	return uint32(v) // #nosec G115 -- bounds-checked above
}

func int32PtrToUint32Ptr(v *int32) *uint32 {
	if v == nil {
		return nil
	}
	out := uint32Saturating(*v)
	return &out
}

// eventStateFromProto is the inverse of eventStateProto. UNSPECIFIED maps
// to the canonical empty-string sentinel, signalling "no state filter" to
// the persistence layer.
func eventStateFromProto(s pb.CurtailmentEventState) models.EventState {
	switch s {
	case pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_UNSPECIFIED:
		return ""
	case pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_PENDING:
		return models.EventStatePending
	case pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_ACTIVE:
		return models.EventStateActive
	case pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_RESTORING:
		return models.EventStateRestoring
	case pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_COMPLETED:
		return models.EventStateCompleted
	case pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_COMPLETED_WITH_FAILURES:
		return models.EventStateCompletedWithFailures
	case pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_CANCELLED:
		return models.EventStateCancelled
	case pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_FAILED:
		return models.EventStateFailed
	}
	return ""
}

func eventStatesFromListEventsRequestProto(msg *pb.ListCurtailmentEventsRequest) []models.EventState {
	if filters := msg.GetStateFilters(); len(filters) > 0 {
		out := make([]models.EventState, 0, len(filters))
		for _, filter := range filters {
			state := eventStateFromProto(filter)
			if state != "" {
				out = append(out, state)
			}
		}
		return out
	}

	if state := eventStateFromProto(msg.GetStateFilter()); state != "" {
		return []models.EventState{state}
	}
	return nil
}

func eventStateProto(s models.EventState) pb.CurtailmentEventState {
	switch s {
	case models.EventStatePending:
		return pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_PENDING
	case models.EventStateActive:
		return pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_ACTIVE
	case models.EventStateRestoring:
		return pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_RESTORING
	case models.EventStateCompleted:
		return pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_COMPLETED
	case models.EventStateCompletedWithFailures:
		return pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_COMPLETED_WITH_FAILURES
	case models.EventStateCancelled:
		return pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_CANCELLED
	case models.EventStateFailed:
		return pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_FAILED
	}
	return pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_UNSPECIFIED
}

func targetStateProto(s models.TargetState) pb.CurtailmentTargetState {
	switch s {
	case models.TargetStatePending:
		return pb.CurtailmentTargetState_CURTAILMENT_TARGET_STATE_PENDING
	case models.TargetStateDispatching:
		return pb.CurtailmentTargetState_CURTAILMENT_TARGET_STATE_DISPATCHING
	case models.TargetStateDispatched:
		return pb.CurtailmentTargetState_CURTAILMENT_TARGET_STATE_DISPATCHED
	case models.TargetStateConfirmed:
		return pb.CurtailmentTargetState_CURTAILMENT_TARGET_STATE_CONFIRMED
	case models.TargetStateDrifted:
		return pb.CurtailmentTargetState_CURTAILMENT_TARGET_STATE_DRIFTED
	case models.TargetStateResolved:
		return pb.CurtailmentTargetState_CURTAILMENT_TARGET_STATE_RESOLVED
	case models.TargetStateReleased:
		return pb.CurtailmentTargetState_CURTAILMENT_TARGET_STATE_RELEASED
	case models.TargetStateRestoreFailed:
		return pb.CurtailmentTargetState_CURTAILMENT_TARGET_STATE_RESTORE_FAILED
	}
	return pb.CurtailmentTargetState_CURTAILMENT_TARGET_STATE_UNSPECIFIED
}

func desiredStateProto(s string) pb.CurtailmentTargetDesiredState {
	switch s {
	case models.DesiredStateCurtailed:
		return pb.CurtailmentTargetDesiredState_CURTAILMENT_TARGET_DESIRED_STATE_CURTAILED
	case models.DesiredStateActive:
		return pb.CurtailmentTargetDesiredState_CURTAILMENT_TARGET_DESIRED_STATE_ACTIVE
	}
	return pb.CurtailmentTargetDesiredState_CURTAILMENT_TARGET_DESIRED_STATE_UNSPECIFIED
}

func targetPhaseProto(s models.TargetPhase) pb.CurtailmentTargetPhase {
	switch s {
	case models.TargetPhaseCurtail:
		return pb.CurtailmentTargetPhase_CURTAILMENT_TARGET_PHASE_CURTAIL
	case models.TargetPhaseRestore:
		return pb.CurtailmentTargetPhase_CURTAILMENT_TARGET_PHASE_RESTORE
	}
	return pb.CurtailmentTargetPhase_CURTAILMENT_TARGET_PHASE_UNSPECIFIED
}

func modeProto(m models.Mode) pb.CurtailmentMode {
	switch m {
	case models.ModeFixedKw:
		return pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW
	case models.ModeFullFleet:
		return pb.CurtailmentMode_CURTAILMENT_MODE_FULL_FLEET
	default:
		return pb.CurtailmentMode_CURTAILMENT_MODE_UNSPECIFIED
	}
}

func strategyProto(s models.Strategy) pb.CurtailmentStrategy {
	if s == models.StrategyLeastEfficientFirst {
		return pb.CurtailmentStrategy_CURTAILMENT_STRATEGY_LEAST_EFFICIENT_FIRST
	}
	return pb.CurtailmentStrategy_CURTAILMENT_STRATEGY_UNSPECIFIED
}

func levelProto(l models.Level) pb.CurtailmentLevel {
	if l == models.LevelFull {
		return pb.CurtailmentLevel_CURTAILMENT_LEVEL_FULL
	}
	return pb.CurtailmentLevel_CURTAILMENT_LEVEL_UNSPECIFIED
}

func priorityProto(p models.Priority) pb.CurtailmentPriority {
	switch p {
	case models.PriorityNormal:
		return pb.CurtailmentPriority_CURTAILMENT_PRIORITY_NORMAL
	case models.PriorityHigh:
		return pb.CurtailmentPriority_CURTAILMENT_PRIORITY_HIGH
	case models.PriorityEmergency:
		return pb.CurtailmentPriority_CURTAILMENT_PRIORITY_EMERGENCY
	}
	return pb.CurtailmentPriority_CURTAILMENT_PRIORITY_UNSPECIFIED
}
