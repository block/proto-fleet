package curtailment

import (
	"encoding/json"
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
	}

	if !out.AllowUnbounded {
		// max_duration_seconds=0 is the proto sentinel for "use the org's
		// configured default"; leave MaxDurationSeconds nil and let
		// Service.Start normalize against curtailment_org_config. Non-zero
		// values are bounds-checked rather than silently saturated so a
		// caller sending a wildly out-of-range value sees InvalidArgument.
		if raw := msg.GetMaxDurationSeconds(); raw > 0 {
			v, err := uint32ToInt32Strict("max_duration_seconds", raw)
			if err != nil {
				return curtailment.StartRequest{}, err
			}
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
// StartCurtailmentResponse. On the fresh-Start path the response describes
// the request that was just persisted; on the idempotent-retry path the
// service hands back a Plan carrying the persisted event + targets, and the
// response is built from those so a caller reusing an idempotency key with
// drifted metadata still sees the actually-stored values.
func toStartResponse(plan *curtailment.Plan, req *pb.StartCurtailmentRequest) *pb.StartCurtailmentResponse {
	if plan.PersistedEvent != nil {
		return startResponseFromPersisted(plan)
	}

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

	// Targets known at Start time are all PENDING; reconciler ticks update
	// them in-place. The retry path uses the persisted target rows instead.
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

// startResponseFromPersisted builds the response purely from the event +
// targets the service handed back from the idempotency short-circuit. The
// retry's request fields are deliberately ignored: a caller reusing a key
// with drifted metadata gets the persisted state, not their re-sent values.
// Scope and mode params are reconstructed from the same JSON shapes
// service.buildInsertParams wrote.
func startResponseFromPersisted(plan *curtailment.Plan) *pb.StartCurtailmentResponse {
	ev := plan.PersistedEvent
	event := &pb.CurtailmentEvent{
		EventUuid:               ev.EventUUID.String(),
		State:                   eventStateProto(ev.State),
		Mode:                    pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW,
		Strategy:                pb.CurtailmentStrategy_CURTAILMENT_STRATEGY_LEAST_EFFICIENT_FIRST,
		Level:                   pb.CurtailmentLevel_CURTAILMENT_LEVEL_FULL,
		Priority:                priorityProto(ev.Priority),
		MaxDurationSeconds:      effectiveMaxDurationSeconds(plan, nil),
		RestoreBatchSize:        int32ToUint32Saturating(ev.RestoreBatchSize),
		RestoreBatchIntervalSec: int32ToUint32Saturating(ev.RestoreBatchIntervalSec),
		MinCurtailedDurationSec: int32ToUint32Saturating(ev.MinCurtailedDurationSec),
		IncludeMaintenance:      ev.IncludeMaintenance,
		ForceIncludeMaintenance: ev.ForceIncludeMaintenance,
		Reason:                  ev.Reason,
		ExternalSource:          stringDeref(ev.ExternalSource),
		ExternalReference:       stringDeref(ev.ExternalReference),
		IdempotencyKey:          stringDeref(ev.IdempotencyKey),
	}
	setScopeFromPersisted(event, ev)
	setModeParamsFromPersisted(event, ev)

	targets := make([]*pb.CurtailmentTarget, len(plan.PersistedTargets))
	rollup := &pb.CurtailmentTargetRollup{}
	for i, t := range plan.PersistedTargets {
		pt := &pb.CurtailmentTarget{
			DeviceIdentifier: t.DeviceIdentifier,
			TargetType:       t.TargetType,
			State:            targetStateProto(t.State),
			DesiredState:     desiredStateProto(t.DesiredState),
			RetryCount:       int32ToUint32Saturating(t.RetryCount),
		}
		if t.BaselinePowerW != nil {
			v := *t.BaselinePowerW
			pt.BaselinePowerW = &v
		}
		if t.ObservedPowerW != nil {
			v := *t.ObservedPowerW
			pt.ObservedPowerW = &v
		}
		if t.LastError != nil {
			pt.LastError = *t.LastError
		}
		targets[i] = pt
		bumpRollup(rollup, t.State)
	}
	rollup.Total = lenToInt32Saturating(len(targets))
	event.Targets = targets
	event.TargetRollup = rollup

	return &pb.StartCurtailmentResponse{Event: event}
}

// setScopeFromPersisted unmarshals the scope_jsonb shape buildInsertParams
// wrote back into the proto Scope oneof on event. Malformed JSON or unknown
// scope types leave Scope unset rather than emitting a half-populated
// message. The helper mutates event because the oneof wrapper interface is
// unexported in the proto package.
func setScopeFromPersisted(event *pb.CurtailmentEvent, ev *models.Event) {
	switch ev.ScopeType {
	case models.ScopeTypeWholeOrg, "":
		event.Scope = &pb.CurtailmentEvent_WholeOrg{WholeOrg: &pb.ScopeWholeOrg{}}
	case models.ScopeTypeDeviceList:
		var v struct {
			DeviceIdentifiers []string `json:"device_identifiers"`
		}
		if err := json.Unmarshal(ev.ScopeJSON, &v); err != nil {
			return
		}
		event.Scope = &pb.CurtailmentEvent_DeviceIdentifiers{
			DeviceIdentifiers: &pb.ScopeDeviceList{DeviceIdentifiers: v.DeviceIdentifiers},
		}
	case models.ScopeTypeDeviceSets:
		var v struct {
			DeviceSetIDs []string `json:"device_set_ids"`
		}
		if err := json.Unmarshal(ev.ScopeJSON, &v); err != nil {
			return
		}
		event.Scope = &pb.CurtailmentEvent_DeviceSetIds{
			DeviceSetIds: &pb.ScopeDeviceSets{DeviceSetIds: v.DeviceSetIDs},
		}
	}
}

// setModeParamsFromPersisted unmarshals the mode_params_jsonb shape
// (target_kw + tolerance_kw scalars) into event's ModeParams oneof.
// tolerance_kw=0 stays nil to match a fresh-Start response when the caller
// never set a tolerance. Mutates event for the same unexported-oneof reason
// as setScopeFromPersisted.
func setModeParamsFromPersisted(event *pb.CurtailmentEvent, ev *models.Event) {
	if len(ev.ModeParamsJSON) == 0 {
		return
	}
	var v struct {
		TargetKW    float64 `json:"target_kw"`
		ToleranceKW float64 `json:"tolerance_kw"`
	}
	if err := json.Unmarshal(ev.ModeParamsJSON, &v); err != nil {
		return
	}
	fk := &pb.FixedKwParams{TargetKw: v.TargetKW}
	if v.ToleranceKW > 0 {
		t := v.ToleranceKW
		fk.ToleranceKw = &t
	}
	event.ModeParams = &pb.CurtailmentEvent_FixedKw{FixedKw: fk}
}

// bumpRollup increments the matching bucket on r. Unknown states fall through
// silently — Total still counts them, so a future TargetState addition is
// visible as total > sum-of-buckets rather than a panic.
func bumpRollup(r *pb.CurtailmentTargetRollup, s models.TargetState) {
	switch s {
	case models.TargetStatePending:
		r.Pending++
	case models.TargetStateDispatched:
		r.Dispatched++
	case models.TargetStateConfirmed:
		r.Confirmed++
	case models.TargetStateDrifted:
		r.Drifted++
	case models.TargetStateResolved:
		r.Resolved++
	case models.TargetStateReleased:
		r.Released++
	case models.TargetStateRestoreFailed:
		r.RestoreFailed++
	}
}

// stringDeref returns the empty string for nil pointers, matching the proto3
// scalar default the request-driven path emits.
func stringDeref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// int32ToUint32Saturating clamps non-negative int32 to uint32 for proto rollup
// and request-echo fields. Persisted values are validated >=0 at insert; the
// floor here is defense-in-depth so a corrupt row can't underflow.
func int32ToUint32Saturating(v int32) uint32 {
	if v < 0 {
		return 0
	}
	return uint32(v) // #nosec G115 -- bounds-checked above
}

// eventStateProto maps the persisted state string onto its proto enum.
// Unknown states pass through as UNSPECIFIED so downstream readers see the
// drift rather than receiving silently coerced PENDING.
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
	default:
		return pb.CurtailmentEventState_CURTAILMENT_EVENT_STATE_UNSPECIFIED
	}
}

// targetStateProto mirrors eventStateProto for per-target state.
func targetStateProto(s models.TargetState) pb.CurtailmentTargetState {
	switch s {
	case models.TargetStatePending:
		return pb.CurtailmentTargetState_CURTAILMENT_TARGET_STATE_PENDING
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
	default:
		return pb.CurtailmentTargetState_CURTAILMENT_TARGET_STATE_UNSPECIFIED
	}
}

// desiredStateProto maps the persisted desired_state string onto its proto
// enum. v1 only writes "curtailed"; unknown strings surface as UNSPECIFIED
// so a future column-value addition is visible rather than silently coerced.
func desiredStateProto(s string) pb.CurtailmentTargetDesiredState {
	if s == "curtailed" {
		return pb.CurtailmentTargetDesiredState_CURTAILMENT_TARGET_DESIRED_STATE_CURTAILED
	}
	return pb.CurtailmentTargetDesiredState_CURTAILMENT_TARGET_DESIRED_STATE_UNSPECIFIED
}

// priorityProto maps a persisted priority string onto its proto enum.
// PriorityHigh round-trips even though the validator rejects it on Start.
func priorityProto(p models.Priority) pb.CurtailmentPriority {
	switch p {
	case models.PriorityEmergency:
		return pb.CurtailmentPriority_CURTAILMENT_PRIORITY_EMERGENCY
	case models.PriorityHigh:
		return pb.CurtailmentPriority_CURTAILMENT_PRIORITY_HIGH
	case models.PriorityNormal, "":
		return pb.CurtailmentPriority_CURTAILMENT_PRIORITY_NORMAL
	default:
		return pb.CurtailmentPriority_CURTAILMENT_PRIORITY_UNSPECIFIED
	}
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

// effectiveMaxDurationSeconds prefers the value Service.Start actually
// persisted (after normalizing the "use org default" sentinel) so the
// response reflects the persisted cap rather than echoing the request's
// raw zero. Falls back to the request value on the Preview-style path
// where Plan does not carry a resolved value.
func effectiveMaxDurationSeconds(plan *curtailment.Plan, req *pb.StartCurtailmentRequest) uint32 {
	if plan != nil && plan.EffectiveMaxDurationSeconds != nil {
		v := *plan.EffectiveMaxDurationSeconds
		if v < 0 {
			return 0
		}
		return uint32(v) // #nosec G115 -- bounds-checked above
	}
	return req.GetMaxDurationSeconds()
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

// uint32ToInt32Strict converts a proto-uint32 to int32, rejecting overflow
// with InvalidArgument naming the field. Silent saturation at the
// translation boundary breaks request/response accuracy for valid protobuf
// inputs above MaxInt32, so callers see a clear error instead.
func uint32ToInt32Strict(field string, v uint32) (int32, error) {
	if v > math.MaxInt32 {
		return 0, fleeterror.NewInvalidArgumentErrorf(
			"%s exceeds int32 max: %d", field, v,
		)
	}
	return int32(v), nil // #nosec G115 -- bounds-checked above
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
