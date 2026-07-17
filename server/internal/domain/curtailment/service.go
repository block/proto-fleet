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
	"github.com/block/proto-fleet/server/internal/domain/infrastructure/driver"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// Scope identifies the target set. SiteIDs and DeviceIdentifiers are unioned
// for mixed scopes; the store resolves site membership without expanding it in
// callers.
type Scope struct {
	Type              models.ScopeType
	SiteID            int64
	SiteIDs           []int64
	DeviceSetIDs      []string
	DeviceIdentifiers []string
}

// PreviewRequest is the service-level shape of a Preview call.
type PreviewRequest struct {
	OrgID                       int64
	Scope                       Scope
	Mode                        models.Mode     // must be ModeFixedKw
	Strategy                    models.Strategy // default StrategyLeastEfficientFirst
	Level                       models.Level    // must be LevelFull
	Priority                    models.Priority // PriorityNormal or PriorityEmergency
	TargetKW                    float64
	ToleranceKW                 float64
	IncludeMaintenance          bool
	ForceIncludeMaintenance     bool
	ForceIncludeAllPairedMiners bool
	CandidateMinPowerWOverride  *int32 // nil = use org default; admin-gated by handler
	PostEventCooldownSec        int32
}

// StartRequest is the service-level shape of a Start call. Adds event-row
// fields (audit + operational controls) on top of PreviewRequest's
// selector inputs.
type StartRequest struct {
	PreviewRequest

	// Reason: operator-supplied audit string. Required (DB CHECK).
	Reason string

	// Zero values pass through verbatim as explicit immediate restore controls.
	RestoreBatchSize        int32
	RestoreBatchIntervalSec int32
	MinCurtailedDurationSec int32
	// Curtailed dispatch controls. Manual Start calls leave
	// UseProfileCurtailSettings=false so effective_batch_size also drives the
	// curtail batch. Automation/profile starts set it true so nil
	// CurtailBatchSize is persisted as NULL, meaning "curtail all selected
	// targets in scope."
	CurtailBatchSize          *int32
	CurtailBatchIntervalSec   int32
	UseProfileCurtailSettings bool

	FacilityFanDeviceIDs []int64
	AuthorizedFanSites   map[int64]int64
	FanOffDelaySec       int32
	FanRestoreDelaySec   int32

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
	store                    interfaces.CurtailmentStore
	fanStore                 interfaces.CurtailmentFanStateStore
	terminalFanRecoveryStore interfaces.CurtailmentTerminalFanRecoveryStore
	forceReleaseFanStore     interfaces.CurtailmentForceReleaseFanRecoveryStore
	metrics                  Metrics
	audit                    AuditLogger
	fans                     FacilityFanController
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

func WithFacilityFanController(controller FacilityFanController) ServiceOption {
	return func(s *Service) { s.fans = controller }
}

func NewService(store interfaces.CurtailmentStore, opts ...ServiceOption) *Service {
	s := &Service{
		store:   store,
		metrics: NoOpMetrics{},
		audit:   NoOpAuditLogger{},
	}
	if fanStore, ok := store.(interfaces.CurtailmentFanStateStore); ok {
		s.fanStore = fanStore
	}
	if terminalFanRecoveryStore, ok := store.(interfaces.CurtailmentTerminalFanRecoveryStore); ok {
		s.terminalFanRecoveryStore = terminalFanRecoveryStore
	}
	if forceReleaseFanStore, ok := store.(interfaces.CurtailmentForceReleaseFanRecoveryStore); ok {
		s.forceReleaseFanStore = forceReleaseFanStore
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
	req.PostEventCooldownSec = effectivePostEventCooldownSec(req)
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
	req.PostEventCooldownSec = effectivePostEventCooldownSec(req.PreviewRequest)

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
	// FULL_FLEET with a genuinely empty scope is valid (nothing currently
	// curtailable). Closed-loop scopes persist an active watcher so newly
	// eligible miners can be admitted while the event is asserted.
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
	if req.RestoreBatchIntervalSec > nonAdminRestoreBatchIntervalMax && !req.CanUseAdminControls {
		return nil, fleeterror.NewForbiddenErrorf(
			"only admins can set restore_batch_interval_sec above %d",
			nonAdminRestoreBatchIntervalMax,
		)
	}

	// Stamp once so buildInsertParams and the Start response agree.
	plan.EffectiveBatchSize = ComputeEffectiveBatchSize(req.RestoreBatchSize, int32(len(plan.Selected))) //nolint:gosec // bounded by per-org fleet size
	if !req.UseProfileCurtailSettings {
		req.CurtailBatchSize = defaultManualCurtailBatchSize(int32(len(plan.Selected))) //nolint:gosec // bounded by per-org fleet size
		req.CurtailBatchIntervalSec = 0
	}
	plan.EffectiveCurtailBatchSize = cloneInt32Ptr(req.CurtailBatchSize)
	plan.EffectiveCurtailBatchIntervalSec = req.CurtailBatchIntervalSec

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
	// Carry stamped lifecycle times into the Plan so the synchronous Start
	// response matches the persisted row (otherwise a later Get/List diverges).
	plan.EndedAt = eventParams.EndedAt
	plan.StartedAt = eventParams.StartedAt

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
	plan.EffectiveCurtailBatchSize = cloneInt32Ptr(req.CurtailBatchSize)
	plan.EffectiveCurtailBatchIntervalSec = req.CurtailBatchIntervalSec

	s.emitStartAuditTrail(ctx, req, plan)
	if req.ForceIncludeMaintenance {
		s.metrics.IncMaintenanceOverride()
	}
	return plan, nil
}

// ListActive returns every non-terminal event for the org, most-recent first.
// Multiple can be active when they target disjoint device scopes (e.g. one per
// site).
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

// GetEventWithTargetsRequest is the service-level shape for an expanded
// curtailment activity. TargetPageToken is empty for the first target page.
type GetEventWithTargetsRequest struct {
	OrgID           int64
	EventUUID       uuid.UUID
	TargetPageSize  int32
	TargetPageToken string
}

// UpdateRequest is the service-level shape of an UpdateCurtailmentEvent
// call. Pointer fields use "nil = preserve, non-nil = write" semantics.
// CanUseAdminControls gates restore_batch_interval_sec above the non-admin cap,
// mirroring Start. restore_batch_size accepts same-value echoes only:
// effective_batch_size is frozen at Start, so operators who need a different
// restore batch size cancel and restart.
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
	if req.RestoreBatchSize != nil && *req.RestoreBatchSize != event.RestoreBatchSize {
		return nil, fleeterror.NewInvalidArgumentError(
			"restore_batch_size cannot be changed after Start; cancel and restart the curtailment to change restore batch size",
		)
	}

	// Collapse no-op patches before any gate or DB write so a UI re-submit
	// of an admin-elevated value doesn't trip the admin gate or bump
	// updated_at.
	patch := effectiveUpdatePatch(event, req)
	if patch.Reason == nil && patch.RestoreBatchIntervalSec == nil && patch.MaxDurationSeconds == nil {
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
	if err := validateAdminRecoveryReason(req.Reason); err != nil {
		return nil, err
	}
	if s.fans != nil {
		event, err := s.store.GetEventByUUID(ctx, req.OrgID, req.EventUUID)
		if err != nil {
			return nil, err
		}
		if event.State == models.EventStatePending || event.State == models.EventStateRestoring {
			if err := s.restoreFansForOperatorRecovery(ctx, event); err != nil {
				return nil, err
			}
		}
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

// ForceReleaseRequest is the service-level shape for operator recovery that
// releases curtailment ownership immediately. It may issue best-effort
// facility fan restore commands, but it must not be reported as graceful
// miner restore completion.
type ForceReleaseRequest struct {
	OrgID     int64
	EventUUID uuid.UUID
	Reason    string
}

type ForceReleaseResult struct {
	Event               *models.Event
	ReleasedTargetCount int64
	OwnershipReleased   bool
	AutomationDisabled  bool
}

func (s *Service) ForceRelease(ctx context.Context, req ForceReleaseRequest) (*ForceReleaseResult, error) {
	if req.OrgID <= 0 {
		return nil, fleeterror.NewInvalidArgumentError("org_id must be set")
	}
	if req.EventUUID == uuid.Nil {
		return nil, fleeterror.NewInvalidArgumentError("event_uuid must be set")
	}
	if err := validateAdminRecoveryReason(req.Reason); err != nil {
		return nil, err
	}
	var recoveryEvent *models.Event
	if s.fans != nil {
		var err error
		recoveryEvent, err = s.store.GetEventByUUID(ctx, req.OrgID, req.EventUUID)
		if err != nil {
			return nil, err
		}
		// Force Release is also the explicit recovery path for a durable fan
		// failure on an already-terminal event. Reissuing it retries ON and a
		// successful write clears fan_last_error before the idempotent release.
		if recoveryEvent.State.IsTerminal() {
			if err := s.restoreFansForOperatorRecovery(ctx, recoveryEvent); err != nil {
				return nil, err
			}
		}
	}

	var released interfaces.ForceReleaseEventResult
	var err error
	if recoveryEvent != nil && !recoveryEvent.State.IsTerminal() && len(recoveryEvent.FacilityFanDeviceIDs) > 0 {
		// Keep the existing fan claims locked across cancellation and the
		// authoritative ON command. Otherwise a concurrent Start could claim
		// physically-off fans after cancellation but before recovery begins.
		if s.forceReleaseFanStore == nil {
			return nil, fleeterror.NewInternalError("force-release facility fan recovery store is unavailable")
		}
		now := time.Now().UTC()
		params := interfaces.UpdateCurtailmentFanStateParams{
			ExpectedEventState: models.EventStateCancelled,
		}
		if recoveryEvent.FanOnSentAt == nil {
			params.FanOnSentAt = &now
		}
		recoveryEvent.State = models.EventStateCancelled
		released, err = s.forceReleaseFanStore.ForceReleaseEventWithFanRecovery(
			ctx,
			req.OrgID,
			req.EventUUID,
			req.Reason,
			recoveryEvent.ID,
			recoveryEvent.FacilityFanDeviceIDs,
			recoveryEvent.FacilityFanSiteIDs,
			params,
			func(commandCtx context.Context) *string {
				return s.fans.SetState(commandCtx, recoveryEvent, driver.PowerOn)
			},
		)
	} else {
		released, err = s.store.ForceReleaseEvent(ctx, req.OrgID, req.EventUUID, req.Reason)
	}
	if released.Event != nil && recoveryEvent != nil && released.Event.FanOnSentAt == nil {
		released.Event.FanOnSentAt = recoveryEvent.FanOnSentAt
	}
	if released.OwnershipReleased {
		s.emitForceReleaseAuditTrail(ctx, req, released.Event, released.SweptTargets)
	}
	if err != nil {
		return nil, err
	}
	result := &ForceReleaseResult{
		Event:               released.Event,
		ReleasedTargetCount: released.SweptTargets,
		OwnershipReleased:   released.OwnershipReleased,
		AutomationDisabled:  released.AutomationDisabled,
	}
	return result, nil
}

func (s *Service) restoreFansForOperatorRecovery(ctx context.Context, event *models.Event) error {
	if event == nil || len(event.FacilityFanDeviceIDs) == 0 {
		return nil
	}
	failedTerminalOn := event.State.IsTerminal() && event.FanOnSentAt != nil && event.FanLastError != nil
	if event.FanOffSentAt == nil && !failedTerminalOn {
		return nil
	}
	now := time.Now().UTC()
	params := interfaces.UpdateCurtailmentFanStateParams{
		ExpectedEventState: event.State,
	}
	// A prior reconciler attempt may already have stamped the first ON attempt.
	// Preserve that sequencing timestamp while still making this final
	// best-effort command before an operator terminalizes the event.
	if event.FanOnSentAt == nil {
		params.FanOnSentAt = &now
	}
	if event.State.IsTerminal() {
		// A successful or never-failed terminal event has no outstanding
		// recovery work. Avoid reasserting stale state after a later event has
		// legitimately claimed the same fan.
		if event.FanLastError == nil {
			return nil
		}
		if s.terminalFanRecoveryStore == nil {
			return fleeterror.NewInternalError("terminal facility fan recovery store is unavailable")
		}
		var lastError *string
		err := s.terminalFanRecoveryStore.RecoverTerminalFanState(
			ctx,
			event.ID,
			event.OrgID,
			event.FacilityFanDeviceIDs,
			event.FacilityFanSiteIDs,
			params,
			func(commandCtx context.Context) *string {
				lastError = s.fans.SetState(commandCtx, event, driver.PowerOn)
				return lastError
			},
		)
		if err != nil {
			return err
		}
		if params.FanOnSentAt != nil {
			event.FanOnSentAt = params.FanOnSentAt
		}
		event.FanLastError = lastError
		return nil
	}
	if s.fanStore == nil {
		return fleeterror.NewInternalError("facility fan recovery state store is unavailable")
	}
	lastError, err := s.fanStore.CommandFanState(ctx, event.ID, params, func(commandCtx context.Context) *string {
		return s.fans.SetState(commandCtx, event, driver.PowerOn)
	})
	if err != nil {
		return fleeterror.NewInternalErrorf(
			"failed to persist facility fan operator recovery for event %s: %v",
			event.EventUUID,
			err,
		)
	}
	if params.FanOnSentAt != nil {
		event.FanOnSentAt = params.FanOnSentAt
	}
	event.FanLastError = lastError
	return nil
}

func validateAdminRecoveryReason(reason string) error {
	if strings.TrimSpace(reason) == "" {
		return fleeterror.NewInvalidArgumentError("reason must be set")
	}
	// Recovery reasons can be fanned out into target last_error columns; cap at
	// proto's rune-based max_len so multi-byte input matches validator behavior.
	if n := utf8.RuneCountInString(reason); n > startTextFieldMaxLen {
		return fleeterror.NewInvalidArgumentErrorf(
			"reason must be at most %d characters, got %d", startTextFieldMaxLen, n,
		)
	}
	return nil
}

// emitStartAuditTrail emits one curtailment_started row. Override flags ride
// on metadata so one curtailment cycle appears as one expandable activity row.
// Best-effort:
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
	if req.Scope.Type == models.ScopeTypeSite {
		metadata["site_id"] = req.Scope.SiteID
	}

	actorType := mapSourceActorTypeToActivity(req.SourceActorType)
	emit := func(eventType, description string) {
		scopeType := string(req.Scope.Type)
		scopeCount := len(plan.Selected)
		event := activitymodels.Event{
			Category:    activitymodels.CategoryCurtailment,
			Type:        eventType,
			Description: description,
			Result:      activitymodels.ResultSuccess,
			ScopeType:   &scopeType,
			ScopeCount:  &scopeCount,
			Metadata:    metadata,
			ActorType:   actorType,
		}
		if req.Scope.Type == models.ScopeTypeSite {
			siteID := req.Scope.SiteID
			event.SiteID = &siteID
		}
		activity.StampActor(ctx, &event)
		if event.OrganizationID == nil {
			orgID := req.OrgID
			event.OrganizationID = &orgID
		}
		if err := s.audit.LogStrict(ctx, event); err != nil {
			slog.Error("curtailment audit log failed",
				"activity_type", eventType, "event_uuid", plan.EventUUID, "error", err)
			s.metrics.IncAuditWriteFailure(eventType)
		}
	}

	emit(ActivityTypeStarted, "Curtailment event started")
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
	stampCurtailmentSite(&row, event)
	if err := s.audit.LogStrict(ctx, row); err != nil {
		slog.Error("curtailment audit log failed",
			"activity_type", eventType, "event_uuid", event.EventUUID, "error", err)
		s.metrics.IncAuditWriteFailure(eventType)
	}
}

func (s *Service) emitForceReleaseAuditTrail(ctx context.Context, req ForceReleaseRequest, event *models.Event, sweptTargets int64) {
	if event == nil {
		return
	}
	metadata := map[string]any{
		"event_uuid":         event.EventUUID.String(),
		"target_state":       string(models.TargetStateReleased),
		"event_state":        string(event.State),
		"reason":             req.Reason,
		"swept_target_count": sweptTargets,
	}
	row := activitymodels.Event{
		Category:    activitymodels.CategoryCurtailment,
		Type:        ActivityTypeForceReleased,
		Description: "Curtailment ownership force-released by admin",
		Result:      activitymodels.ResultSuccess,
		Metadata:    metadata,
		ActorType:   activitymodels.ActorUser,
	}
	activity.StampActor(ctx, &row)
	stampCurtailmentSite(&row, event)
	if err := s.audit.LogStrict(ctx, row); err != nil {
		slog.Error("curtailment audit log failed",
			"activity_type", ActivityTypeForceReleased, "event_uuid", event.EventUUID, "error", err)
		s.metrics.IncAuditWriteFailure(ActivityTypeForceReleased)
	}
}

// emitUpdateAuditTrail emits the Updated row with a "fields" metadata
// list so a feed reader sees operator intent without diffing.
func (s *Service) emitUpdateAuditTrail(ctx context.Context, event *models.Event, patch interfaces.UpdateOperatorFieldsParams) {
	if event == nil {
		return
	}
	changed := make([]string, 0, 3)
	metadata := map[string]any{
		"event_uuid": event.EventUUID.String(),
	}
	if patch.Reason != nil {
		changed = append(changed, "reason")
		metadata["reason"] = *patch.Reason
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
	stampCurtailmentSite(&row, event)
	if err := s.audit.LogStrict(ctx, row); err != nil {
		slog.Error("curtailment audit log failed",
			"activity_type", ActivityTypeUpdated, "event_uuid", event.EventUUID, "error", err)
		s.metrics.IncAuditWriteFailure(ActivityTypeUpdated)
	}
}

// stampCurtailmentSite stamps the activity row with the curtailment's site so
// lifecycle rows (Updated / AdminTerminated) land in /{site}/activity exactly
// like the curtailment_started row does. Site-scoped curtailments persist
// {"site_id": N} in ScopeJSON; whole-org / device-list / device-set scopes have
// no single site and stay NULL (CategoryCurtailment is org-level, so those rows
// surface only in the all-sites feed). The composite site FK / CHECK requires
// organization_id alongside site_id, so we ensure org_id is set (StampActor
// fills it from the session; fall back to the event's org for system actors).
func stampCurtailmentSite(row *activitymodels.Event, event *models.Event) {
	if event == nil || event.ScopeType != models.ScopeTypeSite {
		return
	}
	var scope struct {
		SiteID int64 `json:"site_id"`
	}
	if err := json.Unmarshal(event.ScopeJSON, &scope); err != nil || scope.SiteID <= 0 {
		return
	}
	siteID := scope.SiteID
	row.SiteID = &siteID
	if row.OrganizationID == nil {
		orgID := event.OrgID
		row.OrganizationID = &orgID
	}
}

// mapSourceActorTypeToActivity collapses curtailment's finer api_key/user split
// into the activity model's ActorUser. Scheduler and automation map to their
// synthetic actors.
func mapSourceActorTypeToActivity(t models.SourceActorType) activitymodels.ActorType {
	switch t {
	case models.SourceActorUser, models.SourceActorAPIKey, models.SourceActorWebhook:
		return activitymodels.ActorUser
	case models.SourceActorScheduler:
		return activitymodels.ActorScheduler
	case models.SourceActorAutomation:
		return activitymodels.ActorCurtailment
	default:
		return activitymodels.ActorUser
	}
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
	var targets []*models.Target
	if event.ForceIncludeAllPairedMiners {
		// All-paired starts persist one row per paired-like miner, so a
		// replay must stay count-only like the first-time response —
		// hydrating per-target rows would return a fleet-sized payload.
		rollup, err := s.store.GetTargetRollupByEvent(ctx, orgID, event.EventUUID)
		if err != nil {
			return nil, err
		}
		event.TargetRollup = rollup
	} else {
		var err error
		targets, err = s.store.ListTargetsByEvent(ctx, orgID, event.EventUUID)
		if err != nil {
			return nil, err
		}
	}
	eventUUID := event.EventUUID
	plan := &Plan{
		EventUUID:                        &eventUUID,
		EffectiveRestoreBatchIntervalSec: event.RestoreBatchIntervalSec,
		EffectiveCurtailBatchSize:        cloneInt32Ptr(event.CurtailBatchSize),
		EffectiveCurtailBatchIntervalSec: event.CurtailBatchIntervalSec,
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
		if v > RestoreBatchSizeMax {
			return fleeterror.NewInvalidArgumentErrorf(
				"restore_batch_size must be <= %d, got %d",
				RestoreBatchSizeMax,
				v,
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

// GetEvent returns a single event row without hydrating target details.
// Handlers use this to derive resource-scoped authorization from immutable
// persisted event scope before running lifecycle RPCs.
func (s *Service) GetEvent(ctx context.Context, orgID int64, eventUUID uuid.UUID) (*models.Event, error) {
	if orgID <= 0 {
		return nil, fleeterror.NewInvalidArgumentError("org_id must be set")
	}
	if eventUUID == uuid.Nil {
		return nil, fleeterror.NewInvalidArgumentError("event_uuid must be set")
	}
	event, err := s.store.GetEventByUUID(ctx, orgID, eventUUID)
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, fleeterror.NewNotFoundErrorf("curtailment event not found: %s", eventUUID)
	}
	return event, nil
}

// GetEventWithTargets returns a single historical or active event with a page
// of durable target phase summaries. ListEvents intentionally omits this heavy
// payload; activity detail views fetch it by event_uuid.
func (s *Service) GetEventWithTargets(ctx context.Context, req GetEventWithTargetsRequest) (*models.Event, []*models.Target, string, error) {
	if req.OrgID <= 0 {
		return nil, nil, "", fleeterror.NewInvalidArgumentError("org_id must be set")
	}
	if req.EventUUID == uuid.Nil {
		return nil, nil, "", fleeterror.NewInvalidArgumentError("event_uuid must be set")
	}
	if req.TargetPageSize < 0 {
		return nil, nil, "", fleeterror.NewInvalidArgumentErrorf(
			"target_page_size must be >= 0, got %d", req.TargetPageSize,
		)
	}
	event, err := s.store.GetEventDetailByUUID(ctx, req.OrgID, req.EventUUID)
	if err != nil {
		return nil, nil, "", err
	}
	event.TargetRollup, err = s.store.GetTargetRollupByEvent(ctx, req.OrgID, req.EventUUID)
	if err != nil {
		return nil, nil, "", err
	}
	targets, nextToken, err := s.store.ListTargetsByEventPage(ctx, interfaces.ListTargetsByEventPageParams{
		OrgID:     req.OrgID,
		EventUUID: req.EventUUID,
		PageSize:  req.TargetPageSize,
		PageToken: req.TargetPageToken,
	})
	if err != nil {
		return nil, nil, "", err
	}
	return event, targets, nextToken, nil
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

func (s *Service) ListTargetSiteCoverageByEvent(
	ctx context.Context,
	orgID int64,
	eventUUID uuid.UUID,
) (models.TargetSiteCoverage, error) {
	if orgID <= 0 {
		return models.TargetSiteCoverage{}, fleeterror.NewInvalidArgumentError("org_id must be set")
	}
	if eventUUID == uuid.Nil {
		return models.TargetSiteCoverage{}, fleeterror.NewInvalidArgumentError("event_uuid must be set")
	}
	return s.store.ListTargetSiteCoverageByEvent(ctx, orgID, eventUUID)
}

func (s *Service) ListTargetSiteCoverageByEvents(
	ctx context.Context,
	orgID int64,
	eventUUIDs []uuid.UUID,
) (map[uuid.UUID]models.TargetSiteCoverage, error) {
	if orgID <= 0 {
		return nil, fleeterror.NewInvalidArgumentError("org_id must be set")
	}
	if len(eventUUIDs) == 0 {
		return map[uuid.UUID]models.TargetSiteCoverage{}, nil
	}
	for _, eventUUID := range eventUUIDs {
		if eventUUID == uuid.Nil {
			return nil, fleeterror.NewInvalidArgumentError("event_uuid must be set")
		}
	}
	return s.store.ListTargetSiteCoverageByEvents(ctx, orgID, eventUUIDs)
}

// runSelector runs the org-config → scope → candidate → classify →
// build-plan pipeline shared by Preview and Start. Returns the resolved
// candidate floor (for the decision snapshot) and the OrgConfig (so Start
// can resolve max_duration_seconds=0 without a second DB read).
func (s *Service) runSelector(ctx context.Context, req PreviewRequest) (*Plan, int32, *models.OrgConfig, error) {
	candidateFilter, err := resolveScope(req.Scope)
	if err != nil {
		return nil, 0, nil, err
	}
	// Empty-but-non-nil would match nothing under the query's `IS NULL` check.
	if len(candidateFilter.DeviceIdentifiers) == 0 {
		candidateFilter.DeviceIdentifiers = nil
	}

	orgConfig, err := s.store.GetOrgConfig(ctx, req.OrgID)
	if err != nil {
		return nil, 0, nil, err
	}
	for _, siteID := range candidateFilter.SiteIDs {
		exists, err := s.store.SiteBelongsToOrg(ctx, req.OrgID, siteID)
		if err != nil {
			return nil, 0, nil, err
		}
		if !exists {
			return nil, 0, nil, fleeterror.NewNotFoundErrorf("site %d not found", siteID)
		}
	}

	// Effective candidate floor: per-org default, admin-overridable.
	// Handler enforces the admin role gate.
	minPowerW := orgConfig.CandidateMinPowerW
	if req.CandidateMinPowerWOverride != nil {
		minPowerW = *req.CandidateMinPowerWOverride
	}

	activeDevices, err := s.store.ListActiveCurtailedDevices(ctx, req.OrgID)
	if err != nil {
		return nil, 0, nil, err
	}
	activeSet := toStringSet(activeDevices)

	candidateFilter.OrgID = req.OrgID
	candidates, err := s.store.ListCandidates(ctx, candidateFilter)
	if err != nil {
		return nil, 0, nil, err
	}

	// Cross-org ids are silently dropped by the SQL org_id filter; surface
	// them as NotFound rather than masquerading as InsufficientLoad.
	if len(candidateFilter.DeviceIdentifiers) > 0 {
		if missing := missingDeviceIdentifiers(candidateFilter.DeviceIdentifiers, candidates); len(missing) > 0 {
			return nil, 0, nil, fleeterror.NewNotFoundErrorf(
				"device_identifiers not found in caller's org: %v", missing,
			)
		}
	}

	// All-paired policies intentionally own every paired-like miner in scope,
	// including miners that were recently restored. Cooldown remains enforced
	// for non-policy starts below.
	if req.ForceIncludeAllPairedMiners {
		plan := BuildAllPairedPolicyPlan(
			candidates,
			activeSet,
			req.IncludeMaintenance && req.ForceIncludeMaintenance,
			minPowerW,
		)
		return &plan, minPowerW, orgConfig, nil
	}

	cooldownSet := map[string]struct{}{}
	if req.PostEventCooldownSec > 0 {
		cooldownDevices, err := s.store.ListRecentlyResolvedCurtailedDevices(
			ctx,
			interfaces.ListRecentlyResolvedCurtailedDevicesParams{
				OrgID:             req.OrgID,
				CooldownSec:       req.PostEventCooldownSec,
				DeviceIdentifiers: candidateFilter.DeviceIdentifiers,
				SiteIDs:           candidateFilter.SiteIDs,
			},
		)
		if err != nil {
			return nil, 0, nil, err
		}
		cooldownSet = toStringSet(cooldownDevices)
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
	return &plan, minPowerW, orgConfig, nil
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
	nonAdminRestoreBatchIntervalMax   int32 = 5 * 60
	restoreBatchIntervalUpperBoundSec int32 = 60 * 60
	facilityFanDelayUpperBoundSec     int32 = 60 * 60
	// New response-profile selections are limited to eight, but Start retains
	// the historical API ceiling so profiles saved before that limit can still
	// execute until an operator deliberately reduces their fan set.
	facilityFanDeviceCountMax       = 8
	facilityFanDeviceCountLegacyMax = 1024
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
		if strings.TrimSpace(*req.ExternalSource) == automationExternalSource && req.SourceActorType != models.SourceActorAutomation {
			return fleeterror.NewInvalidArgumentError("external_source is reserved for curtailment automation")
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
	if req.RestoreBatchSize > RestoreBatchSizeMax {
		return fleeterror.NewInvalidArgumentErrorf(
			"restore_batch_size must be <= %d, got %d",
			RestoreBatchSizeMax,
			req.RestoreBatchSize,
		)
	}
	if req.CurtailBatchSize != nil && *req.CurtailBatchSize <= 0 {
		return fleeterror.NewInvalidArgumentErrorf(
			"curtail_batch_size must be > 0 when set, got %d",
			*req.CurtailBatchSize,
		)
	}
	if req.CurtailBatchSize != nil && *req.CurtailBatchSize > responseProfileBatchSizeMax {
		return fleeterror.NewInvalidArgumentErrorf(
			"curtail_batch_size must be <= %d, got %d",
			responseProfileBatchSizeMax,
			*req.CurtailBatchSize,
		)
	}
	if req.CurtailBatchIntervalSec < 0 {
		return fleeterror.NewInvalidArgumentErrorf(
			"curtail_batch_interval_sec must be >= 0, got %d",
			req.CurtailBatchIntervalSec,
		)
	}
	if req.CurtailBatchSize == nil && req.CurtailBatchIntervalSec > 0 {
		return fleeterror.NewInvalidArgumentError(
			"curtail_batch_interval_sec must be 0 when curtail_batch_size is unset",
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
	if req.ForceIncludeAllPairedMiners && !req.CanUseAdminControls {
		return fleeterror.NewForbiddenError("only admins can set force_include_all_paired_miners")
	}
	if req.FanOffDelaySec < 0 {
		return fleeterror.NewInvalidArgumentErrorf("fan_off_delay_sec must be >= 0, got %d", req.FanOffDelaySec)
	}
	if req.FanRestoreDelaySec < 0 {
		return fleeterror.NewInvalidArgumentErrorf("fan_restore_delay_sec must be >= 0, got %d", req.FanRestoreDelaySec)
	}
	if req.FanOffDelaySec > facilityFanDelayUpperBoundSec {
		return fleeterror.NewInvalidArgumentErrorf(
			"fan_off_delay_sec must be <= %d, got %d",
			facilityFanDelayUpperBoundSec, req.FanOffDelaySec,
		)
	}
	if req.FanRestoreDelaySec > facilityFanDelayUpperBoundSec {
		return fleeterror.NewInvalidArgumentErrorf(
			"fan_restore_delay_sec must be <= %d, got %d",
			facilityFanDelayUpperBoundSec, req.FanRestoreDelaySec,
		)
	}
	if len(req.FacilityFanDeviceIDs) > facilityFanDeviceCountLegacyMax {
		return fleeterror.NewInvalidArgumentErrorf(
			"facility_fan_device_ids must contain at most %d devices, got %d",
			facilityFanDeviceCountLegacyMax, len(req.FacilityFanDeviceIDs),
		)
	}
	seenFanIDs := make(map[int64]struct{}, len(req.FacilityFanDeviceIDs))
	for _, fanID := range req.FacilityFanDeviceIDs {
		if fanID <= 0 {
			return fleeterror.NewInvalidArgumentError("facility_fan_device_ids must be positive")
		}
		if _, exists := seenFanIDs[fanID]; exists {
			return fleeterror.NewInvalidArgumentError("facility_fan_device_ids must be unique")
		}
		seenFanIDs[fanID] = struct{}{}
	}
	if req.AuthorizedFanSites != nil {
		if len(req.AuthorizedFanSites) != len(seenFanIDs) {
			return fleeterror.NewFailedPreconditionError("authorized facility fan sites do not match facility_fan_device_ids")
		}
		for fanID := range seenFanIDs {
			if req.AuthorizedFanSites[fanID] <= 0 {
				return fleeterror.NewFailedPreconditionError("authorized facility fan sites do not match facility_fan_device_ids")
			}
		}
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
	if req.CurtailBatchIntervalSec > restoreBatchIntervalUpperBoundSec {
		return fleeterror.NewInvalidArgumentErrorf(
			"curtail_batch_interval_sec must be <= %d, got %d",
			restoreBatchIntervalUpperBoundSec, req.CurtailBatchIntervalSec,
		)
	}
	if req.CurtailBatchIntervalSec > nonAdminRestoreBatchIntervalMax && !req.CanUseAdminControls {
		return fleeterror.NewForbiddenErrorf(
			"only admins can set curtail_batch_interval_sec above %d",
			nonAdminRestoreBatchIntervalMax,
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
	if req.ForceIncludeAllPairedMiners && req.Mode != models.ModeFullFleet {
		return fleeterror.NewInvalidArgumentError("force_include_all_paired_miners requires FULL_FLEET")
	}
	// The policy's durable-ownership loop (release on unpair, reopen on
	// re-pair) only runs for closed-loop scopes; an open-loop (explicit
	// miner / device-set) all-paired event would release unpaired miners
	// and never reclaim them, silently breaking the policy's promise.
	if req.ForceIncludeAllPairedMiners && !isClosedLoopFullFleetStart(req.Scope, req.Mode) {
		return fleeterror.NewInvalidArgumentError(
			"force_include_all_paired_miners requires a whole-org or site scope; explicit miner or device-set scopes are not supported",
		)
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
	if err := validatePostEventCooldownSec(req.PostEventCooldownSec); err != nil {
		return err
	}
	// Maintenance override pair is both-or-neither (DB CHECK is the backstop).
	if req.IncludeMaintenance != req.ForceIncludeMaintenance {
		return fleeterror.NewInvalidArgumentError(
			"include_maintenance and force_include_maintenance must be set together",
		)
	}
	return nil
}

func effectivePostEventCooldownSec(req PreviewRequest) int32 {
	if req.Priority == models.PriorityEmergency {
		return 0
	}
	return req.PostEventCooldownSec
}

func resolveScope(s Scope) (interfaces.ListCandidatesParams, error) {
	if s.SiteID < 0 || hasNonPositiveInt64(s.SiteIDs) {
		return interfaces.ListCandidatesParams{}, fleeterror.NewInvalidArgumentError("site_ids must be positive")
	}
	s = normalizeScope(s)
	switch s.Type {
	case models.ScopeTypeWholeOrg, "":
		// Whole-org dominates any narrower selectors supplied by composable
		// clients, matching "all sites" behavior without expanding sites.
		return interfaces.ListCandidatesParams{}, nil
	case models.ScopeTypeSite:
		if len(s.SiteIDs) != 1 {
			return interfaces.ListCandidatesParams{}, fleeterror.NewInvalidArgumentError("site_id must be set for site scope")
		}
		return interfaces.ListCandidatesParams{SiteIDs: s.SiteIDs}, nil
	case models.ScopeTypeDeviceList:
		if len(s.DeviceIdentifiers) == 0 {
			return interfaces.ListCandidatesParams{}, fleeterror.NewInvalidArgumentError("device_identifiers must be non-empty for device-list scope")
		}
		return interfaces.ListCandidatesParams{DeviceIdentifiers: s.DeviceIdentifiers}, nil
	case models.ScopeTypeMixed:
		if len(s.DeviceSetIDs) > 0 {
			return interfaces.ListCandidatesParams{}, fleeterror.NewUnimplementedErrorf("device-set scope is not implemented; use whole_org, site, or device_list")
		}
		if len(s.SiteIDs) == 0 && len(s.DeviceIdentifiers) == 0 {
			return interfaces.ListCandidatesParams{}, fleeterror.NewInvalidArgumentError("mixed scope must include site_ids or device_identifiers")
		}
		return interfaces.ListCandidatesParams{
			SiteIDs:           s.SiteIDs,
			DeviceIdentifiers: s.DeviceIdentifiers,
		}, nil
	case models.ScopeTypeDeviceSets:
		// Deferred: device-set resolution requires DeviceSetStore wiring
		// outside the curtailment domain. Whole-org and device-list cover
		// the critical paths. Symmetric mutual-exclusion guard for callers
		// who set this Type with DeviceIdentifiers populated.
		return interfaces.ListCandidatesParams{}, fleeterror.NewUnimplementedErrorf("device-set scope is not implemented; use whole_org, site, or device_list")
	default:
		return interfaces.ListCandidatesParams{}, fleeterror.NewInvalidArgumentErrorf("unrecognized scope type: %q", s.Type)
	}
}

func normalizeScope(s Scope) Scope {
	siteIDs := append([]int64(nil), s.SiteIDs...)
	if s.SiteID > 0 {
		siteIDs = append(siteIDs, s.SiteID)
	}
	s.SiteIDs = uniquePositiveInt64s(siteIDs)
	if len(s.SiteIDs) == 1 {
		s.SiteID = s.SiteIDs[0]
	} else {
		s.SiteID = 0
	}
	s.DeviceIdentifiers = uniqueNonEmptyStrings(s.DeviceIdentifiers)
	s.DeviceSetIDs = uniqueNonEmptyStrings(s.DeviceSetIDs)

	if s.Type == models.ScopeTypeWholeOrg {
		return s
	}
	if len(s.DeviceSetIDs) > 0 {
		s.Type = models.ScopeTypeDeviceSets
		return s
	}
	switch {
	case len(s.SiteIDs) > 0 && len(s.DeviceIdentifiers) > 0:
		s.Type = models.ScopeTypeMixed
	case len(s.SiteIDs) > 1:
		s.Type = models.ScopeTypeMixed
	case len(s.SiteIDs) == 1:
		s.Type = models.ScopeTypeSite
	case len(s.DeviceIdentifiers) > 0:
		s.Type = models.ScopeTypeDeviceList
	case s.Type == "":
		s.Type = models.ScopeTypeWholeOrg
	}
	return s
}

func uniquePositiveInt64s(values []int64) []int64 {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(values))
	out := make([]int64, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func uniqueNonEmptyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
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
//
// AllPairedPolicyTargetState (selector.go) classifies the same
// device_status_enum vocabulary for the all-paired policy with deliberately
// different outcomes (see the note on its ERROR/UNKNOWN arm). When adding a
// device status, update both switches and the pinned matrix in
// TestDeviceStatusClassifierMatrix.
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
		case "INACTIVE":
			// Excluded by design: INACTIVE means the miner is sleeping
			// (operator- or curtailment-initiated). Curtailing it is a no-op
			// and restoring it would wake a miner someone deliberately put
			// to sleep.
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
		case "ERROR", "UNKNOWN":
			// Intentionally admitted when telemetry is fresh: operator-sized
			// selection trusts live power/hash samples over the coarse status.
			// The all-paired policy holds these unavailable instead because it
			// dispatches without the freshness gates below
			// (AllPairedPolicyTargetState in selector.go).
		case deviceStatusNeedsMiningPool:
			// Commandability admission (#663): a pool-less miner is reachable,
			// authenticated, and draws idle power — a sleep command lands.
			// Subject to the freshness gates below, plus a positive-power
			// requirement: power-vs-baseline is the only signal that can
			// confirm curtail/restore for a never-hashing miner, so a
			// zero-power sample is as good as stale. Its hash is forced to 0
			// for selection (statusAuthoritativeHashRateHS): a pool-less
			// miner cannot be mining, so a stale-positive hash sample must
			// not let fixed-kW count it as curtailable mining load — the
			// dual-signal filter excludes it there.
			if !hasPositivePowerSample(c) {
				skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipStaleTelemetry})
				summary.ExcludedStale++
				continue
			}
		}
		if c.LatestMetricsAt == nil {
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipStaleTelemetry})
			summary.ExcludedStale++
			continue
		}
		// Missing, non-finite, or negative power/hash samples cannot prove the
		// miner is observable after dispatch; treat them as stale telemetry.
		// Pool-less miners are exempt from the hash-sample requirement: their
		// hash is status-authoritatively 0 (a miner with no pool cannot be
		// mining, and may never have reported a hash sample), and their
		// positive-power requirement was already enforced above.
		requiresHashSample := c.DeviceStatus != deviceStatusNeedsMiningPool
		if !hasNonNegativeFiniteFloat(c.LatestPowerW) ||
			(requiresHashSample && !hasNonNegativeFiniteFloat(c.LatestHashRateHS)) {
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
			HashRateHS:       statusAuthoritativeHashRateHS(c),
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

func hasNonNegativeFiniteFloat(p *float64) bool {
	return p != nil && isFiniteFloat(p) && *p >= 0
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
	scope := normalizeScope(req.Scope)
	scopeJSON, err := MarshalScopeJSON(scope)
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
	decisionJSON, err := marshalDecisionSnapshot(plan, minPowerW, req.PostEventCooldownSec, req.ForceIncludeAllPairedMiners)
	if err != nil {
		return models.InsertEventParams{}, nil, err
	}

	startState := eventStartState(scope, mode, len(plan.Selected))
	// An all-paired start whose every paired miner is currently unavailable
	// holds in pending: closed-loop full-fleet starts otherwise insert as
	// ACTIVE with started_at stamped, so observeActive would enforce
	// max_duration_seconds before a single Curtail could be dispatched and
	// the forced restore would release the never-dispatched policy rows —
	// dropping durable ownership having curtailed nothing. The reconciler
	// promotes the event to active (stamping started_at) once a target
	// confirms; readiness refresh and admission both run during pending.
	if req.ForceIncludeAllPairedMiners &&
		len(plan.Selected) > 0 &&
		plan.UnavailableTargetCount == len(plan.Selected) {
		startState = models.EventStatePending
	}

	// effective_batch_size is non-null from Start so Stop / restorer /
	// response paths just read the column.
	event := models.InsertEventParams{
		EventUUID:                   uuid.New(),
		OrgID:                       req.OrgID,
		State:                       startState,
		Mode:                        mode,
		Strategy:                    models.StrategyLeastEfficientFirst,
		Level:                       models.LevelFull,
		Priority:                    req.Priority,
		LoopType:                    models.LoopTypeOpen,
		ScopeType:                   scope.Type,
		ScopeJSON:                   scopeJSON,
		ModeParamsJSON:              modeParamsJSON,
		CurtailBatchSize:            req.CurtailBatchSize,
		CurtailBatchIntervalSec:     req.CurtailBatchIntervalSec,
		RestoreBatchSize:            req.RestoreBatchSize,
		RestoreBatchIntervalSec:     req.RestoreBatchIntervalSec,
		MinCurtailedDurationSec:     req.MinCurtailedDurationSec,
		MaxDurationSeconds:          req.MaxDurationSeconds,
		AllowUnbounded:              req.AllowUnbounded,
		IncludeMaintenance:          req.IncludeMaintenance,
		ForceIncludeMaintenance:     req.ForceIncludeMaintenance,
		ForceIncludeAllPairedMiners: req.ForceIncludeAllPairedMiners,
		FacilityFanDeviceIDs:        append([]int64(nil), req.FacilityFanDeviceIDs...),
		ExpectedFacilityFanSites:    cloneInt64Map(req.AuthorizedFanSites),
		FanOffDelaySec:              req.FanOffDelaySec,
		FanRestoreDelaySec:          req.FanRestoreDelaySec,
		DecisionSnapshotJSON:        decisionJSON,
		SourceActorType:             req.SourceActorType,
		SourceActorID:               req.SourceActorID,
		ExternalSource:              req.ExternalSource,
		ExternalReference:           req.ExternalReference,
		IdempotencyKey:              req.IdempotencyKey,
		Reason:                      req.Reason,
		CreatedByUserID:             req.CreatedByUserID,
		EffectiveBatchSize:          plan.EffectiveBatchSize,
	}
	if event.Priority == "" {
		event.Priority = models.PriorityNormal
	}
	if event.ScopeType == "" {
		event.ScopeType = models.ScopeTypeWholeOrg
	}

	if isClosedLoopFullFleetStart(scope, mode) {
		event.LoopType = models.LoopTypeClosed
	}
	if event.State == models.EventStateActive && event.StartedAt == nil {
		now := time.Now().UTC()
		event.StartedAt = &now
	}

	var targets []models.InsertTargetParams
	if !isClosedLoopFullFleetStart(scope, mode) || req.ForceIncludeAllPairedMiners {
		targets = BuildInsertTargetParams(plan.Selected, mode, minPowerW)
	}
	return event, targets, nil
}

func cloneInt64Map(values map[int64]int64) map[int64]int64 {
	if values == nil {
		return nil
	}
	out := make(map[int64]int64, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

// BuildInsertTargetParams converts selected devices into miner target rows.
// Reconciler dynamic admission reuses the same baseline semantics as Start.
func BuildInsertTargetParams(selected []SelectedDevice, mode models.Mode, minPowerW int32) []models.InsertTargetParams {
	targets := make([]models.InsertTargetParams, len(selected))
	for i, sel := range selected {
		var baseline *float64
		if shouldPersistBaselinePowerW(mode, sel.PowerW, minPowerW, sel.HashRateHS > 0) {
			v := sel.PowerW
			baseline = &v
		}
		state := sel.TargetState
		if state == "" {
			state = models.TargetStatePending
		}
		var lastError *string
		if sel.LastError != "" {
			lastError = &sel.LastError
		}
		targets[i] = models.InsertTargetParams{
			DeviceIdentifier: sel.DeviceIdentifier,
			TargetType:       targetTypeMiner,
			State:            state,
			DesiredState:     models.DesiredStateCurtailed,
			BaselinePowerW:   baseline,
			LastError:        lastError,
		}
	}
	return targets
}

// BuildFullFleetAdmissionTargets applies the same full-fleet eligibility and
// baseline policy used by Start, for reconciler closed-loop admission.
func BuildFullFleetAdmissionTargets(
	candidates []*models.Candidate,
	includeMaintenance bool,
	minPowerW int32,
) ([]models.InsertTargetParams, []SkippedDevice) {
	eligible, skipped, _ := classifyCandidates(candidates, classifyOpts{
		IncludeMaintenance: includeMaintenance,
		CandidateMinPowerW: minPowerW,
	})
	plan := BuildPlan(eligible, skipped, minPowerW, modes.FullFleet{})
	return BuildInsertTargetParams(plan.Selected, models.ModeFullFleet, minPowerW), plan.Skipped
}

// shouldPersistBaselinePowerW gates baseline capture. Full-fleet selection
// runs without the dual-signal filter, so a below-floor power reading from a
// hashing miner is suspect (dead power monitor) and the hash-only
// confirm/restore fallback is the better signal. For a non-hashing miner the
// hash-only fallback is provably broken (hash is already 0: curtail confirms
// instantly, restore never confirms), so any positive idle-draw baseline is
// strictly better and the floor does not apply.
func shouldPersistBaselinePowerW(mode models.Mode, powerW float64, minPowerW int32, hashing bool) bool {
	if powerW <= 0 {
		return false
	}
	if mode == models.ModeFullFleet && hashing && powerW < float64(minPowerW) {
		return false
	}
	return true
}

// eventStartState is the state a freshly-built event is inserted with.
// Closed-loop FULL_FLEET starts as an active command policy; the reconciler
// claims per-miner targets only when it is about to dispatch.
func eventStartState(scope Scope, mode models.Mode, targetCount int) models.EventState {
	if isClosedLoopFullFleetStart(scope, mode) {
		return models.EventStateActive
	}
	if mode == models.ModeFullFleet && targetCount == 0 {
		return models.EventStateCompleted
	}
	return models.EventStatePending
}

func isClosedLoopFullFleetStart(scope Scope, mode models.Mode) bool {
	scope = normalizeScope(scope)
	if mode != models.ModeFullFleet {
		return false
	}
	switch scope.Type {
	case models.ScopeTypeWholeOrg, models.ScopeTypeSite, "":
		return true
	case models.ScopeTypeMixed:
		return IsSiteOnlyScope(scope)
	case models.ScopeTypeDeviceSets, models.ScopeTypeDeviceList:
		return false
	default:
		return false
	}
}

// IsSiteOnlyScope reports whether scope targets only one or more sites, with
// no explicit devices or device-set selectors.
func IsSiteOnlyScope(scope Scope) bool {
	scope = normalizeScope(scope)
	return len(scope.SiteIDs) > 0 &&
		len(scope.DeviceIdentifiers) == 0 &&
		len(scope.DeviceSetIDs) == 0
}

// MarshalScopeJSON renders the request scope as the JSONB column value.
// Whole-org stores `{}` (NOT NULL).
func MarshalScopeJSON(s Scope) ([]byte, error) {
	s = normalizeScope(s)
	switch s.Type {
	case models.ScopeTypeWholeOrg, "":
		return []byte("{}"), nil
	case models.ScopeTypeSite:
		b, err := json.Marshal(map[string]int64{
			"site_id": s.SiteID,
		})
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to encode scope: %v", err)
		}
		return b, nil
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
	case models.ScopeTypeMixed:
		b, err := json.Marshal(map[string]any{
			"site_ids":           s.SiteIDs,
			"device_identifiers": s.DeviceIdentifiers,
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
	Force     bool // admin-gated upstream; bypasses min_curtailed_duration_sec and automation demand guard
	// AutomationRestore is set only by the automation executor while handling
	// an ON signal from the owning source.
	AutomationRestore bool
}

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

	return s.store.BeginRestoreTransition(ctx, req.OrgID, req.EventUUID, interfaces.BeginRestoreTransitionParams{
		AutomationDemandGuard: automationDemandGuardForStop(event, req),
	})
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

func automationDemandGuardForStop(event *models.Event, req StopRequest) *interfaces.AutomationDemandGuard {
	if event == nil || req.Force || req.AutomationRestore || !isAutomationOwnedEvent(event) {
		return nil
	}
	return &interfaces.AutomationDemandGuard{
		ExternalReference: event.ExternalReference,
	}
}

func isAutomationOwnedEvent(event *models.Event) bool {
	return event != nil &&
		event.SourceActorType == models.SourceActorAutomation &&
		event.ExternalSource != nil &&
		*event.ExternalSource == automationExternalSource
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

const (
	defaultManualCurtailBatchSizeFloor   int32 = 10
	defaultManualCurtailBatchSizeCeiling int32 = 100
)

// defaultManualCurtailBatchSize preserves the manual-curtail dispatch throttle
// independently from restore controls. A zero selected count still gets the
// floor so empty closed-loop full-fleet watchers do not admit every later
// candidate in one tick.
func defaultManualCurtailBatchSize(selectedCount int32) *int32 {
	batchSize := adaptiveManualCurtailBatchSize(selectedCount)
	if batchSize <= 0 {
		batchSize = defaultManualCurtailBatchSizeFloor
	}
	return &batchSize
}

func adaptiveManualCurtailBatchSize(selectedCount int32) int32 {
	if selectedCount <= 0 {
		return 0
	}
	base := int32(math.Ceil(float64(selectedCount) * 0.01))
	if base < defaultManualCurtailBatchSizeFloor {
		return defaultManualCurtailBatchSizeFloor
	}
	if base > defaultManualCurtailBatchSizeCeiling {
		return defaultManualCurtailBatchSizeCeiling
	}
	return base
}

// ComputeEffectiveBatchSize returns the restore batch size stamped at Start.
// restore_batch_size=0 is explicit immediate restore: claim every currently
// selected target in one wave up to RestoreBatchSizeMax. Positive values are
// the caller's explicit restore wave size.
func ComputeEffectiveBatchSize(restoreBatchSize, nonTerminalCount int32) int32 {
	if restoreBatchSize <= 0 {
		if nonTerminalCount <= 0 {
			return 0
		}
		if nonTerminalCount > RestoreBatchSizeMax {
			return RestoreBatchSizeMax
		}
		return nonTerminalCount
	}
	return restoreBatchSize
}

func cloneInt32Ptr(v *int32) *int32 {
	if v == nil {
		return nil
	}
	c := *v
	return &c
}

// marshalDecisionSnapshot captures the selector outputs for the
// decision_snapshot column (rejection counters, realized vs. requested
// kW, resolved candidate floor).
func marshalDecisionSnapshot(plan *Plan, minPowerW int32, postEventCooldownSec int32, forceIncludeAllPairedMiners bool) ([]byte, error) {
	skipped := make([]map[string]string, len(plan.Skipped))
	for i, s := range plan.Skipped {
		skipped[i] = map[string]string{
			"device_identifier": s.DeviceIdentifier,
			"reason":            string(s.Reason),
		}
	}
	snapshot := map[string]any{
		"candidate_min_power_w":           minPowerW,
		"post_event_cooldown_sec":         postEventCooldownSec,
		"estimated_reduction_kw":          plan.EstimatedReductionKW,
		"estimated_remaining_power_kw":    plan.EstimatedRemainingPowerKW,
		"selected_count":                  len(plan.Selected),
		"policy_target_count":             plan.PolicyTargetCount,
		"unavailable_target_count":        plan.UnavailableTargetCount,
		"force_include_all_paired_miners": forceIncludeAllPairedMiners,
		"skipped":                         skipped,
	}
	b, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf(
			"failed to encode decision_snapshot: %v", err,
		)
	}
	return b, nil
}
