package curtailment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/modes"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// Scope identifies the target set: whole-org or explicit device-list;
// device-sets are deferred (resolver lives outside the curtailment domain).
type Scope struct {
	Type              models.ScopeType
	DeviceSetIDs      []string
	DeviceIdentifiers []string
}

// PreviewRequest is the service-level shape of a Preview call.
type PreviewRequest struct {
	OrgID                      int64
	Scope                      Scope
	Mode                       models.Mode     // must be ModeFixedKw
	Strategy                   models.Strategy // default StrategyLeastEfficientFirst
	Level                      models.Level    // must be LevelFull
	Priority                   models.Priority // PriorityNormal or PriorityEmergency (cooldown bypass)
	TargetKW                   float64
	ToleranceKW                float64
	IncludeMaintenance         bool
	ForceIncludeMaintenance    bool
	CandidateMinPowerWOverride *int32 // nil = use org default; admin-gated by handler
}

// StartRequest is the service-level shape of a Start call. Adds event-row
// fields (audit + operational controls) on top of PreviewRequest's
// selector inputs.
type StartRequest struct {
	PreviewRequest

	// Reason: operator-supplied audit string. Required (DB CHECK).
	Reason string

	// Zero values pass through verbatim; handler normalizes to org defaults.
	RestoreBatchSize        int32
	RestoreBatchIntervalSec int32
	MinCurtailedDurationSec int32

	// MaxDurationSeconds: nil when AllowUnbounded=true, else a finite cap.
	MaxDurationSeconds  *int32
	AllowUnbounded      bool
	CanUseAdminControls bool

	// External attribution. Empty-string normalizes to NULL at the store
	// boundary so partial-unique indexes only enforce uniqueness for set keys.
	IdempotencyKey    *string
	ExternalSource    *string
	ExternalReference *string

	// SourceActorType / SourceActorID: audit attribution. Handler derives
	// from session.Info; service stays session-free.
	SourceActorType models.SourceActorType
	SourceActorID   *string

	// CreatedByUserID: operator's user.id captured at handler entry.
	// Persisted on the event so reconciler dispatches under a real user
	// (command_batch_log.created_by has a NOT NULL FK to user.id).
	CreatedByUserID int64
}

// Service orchestrates Preview / Start through the shared config / scope /
// candidate / selector pipeline.
type Service struct {
	store   interfaces.CurtailmentStore
	metrics Metrics
	audit   AuditLogger
}

// ServiceOption configures a Service at construction time.
type ServiceOption func(*Service)

// WithServiceMetrics injects the operational metrics recorder; nil keeps
// the NoOpMetrics default.
func WithServiceMetrics(m Metrics) ServiceOption {
	return func(s *Service) {
		if m != nil {
			s.metrics = m
		}
	}
}

// WithAuditLogger injects the audit-log recorder; nil keeps the
// NoOpAuditLogger default.
func WithAuditLogger(a AuditLogger) ServiceOption {
	return func(s *Service) {
		if a != nil {
			s.audit = a
		}
	}
}

func NewService(store interfaces.CurtailmentStore, opts ...ServiceOption) *Service {
	s := &Service{
		store:   store,
		metrics: NoOpMetrics{},
		audit:   NoOpAuditLogger{},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Preview computes a curtailment plan without persisting any rows. Returns
// fleeterror typed errors the handler maps to Connect codes.
func (s *Service) Preview(ctx context.Context, req PreviewRequest) (*Plan, error) {
	if err := validatePreviewRequest(req); err != nil {
		return nil, err
	}
	plan, _, _, err := s.runSelector(ctx, req)
	if err != nil {
		return nil, err
	}
	return plan, nil
}

// Start runs Preview's selector pipeline and persists the event + targets.
// On OutcomeInsufficientLoad nothing is written; the Plan carries the
// rejection detail (mirrors Preview).
func (s *Service) Start(ctx context.Context, req StartRequest) (*Plan, error) {
	if err := validateStartRequest(req); err != nil {
		return nil, err
	}

	// Idempotent-replay lookup: a prior persisted match short-circuits
	// before selection so duplicate webhook deliveries don't re-run the
	// selector or trip the partial unique indexes. idempotency_key is
	// checked before external_reference so an operator handle can
	// override an upstream re-delivery.
	if existing, replayErr := s.lookupIdempotentReplay(ctx, req); replayErr != nil {
		return nil, replayErr
	} else if existing != nil {
		return s.replayPlanFromPersistedEvent(ctx, req.OrgID, existing)
	}

	plan, minPowerW, orgConfig, err := s.runSelector(ctx, req.PreviewRequest)
	if err != nil {
		return nil, err
	}

	// Start-only (not Preview) so debounced previews don't flood the
	// counter against a static fleet snapshot.
	for _, skip := range plan.Skipped {
		s.metrics.IncCandidateExcluded(string(skip.Reason))
	}

	if plan.InsufficientLoadDetail != nil {
		return plan, nil
	}

	if len(plan.Selected) == 0 && req.Mode != models.ModeFullFleet {
		// Defense-in-depth; FIXED_KW's validator + selector prevent this.
		return nil, fleeterror.NewInvalidArgumentError("no targets selected")
	}
	// FULL_FLEET with a genuinely empty scope is valid (nothing curtailable ==
	// vacuously off); it persists directly COMPLETED with no targets below.
	// runSelector rejects the unsafe non-empty/all-skipped case before this
	// point so automation cannot interpret "nothing actionable curtailed" as
	// satisfied.

	// max_duration_seconds=nil + !AllowUnbounded means "use org default".
	// Bounds-check the normalized value so a misconfigured default surfaces
	// as InvalidArgument instead of tripping the DB CHECK.
	if !req.AllowUnbounded && req.MaxDurationSeconds == nil {
		if orgConfig.MaxDurationDefaultSec <= 0 {
			return nil, fleeterror.NewInvalidArgumentErrorf(
				"org's max_duration_default_sec must be > 0, got %d", orgConfig.MaxDurationDefaultSec,
			)
		}
		if orgConfig.MaxDurationDefaultSec > maxFiniteDurationSeconds {
			return nil, fleeterror.NewInvalidArgumentErrorf(
				"org's max_duration_default_sec must be <= %d, got %d",
				maxFiniteDurationSeconds, orgConfig.MaxDurationDefaultSec,
			)
		}
		v := orgConfig.MaxDurationDefaultSec
		req.MaxDurationSeconds = &v
	}
	// Admin-gate is intrinsically post-normalization: it compares the
	// resolved value to the org default.
	if req.MaxDurationSeconds != nil &&
		orgConfig.MaxDurationDefaultSec > 0 &&
		*req.MaxDurationSeconds > orgConfig.MaxDurationDefaultSec &&
		!req.CanUseAdminControls {
		return nil, fleeterror.NewForbiddenErrorf(
			"only admins can set max_duration_seconds above org default %d",
			orgConfig.MaxDurationDefaultSec,
		)
	}
	if req.RestoreBatchIntervalSec == 0 {
		req.RestoreBatchIntervalSec = defaultRestoreBatchIntervalSec
	}
	if req.RestoreBatchIntervalSec > nonAdminRestoreBatchIntervalMax && !req.CanUseAdminControls {
		return nil, fleeterror.NewForbiddenErrorf(
			"only admins can set restore_batch_interval_sec above %d",
			nonAdminRestoreBatchIntervalMax,
		)
	}

	// Stamp once so buildInsertParams and the Start response agree.
	plan.EffectiveBatchSize = ComputeEffectiveBatchSize(req.RestoreBatchSize, int32(len(plan.Selected))) //nolint:gosec // bounded by per-org fleet size

	eventParams, targetParams, err := buildInsertParams(req, plan, minPowerW)
	if err != nil {
		return nil, err
	}
	// An event inserted already terminal (an empty FULL_FLEET start) resolved
	// instantly; stamp the completion time so history/replay don't surface a
	// completed event with no ended_at.
	if eventParams.State.IsTerminal() && eventParams.EndedAt == nil {
		now := time.Now().UTC()
		eventParams.EndedAt = &now
	}
	// Carry the stamped completion time into the Plan so the synchronous Start
	// response matches the persisted row (otherwise a later Get/List shows
	// ended_at but the Start response does not).
	plan.EndedAt = eventParams.EndedAt

	result, err := s.store.InsertEventWithTargets(ctx, eventParams, targetParams)
	if err != nil {
		// Webhook-replay race: re-issue the lookup so the loser falls
		// into the same replay path as a deliberate retry.
		if errors.Is(err, interfaces.ErrCurtailmentReplayRaceLoss) {
			if existing, replayErr := s.lookupIdempotentReplay(ctx, req); replayErr == nil && existing != nil {
				return s.replayPlanFromPersistedEvent(ctx, req.OrgID, existing)
			}
			return nil, fleeterror.NewAlreadyExistsError(
				"a curtailment event with the same idempotency_key or (external_source, external_reference) already exists",
			)
		}
		return nil, err
	}

	plan.EventUUID = &result.EventUUID
	plan.EffectiveMaxDurationSeconds = req.MaxDurationSeconds
	plan.EffectiveRestoreBatchIntervalSec = req.RestoreBatchIntervalSec

	s.emitStartAuditTrail(ctx, req, plan)
	if req.ForceIncludeMaintenance {
		s.metrics.IncMaintenanceOverride()
	}
	return plan, nil
}

func (s *Service) GetActive(ctx context.Context, orgID int64) (*models.Event, error) {
	if orgID <= 0 {
		return nil, fleeterror.NewInvalidArgumentError("org_id must be set")
	}
	return s.store.GetActiveEvent(ctx, orgID)
}

// ListActive returns every non-terminal event for the org, most-recent first.
// Multiple can be active when they target disjoint device scopes (e.g. one per
// site); GetActive returns only the most-recent.
func (s *Service) ListActive(ctx context.Context, orgID int64) ([]*models.Event, error) {
	if orgID <= 0 {
		return nil, fleeterror.NewInvalidArgumentError("org_id must be set")
	}
	return s.store.ListActiveEvents(ctx, orgID)
}

// ListEventsRequest is the service-level shape of a ListCurtailmentEvents
// call. PageToken is empty for the first page; subsequent pages reuse the
// next-page token returned by the previous call. StateFilters empty means
// all states; the handler maps proto enums to canonical EventState values.
type ListEventsRequest struct {
	OrgID        int64
	PageSize     int32
	PageToken    string
	StateFilters []models.EventState
}

// UpdateRequest is the service-level shape of an UpdateCurtailmentEvent
// call. Pointer fields use "nil = preserve, non-nil = write" semantics.
// CanUseAdminControls gates restore_batch_interval_sec above the
// non-admin cap, mirroring Start. effective_batch_size is not on this
// surface — recompute-vs-freeze of the batch size mid-event would race
// an in-flight restore claim, so operators who need a different batch
// size cancel and restart.
type UpdateRequest struct {
	OrgID                   int64
	EventUUID               uuid.UUID
	Reason                  *string
	RestoreBatchSize        *int32
	RestoreBatchIntervalSec *int32
	MaxDurationSeconds      *int32
	CanUseAdminControls     bool
}

// Update applies operator-safe field changes to a non-terminal event.
// State must be pending or active; restoring and terminal states reject
// with FailedPrecondition. The store re-asserts the state predicate as
// defense in depth — a race where the row advanced between the pre-read
// and the UPDATE surfaces as FailedPrecondition with a distinct message
// from the pre-read rejection.
func (s *Service) Update(ctx context.Context, req UpdateRequest) (*models.Event, error) {
	if req.OrgID <= 0 {
		return nil, fleeterror.NewInvalidArgumentError("org_id must be set")
	}
	if req.EventUUID == uuid.Nil {
		return nil, fleeterror.NewInvalidArgumentError("event_uuid must be set")
	}
	if err := validateUpdateRequest(req); err != nil {
		return nil, err
	}

	event, err := s.store.GetEventByUUID(ctx, req.OrgID, req.EventUUID)
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, fleeterror.NewNotFoundErrorf("curtailment event not found: %s", req.EventUUID)
	}

	switch event.State { //nolint:exhaustive // pending/active is the operator-safe surface; everything else maps to the default rejection.
	case models.EventStatePending, models.EventStateActive:
	default:
		return nil, fleeterror.NewFailedPreconditionErrorf(
			"curtailment event in state %q cannot be updated; restoring/terminal events reject operator-safe updates",
			event.State,
		)
	}

	// Collapse no-op patches before any gate or DB write so a UI re-submit
	// of an admin-elevated value doesn't trip the admin gate or bump
	// updated_at.
	patch := effectiveUpdatePatch(event, req)
	if patch.Reason == nil && patch.RestoreBatchSize == nil &&
		patch.RestoreBatchIntervalSec == nil && patch.MaxDurationSeconds == nil {
		return event, nil
	}

	// Admin gate mirrors Start. Compares against the effective patch so a
	// no-op echo passes; fetch org config lazily on a real write.
	if patch.MaxDurationSeconds != nil && !req.CanUseAdminControls {
		orgConfig, err := s.store.GetOrgConfig(ctx, req.OrgID)
		if err != nil {
			return nil, err
		}
		if orgConfig.MaxDurationDefaultSec > 0 &&
			*patch.MaxDurationSeconds > orgConfig.MaxDurationDefaultSec {
			return nil, fleeterror.NewForbiddenErrorf(
				"only admins can set max_duration_seconds above org default %d",
				orgConfig.MaxDurationDefaultSec,
			)
		}
	}
	// Symmetric gate for restore_batch_interval_sec above the non-admin cap.
	if patch.RestoreBatchIntervalSec != nil &&
		*patch.RestoreBatchIntervalSec > nonAdminRestoreBatchIntervalMax &&
		!req.CanUseAdminControls {
		return nil, fleeterror.NewForbiddenErrorf(
			"only admins can set restore_batch_interval_sec above %d",
			nonAdminRestoreBatchIntervalMax,
		)
	}

	updated, err := s.store.UpdateOperatorFields(ctx, event.ID, req.OrgID, patch)
	if err != nil {
		if errors.Is(err, interfaces.ErrCurtailmentEventStateRaceLoss) {
			return nil, fleeterror.NewFailedPreconditionError(
				"curtailment event state advanced during update; retry against the current event state",
			)
		}
		return nil, err
	}
	s.emitUpdateAuditTrail(ctx, updated, patch)
	return updated, nil
}

// effectiveUpdatePatch drops fields whose patched value matches the
// persisted value, so a same-value SQL UPDATE doesn't bump updated_at
// and a no-op echo of an admin-elevated value doesn't trip the admin gate.
func effectiveUpdatePatch(event *models.Event, req UpdateRequest) interfaces.UpdateOperatorFieldsParams {
	patch := interfaces.UpdateOperatorFieldsParams{}
	if req.Reason != nil && *req.Reason != event.Reason {
		patch.Reason = req.Reason
	}
	if req.RestoreBatchSize != nil && *req.RestoreBatchSize != event.RestoreBatchSize {
		patch.RestoreBatchSize = req.RestoreBatchSize
	}
	if req.RestoreBatchIntervalSec != nil && *req.RestoreBatchIntervalSec != event.RestoreBatchIntervalSec {
		patch.RestoreBatchIntervalSec = req.RestoreBatchIntervalSec
	}
	if req.MaxDurationSeconds != nil {
		switch {
		case event.MaxDurationSeconds == nil:
			patch.MaxDurationSeconds = req.MaxDurationSeconds
		case *event.MaxDurationSeconds != *req.MaxDurationSeconds:
			patch.MaxDurationSeconds = req.MaxDurationSeconds
		}
	}
	return patch
}

// Service-specific FleetErrorCode values ride on
// commonv1.FleetErrorDetails.Service so callers branch without
// string-matching the debug message. Stable — never renumber.
const (
	// FleetErrorCodeAdminTerminateInFlightCommands: a target still has an
	// in-flight Curtail. Recoverable via StopCurtailment first.
	FleetErrorCodeAdminTerminateInFlightCommands int32 = 1
	// FleetErrorCodeAdminTerminateStateConflict: event already terminal
	// in a different state. Not retryable.
	FleetErrorCodeAdminTerminateStateConflict int32 = 2
)

// AdminTerminateRequest is the service-level shape of an
// AdminTerminateEvent call. TargetState must be CANCELLED or FAILED;
// Reason is recorded as per-target last_error and on the audit row.
type AdminTerminateRequest struct {
	OrgID       int64
	EventUUID   uuid.UUID
	TargetState models.EventState
	Reason      string
}

// AdminTerminate forces a pending/restoring event to a terminal state and
// sweeps all non-terminal targets to RESTORE_FAILED. Active events must be
// stopped first so already-curtailed devices enter restore. Idempotent on a
// re-issue with the same target_state; FailedPrecondition when the event
// is already terminal in a different state. NotFound surfaces cross-org
// access attempts and stale operator references uniformly.
func (s *Service) AdminTerminate(ctx context.Context, req AdminTerminateRequest) (*models.Event, error) {
	if req.OrgID <= 0 {
		return nil, fleeterror.NewInvalidArgumentError("org_id must be set")
	}
	if req.EventUUID == uuid.Nil {
		return nil, fleeterror.NewInvalidArgumentError("event_uuid must be set")
	}
	if req.TargetState != models.EventStateCancelled && req.TargetState != models.EventStateFailed {
		return nil, fleeterror.NewInvalidArgumentErrorf(
			"target_state must be CANCELLED or FAILED, got %q", req.TargetState,
		)
	}
	if strings.TrimSpace(req.Reason) == "" {
		return nil, fleeterror.NewInvalidArgumentError("reason must be set")
	}
	// Reason is fanned out into every swept target's last_error column;
	// cap at proto's rune-based max_len so multi-byte input matches.
	if n := utf8.RuneCountInString(req.Reason); n > startTextFieldMaxLen {
		return nil, fleeterror.NewInvalidArgumentErrorf(
			"reason must be at most %d characters, got %d", startTextFieldMaxLen, n,
		)
	}

	updated, transitioned, err := s.store.AdminTerminateEvent(ctx, req.OrgID, req.EventUUID, req.TargetState, req.Reason)
	if err != nil {
		if errors.Is(err, interfaces.ErrCurtailmentAdminTerminateStateConflict) {
			return nil, fleeterror.NewErrorWithServiceCode(
				fmt.Sprintf("curtailment event is already terminal in a different state; admin terminate to %q is not applicable", req.TargetState),
				connect.CodeFailedPrecondition,
				FleetErrorCodeAdminTerminateStateConflict,
			)
		}
		if errors.Is(err, interfaces.ErrCurtailmentAdminTerminateActiveEvent) {
			return nil, fleeterror.NewErrorWithServiceCode(
				"curtailment event has miners with in-flight curtail commands; call StopCurtailment first to issue compensating uncurtail commands before admin termination",
				connect.CodeFailedPrecondition,
				FleetErrorCodeAdminTerminateInFlightCommands,
			)
		}
		return nil, err
	}
	// Replay rows on transitioned=false preserve a race-loser's actor +
	// reason that would otherwise be dropped from the audit feed.
	s.emitAdminTerminateAuditTrail(ctx, req, updated, transitioned)
	return updated, nil
}

// emitStartAuditTrail emits curtailment_started plus one row per
// override (allow_unbounded, force_include_maintenance). Best-effort:
// a transient audit failure increments IncAuditWriteFailure but doesn't
// roll back the committed Start.
func (s *Service) emitStartAuditTrail(ctx context.Context, req StartRequest, plan *Plan) {
	mode := req.Mode
	if mode == "" {
		mode = models.ModeFixedKw
	}
	metadata := map[string]any{
		"strategy":                  string(req.Strategy),
		"level":                     string(req.Level),
		"priority":                  string(req.Priority),
		"scope_type":                string(req.Scope.Type),
		"mode":                      string(mode),
		"selected_count":            len(plan.Selected),
		"skipped_count":             len(plan.Skipped),
		"allow_unbounded":           req.AllowUnbounded,
		"force_include_maintenance": req.ForceIncludeMaintenance,
		"source_actor":              string(req.SourceActorType),
	}
	if mode == models.ModeFixedKw {
		metadata["target_kw"] = req.TargetKW
		metadata["tolerance_kw"] = req.ToleranceKW
	}
	if plan.EventUUID != nil {
		metadata["event_uuid"] = plan.EventUUID.String()
	}
	if req.MaxDurationSeconds != nil {
		metadata["max_duration_seconds"] = *req.MaxDurationSeconds
	}
	if req.IdempotencyKey != nil {
		metadata["idempotency_key"] = *req.IdempotencyKey
	}
	if req.ExternalSource != nil {
		metadata["external_source"] = *req.ExternalSource
	}
	if req.ExternalReference != nil {
		metadata["external_reference"] = *req.ExternalReference
	}

	actorType := mapSourceActorTypeToActivity(req.SourceActorType)
	emit := func(eventType, description string) {
		event := activitymodels.Event{
			Category:    activitymodels.CategoryCurtailment,
			Type:        eventType,
			Description: description,
			Result:      activitymodels.ResultSuccess,
			Metadata:    metadata,
			ActorType:   actorType,
		}
		activity.StampActor(ctx, &event)
		if err := s.audit.LogStrict(ctx, event); err != nil {
			slog.Error("curtailment audit log failed",
				"activity_type", eventType, "event_uuid", plan.EventUUID, "error", err)
			s.metrics.IncAuditWriteFailure(eventType)
		}
	}

	emit(ActivityTypeStarted, "Curtailment event started")
	if req.AllowUnbounded {
		emit(ActivityTypeStartedUnbounded, "Curtailment event started with allow_unbounded override")
	}
	if req.ForceIncludeMaintenance {
		emit(ActivityTypeStartedForceMaintenance, "Curtailment event started with force_include_maintenance override")
	}
}

// emitAdminTerminateAuditTrail emits AdminTerminated when transitioned=true
// (primary row) or AdminTerminatedReplay when false (idempotent echo that
// preserves a race-loser's actor + reason). Best-effort.
func (s *Service) emitAdminTerminateAuditTrail(ctx context.Context, req AdminTerminateRequest, event *models.Event, transitioned bool) {
	if event == nil {
		return
	}
	eventType := ActivityTypeAdminTerminated
	description := "Curtailment event force-terminated by admin"
	if !transitioned {
		eventType = ActivityTypeAdminTerminatedReplay
		description = "Curtailment admin-terminate idempotent replay (event already terminal in this target state)"
	}
	metadata := map[string]any{
		"event_uuid":   event.EventUUID.String(),
		"target_state": string(req.TargetState),
		"reason":       req.Reason,
	}
	row := activitymodels.Event{
		Category:    activitymodels.CategoryCurtailment,
		Type:        eventType,
		Description: description,
		Result:      activitymodels.ResultSuccess,
		Metadata:    metadata,
		ActorType:   activitymodels.ActorUser,
	}
	activity.StampActor(ctx, &row)
	if err := s.audit.LogStrict(ctx, row); err != nil {
		slog.Error("curtailment audit log failed",
			"activity_type", eventType, "event_uuid", event.EventUUID, "error", err)
		s.metrics.IncAuditWriteFailure(eventType)
	}
}

// emitUpdateAuditTrail emits the Updated row with a "fields" metadata
// list so a feed reader sees operator intent without diffing.
func (s *Service) emitUpdateAuditTrail(ctx context.Context, event *models.Event, patch interfaces.UpdateOperatorFieldsParams) {
	if event == nil {
		return
	}
	changed := make([]string, 0, 4)
	metadata := map[string]any{
		"event_uuid": event.EventUUID.String(),
	}
	if patch.Reason != nil {
		changed = append(changed, "reason")
		metadata["reason"] = *patch.Reason
	}
	if patch.RestoreBatchSize != nil {
		changed = append(changed, "restore_batch_size")
		metadata["restore_batch_size"] = *patch.RestoreBatchSize
	}
	if patch.RestoreBatchIntervalSec != nil {
		changed = append(changed, "restore_batch_interval_sec")
		metadata["restore_batch_interval_sec"] = *patch.RestoreBatchIntervalSec
	}
	if patch.MaxDurationSeconds != nil {
		changed = append(changed, "max_duration_seconds")
		metadata["max_duration_seconds"] = *patch.MaxDurationSeconds
	}
	if len(changed) == 0 {
		return
	}
	metadata["fields"] = changed
	row := activitymodels.Event{
		Category:    activitymodels.CategoryCurtailment,
		Type:        ActivityTypeUpdated,
		Description: "Curtailment event operator fields updated",
		Result:      activitymodels.ResultSuccess,
		Metadata:    metadata,
		ActorType:   activitymodels.ActorUser,
	}
	activity.StampActor(ctx, &row)
	if err := s.audit.LogStrict(ctx, row); err != nil {
		slog.Error("curtailment audit log failed",
			"activity_type", ActivityTypeUpdated, "event_uuid", event.EventUUID, "error", err)
		s.metrics.IncAuditWriteFailure(ActivityTypeUpdated)
	}
}

// mapSourceActorTypeToActivity collapses curtailment's finer
// api_key/user split into the activity model's ActorUser; Scheduler
// maps directly.
func mapSourceActorTypeToActivity(t models.SourceActorType) activitymodels.ActorType {
	if t == models.SourceActorScheduler {
		return activitymodels.ActorScheduler
	}
	return activitymodels.ActorUser
}

// lookupIdempotentReplay returns the prior event a webhook-style replay
// reuses, or nil when no replay applies. idempotency_key wins over
// (external_source, external_reference).
func (s *Service) lookupIdempotentReplay(ctx context.Context, req StartRequest) (*models.Event, error) {
	if req.IdempotencyKey != nil && *req.IdempotencyKey != "" {
		existing, err := s.store.GetEventByIdempotencyKey(ctx, req.OrgID, *req.IdempotencyKey)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return existing, nil
		}
	}
	if req.ExternalSource != nil && *req.ExternalSource != "" &&
		req.ExternalReference != nil && *req.ExternalReference != "" {
		existing, err := s.store.GetEventByExternalReference(ctx, req.OrgID, *req.ExternalSource, *req.ExternalReference)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return existing, nil
		}
	}
	return nil, nil
}

// replayPlanFromPersistedEvent returns the persisted shape for an
// idempotency replay; the retry body is ignored — the row is the source
// of truth.
func (s *Service) replayPlanFromPersistedEvent(ctx context.Context, orgID int64, event *models.Event) (*Plan, error) {
	targets, err := s.store.ListTargetsByEvent(ctx, orgID, event.EventUUID)
	if err != nil {
		return nil, err
	}
	eventUUID := event.EventUUID
	plan := &Plan{
		EventUUID:                        &eventUUID,
		EffectiveRestoreBatchIntervalSec: event.RestoreBatchIntervalSec,
		ReplayEvent:                      event,
		ReplayTargets:                    targets,
	}
	if event.EffectiveBatchSize != nil {
		plan.EffectiveBatchSize = *event.EffectiveBatchSize
	}
	if event.MaxDurationSeconds != nil {
		v := *event.MaxDurationSeconds
		plan.EffectiveMaxDurationSeconds = &v
	}
	return plan, nil
}

// validateUpdateRequest mirrors the Start-time bounds so a misconfigured
// Update can't tunnel past the proto validator and hit a DB CHECK.
func validateUpdateRequest(req UpdateRequest) error {
	// Empty patches still bump updated_at via the COALESCE write — reject
	// them so the column doesn't drift on a no-op call.
	if req.Reason == nil &&
		req.RestoreBatchSize == nil &&
		req.RestoreBatchIntervalSec == nil &&
		req.MaxDurationSeconds == nil {
		return fleeterror.NewInvalidArgumentError(
			"at least one of reason, restore_batch_size, restore_batch_interval_sec, or max_duration_seconds must be set",
		)
	}
	if req.Reason != nil {
		v := *req.Reason
		if strings.TrimSpace(v) == "" {
			// Empty Reason silently no-ops via the UPDATE's COALESCE.
			return fleeterror.NewInvalidArgumentError("reason must be non-empty when set")
		}
		if n := utf8.RuneCountInString(v); n > startTextFieldMaxLen {
			return fleeterror.NewInvalidArgumentErrorf(
				"reason must be at most %d characters, got %d", startTextFieldMaxLen, n,
			)
		}
	}
	if req.RestoreBatchSize != nil {
		v := *req.RestoreBatchSize
		if v < 0 {
			return fleeterror.NewInvalidArgumentErrorf(
				"restore_batch_size must be >= 0, got %d", v,
			)
		}
	}
	if req.RestoreBatchIntervalSec != nil {
		v := *req.RestoreBatchIntervalSec
		if v < 0 {
			return fleeterror.NewInvalidArgumentErrorf(
				"restore_batch_interval_sec must be >= 0, got %d", v,
			)
		}
		if v > restoreBatchIntervalUpperBoundSec {
			return fleeterror.NewInvalidArgumentErrorf(
				"restore_batch_interval_sec must be <= %d, got %d",
				restoreBatchIntervalUpperBoundSec, v,
			)
		}
	}
	if req.MaxDurationSeconds != nil {
		v := *req.MaxDurationSeconds
		if v <= 0 {
			return fleeterror.NewInvalidArgumentErrorf(
				"max_duration_seconds must be > 0, got %d", v,
			)
		}
		if v > maxFiniteDurationSeconds {
			return fleeterror.NewInvalidArgumentErrorf(
				"max_duration_seconds must be <= %d, got %d",
				maxFiniteDurationSeconds, v,
			)
		}
	}
	return nil
}

// ListEvents returns cursor-paginated event history for an org. The
// store trims the decision_snapshot at the SQL boundary; the handler
// hydrates the JSONB into the wire field unchanged.
func (s *Service) ListEvents(ctx context.Context, req ListEventsRequest) ([]*models.Event, string, error) {
	if req.OrgID <= 0 {
		return nil, "", fleeterror.NewInvalidArgumentError("org_id must be set")
	}
	if req.PageSize < 0 {
		return nil, "", fleeterror.NewInvalidArgumentErrorf(
			"page_size must be >= 0, got %d", req.PageSize,
		)
	}
	return s.store.ListEvents(ctx, interfaces.ListEventsParams{
		OrgID:        req.OrgID,
		PageSize:     req.PageSize,
		PageToken:    req.PageToken,
		StateFilters: req.StateFilters,
	})
}

func (s *Service) GetActiveWithTargets(ctx context.Context, orgID int64) (*models.Event, []*models.Target, error) {
	event, err := s.GetActive(ctx, orgID)
	if err != nil || event == nil {
		return event, nil, err
	}
	targets, err := s.store.ListTargetsByEvent(ctx, orgID, event.EventUUID)
	if err != nil {
		return nil, nil, err
	}
	return event, targets, nil
}

func (s *Service) ListTargetsByEvent(ctx context.Context, orgID int64, eventUUID uuid.UUID) ([]*models.Target, error) {
	if orgID <= 0 {
		return nil, fleeterror.NewInvalidArgumentError("org_id must be set")
	}
	if eventUUID == uuid.Nil {
		return nil, fleeterror.NewInvalidArgumentError("event_uuid must be set")
	}
	return s.store.ListTargetsByEvent(ctx, orgID, eventUUID)
}

// runSelector runs the org-config → scope → candidate → classify →
// build-plan pipeline shared by Preview and Start. Returns the resolved
// candidate floor (for the decision snapshot) and the OrgConfig (so Start
// can resolve max_duration_seconds=0 without a second DB read).
func (s *Service) runSelector(ctx context.Context, req PreviewRequest) (*Plan, int32, *models.OrgConfig, error) {
	deviceFilter, err := resolveScope(req.Scope)
	if err != nil {
		return nil, 0, nil, err
	}
	// Empty-but-non-nil would match nothing under the query's `IS NULL` check.
	if len(deviceFilter) == 0 {
		deviceFilter = nil
	}

	orgConfig, err := s.store.GetOrgConfig(ctx, req.OrgID)
	if err != nil {
		return nil, 0, nil, err
	}

	// Effective candidate floor: per-org default, admin-overridable.
	// Handler enforces the admin role gate.
	minPowerW := orgConfig.CandidateMinPowerW
	if req.CandidateMinPowerWOverride != nil {
		minPowerW = *req.CandidateMinPowerWOverride
	}

	// EMERGENCY skips post_event_cooldown_sec.
	bypassCooldown := req.Priority == models.PriorityEmergency

	activeDevices, err := s.store.ListActiveCurtailedDevices(ctx, req.OrgID)
	if err != nil {
		return nil, 0, nil, err
	}
	activeSet := toStringSet(activeDevices)

	cooldownSet := map[string]struct{}{}
	if !bypassCooldown {
		cd, err := s.store.ListRecentlyResolvedCurtailedDevices(ctx, req.OrgID, orgConfig.PostEventCooldownSec)
		if err != nil {
			return nil, 0, nil, err
		}
		cooldownSet = toStringSet(cd)
	}

	candidates, err := s.store.ListCandidates(ctx, req.OrgID, deviceFilter)
	if err != nil {
		return nil, 0, nil, err
	}

	// Cross-org ids are silently dropped by the SQL org_id filter; surface
	// them as NotFound rather than masquerading as InsufficientLoad.
	if len(deviceFilter) > 0 {
		if missing := missingDeviceIdentifiers(deviceFilter, candidates); len(missing) > 0 {
			return nil, 0, nil, fleeterror.NewNotFoundErrorf(
				"device_identifiers not found in caller's org: %v", missing,
			)
		}
	}

	// TODO: registry-driven curtail_full capability check. classifyCandidates
	// already skips devices missing driver metadata as defense-in-depth.

	eligible, preFiltered, summary := classifyCandidates(candidates, classifyOpts{
		IncludeMaintenance: req.IncludeMaintenance && req.ForceIncludeMaintenance,
		ActiveEventDevices: activeSet,
		CooldownDevices:    cooldownSet,
		CandidateMinPowerW: minPowerW,
	})

	mode, err := buildMode(req.Mode, req.TargetKW, req.ToleranceKW, summary)
	if err != nil {
		return nil, 0, nil, err
	}

	plan := BuildPlan(eligible, preFiltered, minPowerW, mode)
	if req.Mode == models.ModeFullFleet && len(plan.Selected) == 0 && len(plan.Skipped) > 0 {
		detail := fullFleetAllSkippedDetail(plan.Skipped, minPowerW)
		plan.Outcome = modes.OutcomeInsufficientLoad
		plan.InsufficientLoadDetail = &detail
	}
	return &plan, minPowerW, orgConfig, nil
}

func fullFleetAllSkippedDetail(skipped []SkippedDevice, minPowerW int32) modes.InsufficientLoadDetail {
	detail := modes.InsufficientLoadDetail{CandidateMinPowerW: minPowerW}
	for _, skip := range skipped {
		switch skip.Reason {
		case SkipBelowThreshold:
			detail.ExcludedBelowThreshold++
		case SkipPhantomLoadNoHash:
			detail.ExcludedPhantomLoad++
		case SkipPowerTelemetryUnreliable:
			detail.ExcludedDeadMonitor++
		case SkipUnreachableResidualLoad:
			detail.ExcludedOffline++
		case SkipMaintenance:
			detail.ExcludedMaintenance++
		case SkipUpdating:
			detail.ExcludedUpdating++
		case SkipRebootRequired:
			detail.ExcludedRebootRequired++
		case SkipStaleTelemetry:
			detail.ExcludedStale++
		case SkipNonActionableStatus:
			detail.ExcludedNonActionable++
		case SkipPairing:
			detail.ExcludedPairing++
		case SkipCooldown:
			detail.ExcludedCooldown++
		case SkipActiveEvent:
			detail.ExcludedActiveEvent++
		case SkipCurtailFullUnsupported:
			detail.ExcludedCapabilityMiss++
		}
	}
	return detail
}

// buildMode constructs the selection mode from the request. FULL_FLEET takes
// the whole eligible set; the default (FIXED_KW, including the unset zero
// value) sizes selection to target_kw.
func buildMode(m models.Mode, targetKW, toleranceKW float64, summary modes.InsufficientLoadDetail) (modes.Mode, error) {
	if m == models.ModeFullFleet {
		return modes.FullFleet{}, nil
	}
	fk, err := modes.NewFixedKw(targetKW, toleranceKW, summary)
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("invalid FIXED_KW params: %v", err)
	}
	return fk, nil
}

const (
	// startTextFieldMaxLen mirrors proto max_len for the operator-supplied
	// text fields; backstop for non-Connect callers.
	startTextFieldMaxLen = 256

	maxFiniteDurationSeconds          int32 = 7 * 24 * 60 * 60
	defaultRestoreBatchIntervalSec    int32 = 30
	nonAdminRestoreBatchIntervalMax   int32 = 5 * 60
	restoreBatchIntervalUpperBoundSec int32 = 60 * 60
)

func validateStartRequest(req StartRequest) error {
	if err := validatePreviewRequest(req.PreviewRequest); err != nil {
		return err
	}
	if strings.TrimSpace(req.Reason) == "" {
		return fleeterror.NewInvalidArgumentError("reason must be non-empty")
	}
	if n := utf8.RuneCountInString(req.Reason); n > startTextFieldMaxLen {
		return fleeterror.NewInvalidArgumentErrorf(
			"reason must be at most %d characters, got %d", startTextFieldMaxLen, n,
		)
	}
	if req.IdempotencyKey != nil {
		if n := utf8.RuneCountInString(*req.IdempotencyKey); n > startTextFieldMaxLen {
			return fleeterror.NewInvalidArgumentErrorf(
				"idempotency_key must be at most %d characters, got %d", startTextFieldMaxLen, n,
			)
		}
	}
	if req.ExternalSource != nil {
		if n := utf8.RuneCountInString(*req.ExternalSource); n > startTextFieldMaxLen {
			return fleeterror.NewInvalidArgumentErrorf(
				"external_source must be at most %d characters, got %d", startTextFieldMaxLen, n,
			)
		}
	}
	if req.ExternalReference != nil {
		if n := utf8.RuneCountInString(*req.ExternalReference); n > startTextFieldMaxLen {
			return fleeterror.NewInvalidArgumentErrorf(
				"external_reference must be at most %d characters, got %d", startTextFieldMaxLen, n,
			)
		}
	}
	if req.RestoreBatchSize < 0 {
		return fleeterror.NewInvalidArgumentErrorf(
			"restore_batch_size must be >= 0, got %d", req.RestoreBatchSize,
		)
	}
	if req.RestoreBatchIntervalSec < 0 {
		return fleeterror.NewInvalidArgumentErrorf(
			"restore_batch_interval_sec must be >= 0, got %d", req.RestoreBatchIntervalSec,
		)
	}
	if req.MinCurtailedDurationSec < 0 {
		return fleeterror.NewInvalidArgumentErrorf(
			"min_curtailed_duration_sec must be >= 0, got %d", req.MinCurtailedDurationSec,
		)
	}
	// allow_unbounded + finite max_duration are mutually exclusive.
	if req.AllowUnbounded && req.MaxDurationSeconds != nil {
		return fleeterror.NewInvalidArgumentError(
			"max_duration_seconds must be unset when allow_unbounded is true",
		)
	}
	if req.AllowUnbounded && !req.CanUseAdminControls {
		return fleeterror.NewForbiddenError("only admins can set allow_unbounded")
	}
	if req.CandidateMinPowerWOverride != nil && !req.CanUseAdminControls {
		return fleeterror.NewForbiddenError("only admins can set candidate_min_power_w_override")
	}
	if req.ForceIncludeMaintenance && !req.CanUseAdminControls {
		return fleeterror.NewForbiddenError("only admins can set force_include_maintenance")
	}
	if !req.AllowUnbounded && req.MaxDurationSeconds != nil && *req.MaxDurationSeconds <= 0 {
		return fleeterror.NewInvalidArgumentErrorf(
			"max_duration_seconds must be > 0, got %d", *req.MaxDurationSeconds,
		)
	}
	if req.MaxDurationSeconds != nil && *req.MaxDurationSeconds > maxFiniteDurationSeconds {
		return fleeterror.NewInvalidArgumentErrorf(
			"max_duration_seconds must be <= %d, got %d",
			maxFiniteDurationSeconds, *req.MaxDurationSeconds,
		)
	}
	if req.RestoreBatchIntervalSec > restoreBatchIntervalUpperBoundSec {
		return fleeterror.NewInvalidArgumentErrorf(
			"restore_batch_interval_sec must be <= %d, got %d",
			restoreBatchIntervalUpperBoundSec, req.RestoreBatchIntervalSec,
		)
	}
	if req.SourceActorType == "" {
		return fleeterror.NewInvalidArgumentError("source_actor_type must be set")
	}
	if req.CreatedByUserID <= 0 {
		return fleeterror.NewInvalidArgumentError("created_by_user_id must be set")
	}
	return nil
}

func validatePreviewRequest(req PreviewRequest) error {
	if req.Mode != "" && req.Mode != models.ModeFixedKw && req.Mode != models.ModeFullFleet {
		return fleeterror.NewInvalidArgumentErrorf("mode %q is not supported; only FIXED_KW and FULL_FLEET", req.Mode)
	}
	if req.Level != "" && req.Level != models.LevelFull {
		return fleeterror.NewInvalidArgumentErrorf("level %q is not supported; only FULL", req.Level)
	}
	if req.Strategy != "" && req.Strategy != models.StrategyLeastEfficientFirst {
		return fleeterror.NewInvalidArgumentErrorf(
			"strategy %q is not supported; only LEAST_EFFICIENT_FIRST", req.Strategy,
		)
	}
	// HIGH is proto-reserved but unimplemented; reject explicitly.
	if req.Priority != "" && req.Priority != models.PriorityNormal && req.Priority != models.PriorityEmergency {
		return fleeterror.NewInvalidArgumentErrorf(
			"priority %q is not supported; use NORMAL or EMERGENCY", req.Priority,
		)
	}
	// FIXED_KW kW-target validation. FULL_FLEET curtails the whole eligible set
	// and ignores target_kw / tolerance_kw.
	if req.Mode != models.ModeFullFleet {
		// NaN/+/-Inf comparisons evaluate false, slipping past the > 0/>= 0
		// guards below and poisoning FixedKw's running sum.
		if math.IsNaN(req.TargetKW) || math.IsInf(req.TargetKW, 0) {
			return fleeterror.NewInvalidArgumentErrorf("target_kw must be a finite number, got %v", req.TargetKW)
		}
		if math.IsNaN(req.ToleranceKW) || math.IsInf(req.ToleranceKW, 0) {
			return fleeterror.NewInvalidArgumentErrorf("tolerance_kw must be a finite number, got %v", req.ToleranceKW)
		}
		if req.TargetKW <= 0 {
			return fleeterror.NewInvalidArgumentErrorf("target_kw must be > 0, got %v", req.TargetKW)
		}
		if req.ToleranceKW < 0 {
			return fleeterror.NewInvalidArgumentErrorf("tolerance_kw must be >= 0, got %v", req.ToleranceKW)
		}
		// tolerance_kw >= target_kw makes the undershoot branch trivially pass
		// at zero candidate sum, producing a misleading empty "successful" plan.
		if req.ToleranceKW >= req.TargetKW {
			return fleeterror.NewInvalidArgumentErrorf(
				"tolerance_kw must be < target_kw, got tolerance=%v target=%v",
				req.ToleranceKW, req.TargetKW,
			)
		}
	}
	// Bounds match proto-side validator; backstop for non-Connect callers.
	if req.CandidateMinPowerWOverride != nil &&
		(*req.CandidateMinPowerWOverride < 1 || *req.CandidateMinPowerWOverride > 10_000_000) {
		return fleeterror.NewInvalidArgumentErrorf(
			"candidate_min_power_w_override must be in [1, 10_000_000], got %d",
			*req.CandidateMinPowerWOverride,
		)
	}
	// Maintenance override pair is both-or-neither (DB CHECK is the backstop).
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
		// Empty Type defaults to whole-org, but device IDs alongside it
		// signal mismatched intent — reject rather than silently widening.
		if len(s.DeviceIdentifiers) > 0 || len(s.DeviceSetIDs) > 0 {
			return nil, fleeterror.NewInvalidArgumentError(
				"scope type must be set when device_identifiers or device_set_ids are provided",
			)
		}
		return nil, nil
	case models.ScopeTypeDeviceList:
		if len(s.DeviceIdentifiers) == 0 {
			return nil, fleeterror.NewInvalidArgumentError("device_identifiers must be non-empty for device-list scope")
		}
		// Oneof-style mutual exclusion for non-Connect callers.
		if len(s.DeviceSetIDs) > 0 {
			return nil, fleeterror.NewInvalidArgumentError(
				"device_set_ids must be empty when scope type is device_list",
			)
		}
		return s.DeviceIdentifiers, nil
	case models.ScopeTypeDeviceSets:
		// Deferred: device-set resolution requires DeviceSetStore wiring
		// outside the curtailment domain. Whole-org and device-list cover
		// the critical paths. Symmetric mutual-exclusion guard for callers
		// who set this Type with DeviceIdentifiers populated.
		if len(s.DeviceIdentifiers) > 0 {
			return nil, fleeterror.NewInvalidArgumentError(
				"device_identifiers must be empty when scope type is device_sets",
			)
		}
		return nil, fleeterror.NewUnimplementedErrorf("device-set scope is not implemented; use whole_org or device_list")
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

// classifyCandidates partitions candidates into selector inputs vs. a
// pre-filter skipped list with reasons; summary counts increment in lockstep
// so insufficient-load can echo per-reason totals without re-walking.
func classifyCandidates(cands []*models.Candidate, opts classifyOpts) ([]CandidateInput, []SkippedDevice, modes.InsufficientLoadDetail) {
	eligible := make([]CandidateInput, 0, len(cands))
	skipped := make([]SkippedDevice, 0, len(cands))
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
		// Capability gate: no driver → can't dispatch.
		if c.DriverName == nil || *c.DriverName == "" {
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipCurtailFullUnsupported})
			summary.ExcludedCapabilityMiss++
			continue
		}
		switch c.DeviceStatus {
		case "":
			// Missing device_status: not provably curtail-safe.
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipStaleTelemetry})
			summary.ExcludedStale++
			continue
		case "UPDATING":
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipUpdating})
			summary.ExcludedUpdating++
			continue
		case "REBOOT_REQUIRED":
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipRebootRequired})
			summary.ExcludedRebootRequired++
			continue
		case "OFFLINE":
			// Unaddressable; counted as residual load.
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipUnreachableResidualLoad})
			summary.ExcludedOffline++
			continue
		case "INACTIVE", "NEEDS_MINING_POOL":
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipNonActionableStatus})
			summary.ExcludedNonActionable++
			continue
		case "MAINTENANCE":
			if !opts.IncludeMaintenance {
				skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipMaintenance})
				summary.ExcludedMaintenance++
				continue
			}
			// Admitted via override pair; fall through to freshness check.
		}
		if c.LatestMetricsAt == nil {
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipStaleTelemetry})
			summary.ExcludedStale++
			continue
		}
		// NaN/Inf would slip past the dual-signal filter; treat as stale.
		if !isFiniteFloat(c.LatestPowerW) || !isFiniteFloat(c.LatestHashRateHS) {
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipStaleTelemetry})
			summary.ExcludedStale++
			continue
		}
		if _, cooled := opts.CooldownDevices[c.DeviceIdentifier]; cooled {
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipCooldown})
			summary.ExcludedCooldown++
			continue
		}
		// Non-finite avg_efficiency breaks sort transitivity; rank last.
		avgEff := c.AvgEfficiencyJH
		if !isFiniteFloat(avgEff) {
			avgEff = nil
		}
		eligible = append(eligible, CandidateInput{
			DeviceIdentifier: c.DeviceIdentifier,
			PowerW:           derefFloat(c.LatestPowerW),
			HashRateHS:       derefFloat(c.LatestHashRateHS),
			AvgEfficiencyJH:  avgEff,
		})
	}
	return eligible, skipped, summary
}

// missingDeviceIdentifiers returns requested IDs the org-scoped listing
// did not surface (cross-org or soft-deleted; both are out of scope).
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

// isFiniteFloat returns true for nil pointers; otherwise checks the
// pointee is neither NaN nor Inf.
func isFiniteFloat(p *float64) bool {
	if p == nil {
		return true
	}
	return !math.IsNaN(*p) && !math.IsInf(*p, 0)
}

// curtailment_target column values written at Start.
const targetTypeMiner = "miner"

// buildInsertParams assembles event + per-target params from a successful
// plan. baseline_power_w comes from the telemetry snapshot the selector
// ranked against; non-positive PowerW maps to NULL (a zero baseline would
// produce a misleading "100% reduction" report at restore).
func buildInsertParams(req StartRequest, plan *Plan, minPowerW int32) (models.InsertEventParams, []models.InsertTargetParams, error) {
	scopeJSON, err := marshalScopeJSON(req.Scope)
	if err != nil {
		return models.InsertEventParams{}, nil, err
	}
	mode := req.Mode
	if mode == "" {
		mode = models.ModeFixedKw
	}
	modeParamsJSON := []byte("{}")
	if mode == models.ModeFixedKw {
		modeParamsJSON, err = json.Marshal(map[string]float64{
			"target_kw":    req.TargetKW,
			"tolerance_kw": req.ToleranceKW,
		})
		if err != nil {
			return models.InsertEventParams{}, nil, fleeterror.NewInternalErrorf(
				"failed to encode mode_params: %v", err,
			)
		}
	}
	decisionJSON, err := marshalDecisionSnapshot(plan, minPowerW)
	if err != nil {
		return models.InsertEventParams{}, nil, err
	}

	// effective_batch_size is non-null from Start so Stop / restorer /
	// response paths just read the column.
	event := models.InsertEventParams{
		EventUUID:               uuid.New(),
		OrgID:                   req.OrgID,
		State:                   eventStartState(mode, len(plan.Selected)),
		Mode:                    mode,
		Strategy:                models.StrategyLeastEfficientFirst,
		Level:                   models.LevelFull,
		Priority:                req.Priority,
		LoopType:                models.LoopTypeOpen,
		ScopeType:               req.Scope.Type,
		ScopeJSON:               scopeJSON,
		ModeParamsJSON:          modeParamsJSON,
		RestoreBatchSize:        req.RestoreBatchSize,
		RestoreBatchIntervalSec: req.RestoreBatchIntervalSec,
		MinCurtailedDurationSec: req.MinCurtailedDurationSec,
		MaxDurationSeconds:      req.MaxDurationSeconds,
		AllowUnbounded:          req.AllowUnbounded,
		IncludeMaintenance:      req.IncludeMaintenance,
		ForceIncludeMaintenance: req.ForceIncludeMaintenance,
		DecisionSnapshotJSON:    decisionJSON,
		SourceActorType:         req.SourceActorType,
		SourceActorID:           req.SourceActorID,
		ExternalSource:          req.ExternalSource,
		ExternalReference:       req.ExternalReference,
		IdempotencyKey:          req.IdempotencyKey,
		Reason:                  req.Reason,
		CreatedByUserID:         req.CreatedByUserID,
		EffectiveBatchSize:      plan.EffectiveBatchSize,
	}
	if event.Priority == "" {
		event.Priority = models.PriorityNormal
	}
	if event.ScopeType == "" {
		event.ScopeType = models.ScopeTypeWholeOrg
	}

	targets := make([]models.InsertTargetParams, len(plan.Selected))
	for i, sel := range plan.Selected {
		var baseline *float64
		if sel.PowerW > 0 {
			v := sel.PowerW
			baseline = &v
		}
		targets[i] = models.InsertTargetParams{
			DeviceIdentifier: sel.DeviceIdentifier,
			TargetType:       targetTypeMiner,
			State:            models.TargetStatePending,
			DesiredState:     models.DesiredStateCurtailed,
			BaselinePowerW:   baseline,
		}
	}
	return event, targets, nil
}

// eventStartState is the state a freshly-built event is inserted with. A
// FULL_FLEET event with no eligible targets is vacuously complete on arrival
// (nothing to curtail or restore); everything else starts PENDING and the
// reconciler drives it.
func eventStartState(mode models.Mode, targetCount int) models.EventState {
	if mode == models.ModeFullFleet && targetCount == 0 {
		return models.EventStateCompleted
	}
	return models.EventStatePending
}

// marshalScopeJSON renders the request scope as the JSONB column value.
// Whole-org stores `{}` (NOT NULL).
func marshalScopeJSON(s Scope) ([]byte, error) {
	switch s.Type {
	case models.ScopeTypeWholeOrg, "":
		return []byte("{}"), nil
	case models.ScopeTypeDeviceList:
		b, err := json.Marshal(map[string][]string{
			"device_identifiers": s.DeviceIdentifiers,
		})
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to encode scope: %v", err)
		}
		return b, nil
	case models.ScopeTypeDeviceSets:
		b, err := json.Marshal(map[string][]string{
			"device_set_ids": s.DeviceSetIDs,
		})
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to encode scope: %v", err)
		}
		return b, nil
	default:
		return nil, fleeterror.NewInternalErrorf("unrecognized scope type: %q", s.Type)
	}
}

// StopRequest is the service-level shape of a Stop call. The handler maps
// it from `StopCurtailmentRequest` after deriving OrgID from session.Info
// and gating `Force` on Admin role.
type StopRequest struct {
	OrgID     int64
	EventUUID uuid.UUID
	Force     bool // admin-gated upstream; bypasses min_curtailed_duration_sec
}

// Adaptive batch-sizing constants. [10, 100] is the inrush envelope, computed
// at Start time from the selected target count.
const (
	minBatchSizeFloor   int32 = 10
	maxBatchSizeCeiling int32 = 100
)

// Stop transitions a non-terminal event to `restoring` and flips every
// non-terminal target to (desired_state='active', state='pending').
// Idempotent re-Stop returns the current row without writing; terminal
// events return FailedPrecondition.
func (s *Service) Stop(ctx context.Context, req StopRequest) (*models.Event, error) {
	if err := validateStopRequest(req); err != nil {
		return nil, err
	}

	event, err := s.store.GetEventByUUID(ctx, req.OrgID, req.EventUUID)
	if err != nil {
		return nil, err
	}

	// Fast-path check; BeginRestoreTransition's WHERE guard is authoritative.
	if event.State.IsTerminal() {
		return nil, fleeterror.NewFailedPreconditionErrorf(
			"cannot stop curtailment event %s in terminal state %q",
			event.EventUUID, event.State,
		)
	}
	if event.State == models.EventStateRestoring {
		// Idempotent re-Stop.
		return event, nil
	}

	if err := checkMinCurtailedDurationGate(event, req.Force, time.Now()); err != nil {
		return nil, err
	}

	return s.store.BeginRestoreTransition(ctx, req.OrgID, req.EventUUID)
}

// RecurtailRequest re-asserts curtailment on a restoring event.
type RecurtailRequest struct {
	OrgID     int64
	EventUUID uuid.UUID
}

// Recurtail flips a restoring event back to pending and reclaims restore
// targets. Non-restoring non-terminal events are idempotent; terminal events
// fail.
func (s *Service) Recurtail(ctx context.Context, req RecurtailRequest) (*models.Event, error) {
	if req.OrgID <= 0 {
		return nil, fleeterror.NewInvalidArgumentError("org_id must be set")
	}
	if req.EventUUID == uuid.Nil {
		return nil, fleeterror.NewInvalidArgumentError("event_uuid must be set")
	}
	return s.store.BeginRecurtailTransition(ctx, req.OrgID, req.EventUUID)
}

func validateStopRequest(req StopRequest) error {
	if req.OrgID <= 0 {
		return fleeterror.NewInvalidArgumentError("org_id must be set")
	}
	if req.EventUUID == uuid.Nil {
		return fleeterror.NewInvalidArgumentError("event_uuid must be set")
	}
	return nil
}

// checkMinCurtailedDurationGate enforces `min_curtailed_duration_sec`
// only on active events; admin force=true bypasses.
func checkMinCurtailedDurationGate(event *models.Event, force bool, now time.Time) error {
	if force {
		return nil
	}
	if event.State != models.EventStateActive {
		return nil
	}
	if event.MinCurtailedDurationSec <= 0 || event.StartedAt == nil {
		return nil
	}
	elapsed := now.Sub(*event.StartedAt)
	required := time.Duration(event.MinCurtailedDurationSec) * time.Second
	if elapsed >= required {
		return nil
	}
	return fleeterror.NewFailedPreconditionErrorf(
		"min_curtailed_duration_sec not elapsed: %ds of %ds; an admin can supply force=true on Stop to bypass this gate",
		int64(elapsed.Seconds()), event.MinCurtailedDurationSec,
	)
}

// ComputeEffectiveBatchSize returns max(restore_batch_size, ceil(0.01 × non_terminal_count))
// clamped to [minBatchSizeFloor, maxBatchSizeCeiling]. Stamped at Start;
// the restorer reads the column.
func ComputeEffectiveBatchSize(restoreBatchSize, nonTerminalCount int32) int32 {
	base := restoreBatchSize
	if base < 0 {
		base = 0
	}
	if nonTerminalCount > 0 {
		onePercent := int32(math.Ceil(float64(nonTerminalCount) * 0.01))
		if onePercent > base {
			base = onePercent
		}
	}
	if base < minBatchSizeFloor {
		base = minBatchSizeFloor
	}
	if base > maxBatchSizeCeiling {
		base = maxBatchSizeCeiling
	}
	return base
}

// marshalDecisionSnapshot captures the selector outputs for the
// decision_snapshot column (rejection counters, realized vs. requested
// kW, resolved candidate floor).
func marshalDecisionSnapshot(plan *Plan, minPowerW int32) ([]byte, error) {
	skipped := make([]map[string]string, len(plan.Skipped))
	for i, s := range plan.Skipped {
		skipped[i] = map[string]string{
			"device_identifier": s.DeviceIdentifier,
			"reason":            string(s.Reason),
		}
	}
	snapshot := map[string]any{
		"candidate_min_power_w":        minPowerW,
		"estimated_reduction_kw":       plan.EstimatedReductionKW,
		"estimated_remaining_power_kw": plan.EstimatedRemainingPowerKW,
		"selected_count":               len(plan.Selected),
		"skipped":                      skipped,
	}
	b, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf(
			"failed to encode decision_snapshot: %v", err,
		)
	}
	return b, nil
}
