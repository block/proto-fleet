package interfaces

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
)

// ErrCurtailmentReplayRaceLoss is returned by InsertEventWithTargets when
// a concurrent first-time Start sharing the same idempotency_key or
// (external_source, external_reference) won the partial-unique-index race.
// Callers re-issue the matching lookup to surface the winner's row.
var ErrCurtailmentReplayRaceLoss = errors.New("curtailment event was inserted concurrently by a duplicate-protected channel; replay the persisted winner")

// ErrCurtailmentAdminTerminateStateConflict: the event already sits in a
// different terminal state than the caller requested.
var ErrCurtailmentAdminTerminateStateConflict = errors.New("curtailment event is already terminal in a different state")

// ErrCurtailmentAdminTerminateActiveEvent: a target still has an in-flight
// Curtail (desired_state='curtailed' AND state ∈ dispatching/dispatched/
// confirmed/drifted). Restore-phase Uncurtails do not trip this — they
// carry desired_state='active'. Caller must Stop first.
var ErrCurtailmentAdminTerminateActiveEvent = errors.New("curtailment event has in-flight curtail commands; must be stopped before admin termination")

// ErrCurtailmentEventStateRaceLoss is returned by UpdateOperatorFields,
// UpdateEventState, and UpdateTargetState when the SQL guard matches zero
// rows because the parent event advanced out of the non-terminal window.
// Reconciler skips with a metric; the Update service path returns
// FailedPrecondition.
var ErrCurtailmentEventStateRaceLoss = errors.New("curtailment event state advanced before write")

// UpdateCurtailmentTargetStateParams: optional patch fields. Nil pointers
// leave the column unchanged via COALESCE.
//
// ExpectedEventState scopes the write to the reconciler phase and locks the
// parent event row before updating the target. ExpectedDesiredState scopes the
// write to the dispatch direction ('curtailed' on Curtail-phase writes,
// 'active' on Restore-phase) so a concurrent Stop that flipped desired_state
// race-loses instead of being clobbered.
//
// ExpectedState and ExpectedDispatchBatchUUID are the confirmation fast-path
// race guards. When set, the target's current state must equal ExpectedState
// (e.g. 'dispatched') and the applicable phase batch UUID — curtail_batch_uuid
// when desired_state='curtailed', restore_batch_uuid when 'active' — must equal
// ExpectedDispatchBatchUUID. Together they make concurrent confirmation writes
// single-winner: a duplicate promotion (state already advanced) or a
// timeout/redispatch that stamped a new batch UUID (ABA) matches zero rows and
// maps to ErrCurtailmentEventStateRaceLoss. Nil leaves the guard off, so the
// existing full-tick writes are unaffected.
type UpdateCurtailmentTargetStateParams struct {
	State                     models.TargetState
	LastDispatchedAt          *time.Time
	LastBatchUUID             *string
	ObservedPowerW            *float64
	ObservedAt                *time.Time
	ConfirmedAt               *time.Time
	RetryCount                *int32
	LastError                 *string
	ExpectedEventState        *models.EventState
	ExpectedDesiredState      *string
	ExpectedState             *models.TargetState
	ExpectedDispatchBatchUUID *string
}

// AllPairedReadinessUpdate is one pending/unavailable readiness flip in the
// bulk all-paired refresh. Restore-failed topology obligations may first park
// as unavailable so a later commandability transition can requeue them.
// ExpectedState and ExpectedDesiredState prevent a decision made from a stale
// target snapshot from overwriting a concurrent state or phase transition.
// Reason is the unavailable reason; empty clears last_error. BaselinePowerW,
// when set on a curtail promotion, backfills a NULL baseline from current
// telemetry; the SQL never overwrites an existing baseline.
type AllPairedReadinessUpdate struct {
	DeviceIdentifier     string
	ExpectedState        models.TargetState
	ExpectedDesiredState string
	State                models.TargetState
	Reason               string
	BaselinePowerW       *float64
}

// ConfirmationBatchSize bounds both one fast-path eligibility page and each
// guarded bulk write so a single pulse cannot monopolize sampler or DB
// capacity.
const ConfirmationBatchSize = 500

// ConfirmationPageCursor is the stable keyset position for the global
// eligibility scan. The zero value starts a new sweep.
type ConfirmationPageCursor struct {
	AfterEventID          int64
	AfterDeviceIdentifier string
}

// ConfirmationUpdate is one positive fast-path promotion submitted to the
// guarded bulk confirmation write. The store revalidates event phase, target
// state/direction/batch, and the exact live device row before applying it.
type ConfirmationUpdate struct {
	DeviceDatabaseID int64
	DeviceIdentifier string
	Phase            models.TargetPhase
	BatchUUID        string
	ObservedPowerW   *float64
	ObservedAt       time.Time
	ConfirmedAt      time.Time
}

type ConfirmationBulkResult struct {
	AppliedCount            int
	SampleDeviceIdentifiers []string
}

// CurtailmentConfirmationStore is the store surface required only by the
// optional confirmation fast path. Keeping it separate from CurtailmentStore
// avoids expanding handler/test doubles that can never run the pulse.
type CurtailmentConfirmationStore interface {
	// ListEligibleConfirmationTargets returns at most ConfirmationBatchSize
	// phase-valid dispatched targets across all orgs after the exclusive
	// cursor: curtail work under pending/active events and restore work under
	// restoring events. The cursor's zero value starts a new global sweep.
	ListEligibleConfirmationTargets(
		ctx context.Context,
		cursor ConfirmationPageCursor,
	) ([]models.ConfirmationTarget, error)

	// BulkConfirmTargets applies positive confirmations for one event in one
	// guarded statement and returns only device identifiers that won.
	BulkConfirmTargets(
		ctx context.Context,
		eventID int64,
		expectedEventState models.EventState,
		updates []ConfirmationUpdate,
	) (ConfirmationBulkResult, error)
}

// UpsertCurtailmentHeartbeatParams describes the singleton liveness row
// upserted at the end of every successful reconciler tick.
type UpsertCurtailmentHeartbeatParams struct {
	LastTickAt         time.Time
	LastTickUUID       uuid.UUID
	LastTickDurationMS *int32
	ActiveEventCount   int32
}

type BeginRestoreTransitionParams struct {
	AutomationDemandGuard *AutomationDemandGuard
	// KnownUnsentDeviceIdentifiers identifies DISPATCHING rows for which the
	// caller knows the physical command boundary was never crossed.
	KnownUnsentDeviceIdentifiers []string
}

type AutomationDemandGuard struct {
	ExternalReference *string
}

// ListEventsParams configures the cursor-paginated history query.
// PageToken empty = first page; StateFilters empty = all states.
// PageSize <=0 falls back to the store's default page size.
type ListEventsParams struct {
	OrgID        int64
	PageSize     int32
	PageToken    string
	StateFilters []models.EventState
}

// ListTargetsByEventPageParams configures cursor-paginated target detail for
// one curtailment event. PageToken empty = first page.
type ListTargetsByEventPageParams struct {
	OrgID     int64
	EventUUID uuid.UUID
	PageSize  int32
	PageToken string
}

// ResponseProfileStore is the persistence boundary for reusable curtailment
// response profiles. Automation uses these later; CRUD is org-scoped.
type ResponseProfileStore interface {
	ListResponseProfiles(ctx context.Context, orgID int64) ([]*models.ResponseProfile, error)
	GetResponseProfile(ctx context.Context, orgID, profileID int64) (*models.ResponseProfile, error)
	ListCandidates(ctx context.Context, params ListCandidatesParams) ([]*models.Candidate, error)
	ListResponseProfileDeviceSites(ctx context.Context, orgID int64, deviceIdentifiers []string) (map[string]*int64, error)
	ListResponseProfileInfrastructureDevices(ctx context.Context, orgID int64, infrastructureDeviceIDs []int64) (map[int64]models.ResponseProfileInfrastructureDevice, error)
	CreateResponseProfile(ctx context.Context, profile models.ResponseProfile, expectedDeviceSites map[string]*int64, expectedInfrastructureDevices map[int64]models.ResponseProfileInfrastructureDevice) (*models.ResponseProfile, error)
	UpdateResponseProfile(ctx context.Context, profile models.ResponseProfile, expectedDeviceSites map[string]*int64, expectedInfrastructureDevices map[int64]models.ResponseProfileInfrastructureDevice, expectedSiteID *int64, expectedScopeJSON []byte, expectedFacilityFanSettings models.ResponseProfileFanSettings) (*models.ResponseProfile, error)
	DeleteResponseProfile(ctx context.Context, orgID, profileID int64, expectedSiteID *int64, expectedScopeJSON, expectedAuthorizationEnvelopeJSON []byte, expectedFacilityFanSettings models.ResponseProfileFanSettings) error
	CountAutomationRulesByResponseProfile(ctx context.Context, orgID, profileID int64) (int64, error)
	SiteBelongsToOrg(ctx context.Context, orgID, siteID int64) (bool, error)
}

// AutomationStore is the persistence boundary for curtailment automation rule
// CRUD and executor state.
//
//nolint:interfacebloat // Rule CRUD and durable executor state are one transactional persistence boundary.
type AutomationStore interface {
	ListAutomationRules(ctx context.Context, orgID int64) ([]*models.AutomationRule, error)
	GetAutomationRule(ctx context.Context, orgID, ruleID int64) (*models.AutomationRule, error)
	ListEnabledAutomationRulesByMQTTSource(ctx context.Context, mqttSourceID int64) ([]*models.AutomationRule, error)
	CreateAutomationRule(ctx context.Context, rule models.AutomationRule, expectedFanSettings models.ResponseProfileFanSettings) (*models.AutomationRule, error)
	UpdateAutomationRule(ctx context.Context, rule models.AutomationRule, expectedFanSettings models.ResponseProfileFanSettings) (*models.AutomationRule, error)
	SetAutomationRuleEnabled(ctx context.Context, orgID, ruleID int64, enabled bool, expectedFanSettings models.ResponseProfileFanSettings) (*models.AutomationRule, error)
	DeleteAutomationRule(ctx context.Context, orgID, ruleID int64) error
	CountAutomationRulesByMQTTSource(ctx context.Context, orgID, sourceID int64) (int64, error)
	RecordAutomationSignal(ctx context.Context, ruleID int64, signal models.AutomationSignal, at time.Time) error
	// SetAutomationActiveEvent records the rule's live event; it fails if the
	// rule is disabled or no longer bound to mqttSourceID (the source whose
	// signal started the event), so a mid-signal re-point cannot mis-attribute it.
	SetAutomationActiveEvent(ctx context.Context, ruleID, mqttSourceID int64, eventUUID uuid.UUID, at time.Time) error
	ClearAutomationActiveEvent(ctx context.Context, ruleID int64, at time.Time) error
	RecordAutomationRestoreStarted(ctx context.Context, ruleID int64, at time.Time) error
	RecordAutomationExecutionError(ctx context.Context, ruleID int64, message string, at time.Time) error
}

const CurtailmentResolvedMinerMax = 10000

// ListCandidatesParams scopes selector candidate reads. Empty selector slices
// mean whole-org. Curtailment validates that no more than one selector type is
// set before crossing the store boundary.
type ListCandidatesParams struct {
	OrgID int64
	// ResultLimit is zero for no limit; selector entry points use max+1 so
	// they can distinguish an exact-bound result from overflow.
	ResultLimit       int32
	DeviceIdentifiers []string
	SiteIDs           []int64
	BuildingIDs       []int64
	RackIDs           []int64
	GroupIDs          []int64
}

type ListRecentlyResolvedCurtailedDevicesParams struct {
	OrgID             int64
	ExcludeEventID    int64
	CooldownSec       int32
	DeviceIdentifiers []string
	SiteIDs           []int64
}

// CurtailmentTopologyScopeCoverage is the authorization envelope derived from
// a validated topology selector. SiteIDs is the combined compatibility view;
// the split fields preserve why each site is covered. RequireOrgWide is set
// when any selected resource or member is unassigned, or when a group has no
// members and therefore unbounded future coverage.
type CurtailmentTopologyScopeCoverage struct {
	SiteIDs                 []int64
	SelectedResourceSiteIDs []int64
	CurrentMemberSiteIDs    []int64
	RequireOrgWide          bool
}

// CurtailmentTopologyScopeStore validates topology selectors and derives their
// current authorization coverage. It is separate from CurtailmentStore so
// non-topology test stores do not need to implement an unused capability.
type CurtailmentTopologyScopeStore interface {
	ResolveCurtailmentTopologyScope(
		ctx context.Context,
		params ListCandidatesParams,
	) (CurtailmentTopologyScopeCoverage, error)
}

// CurtailmentTopologyTargetRestoreStore moves targets that left a live
// topology selector into the existing per-target restore state machine while
// leaving the parent watcher active.
type CurtailmentTopologyTargetRestoreStore interface {
	BeginCurtailmentTopologyTargetRestore(
		ctx context.Context,
		event *models.Event,
		deviceIdentifiers []string,
	) (int64, error)
}

// CurtailmentTopologyDispatchSnapshot is one database snapshot of both the
// selector's authorization coverage and the subset of the dispatch batch that
// is still a member. Keeping these reads together prevents a placement change
// from being authorized against coverage from a different point in time.
type CurtailmentTopologyDispatchSnapshot struct {
	Coverage                        CurtailmentTopologyScopeCoverage
	DispatchMemberDeviceIdentifiers []string
}

// CurtailmentTopologyDispatchStore performs the live topology check used at
// the physical command boundary. It is separate from the start-time topology
// resolver because only the reconciler needs batch membership in the result.
type CurtailmentTopologyDispatchStore interface {
	ResolveCurtailmentTopologyDispatch(
		ctx context.Context,
		params ListCandidatesParams,
		dispatchDeviceIdentifiers []string,
	) (CurtailmentTopologyDispatchSnapshot, error)
}

// CurtailmentTopologyDispatchFenceSnapshot is the event and topology state
// protected by the dispatch fence for the duration of its callback.
type CurtailmentTopologyDispatchFenceSnapshot struct {
	Event    *models.Event
	Topology CurtailmentTopologyDispatchSnapshot
}

// CurtailmentTopologyDispatchFenceStore serializes event transitions,
// topology mutations, and creator permission revocations through the physical
// Curtail command callback. Implementations must keep referenced user/device
// rows compatible with the foreign-key locks acquired by command enqueueing.
type CurtailmentTopologyDispatchFenceStore interface {
	WithCurtailmentTopologyDispatchFence(
		ctx context.Context,
		event *models.Event,
		params ListCandidatesParams,
		dispatchDeviceIdentifiers []string,
		command func(CurtailmentTopologyDispatchFenceSnapshot) error,
	) error
}

// UpdateOperatorFieldsParams carries the optional patch fields for a
// partial event update. nil values preserve the column via COALESCE.
// effective_batch_size is not on this surface — recomputing mid-event
// would race an in-flight restore claim.
type UpdateOperatorFieldsParams struct {
	Reason                  *string
	RestoreBatchSize        *int32
	RestoreBatchIntervalSec *int32
	MaxDurationSeconds      *int32
}

// UpdateCurtailmentFanStateParams persists one reconciler or operator-recovery
// fan attempt. Nil timestamps preserve the corresponding send stamp;
// ClearFanAirflowReopenedAt resets the active marker after fans turn off again.
// LastError nil clears a previous failure after a successful re-assertion.
type UpdateCurtailmentFanStateParams struct {
	ExpectedEventState models.EventState
	FanOffSentAt       *time.Time
	FanOnSentAt        *time.Time
	// FanAirflowReopenedAt preserves the first reopen attempt for alert timing.
	// OnSuccess replaces it only when the hardware command succeeds, so the
	// cooling delay begins from confirmed airflow rather than a failed attempt.
	FanAirflowReopenedAt          *time.Time
	FanAirflowReopenedAtOnSuccess *time.Time
	ClearFanAirflowReopenedAt     bool
	LastError                     *string
}

type CurtailmentFanStateStore interface {
	CommandFanState(
		ctx context.Context,
		eventID int64,
		params UpdateCurtailmentFanStateParams,
		command func(context.Context) *string,
	) (*string, error)
}

// CurtailmentTerminalFanRecoveryStore serializes an operator's terminal-event
// fan recovery against new Start claims. The implementation holds the same
// per-fan claim locks used by Start while it checks for newer owners, invokes
// command, and persists the resulting error state.
type CurtailmentTerminalFanRecoveryStore interface {
	RecoverTerminalFanState(
		ctx context.Context,
		eventID, orgID int64,
		facilityFanDeviceIDs []int64,
		facilityFanSiteIDs []int64,
		params UpdateCurtailmentFanStateParams,
		command func(context.Context) *string,
	) error
}

// CurtailmentAdminTerminateFanRecoveryStore serializes an operator admin
// termination against fan recovery and concurrent lifecycle transitions. The
// implementation holds the event lock while deciding whether recovery is
// required, commanding fans ON, persisting the fan result, and terminalizing.
type CurtailmentAdminTerminateFanRecoveryStore interface {
	AdminTerminateEventWithFanRecovery(
		ctx context.Context,
		orgID int64,
		eventUUID uuid.UUID,
		targetState models.EventState,
		reason string,
		command func(context.Context, *models.Event) *string,
	) (event *models.Event, transitioned bool, err error)
}

// CurtailmentForceReleaseFanRecoveryStore holds the active event's fan claim
// locks across terminalization and its authoritative ON command. This keeps a
// concurrent Start from claiming physically-off fans in the gap between those
// two operations.
type CurtailmentForceReleaseFanRecoveryStore interface {
	ForceReleaseEventWithFanRecovery(
		ctx context.Context,
		orgID int64,
		eventUUID uuid.UUID,
		reason string,
		eventID int64,
		facilityFanDeviceIDs []int64,
		facilityFanSiteIDs []int64,
		params UpdateCurtailmentFanStateParams,
		command func(context.Context) *string,
	) (ForceReleaseEventResult, error)
}

type ForceReleaseEventResult struct {
	Event              *models.Event
	SweptTargets       int64
	OwnershipReleased  bool
	AutomationDisabled bool
}

// CurtailmentStore is the persistence boundary for the curtailment domain.
// All methods are org-scoped except where noted.
//
//nolint:interfacebloat // Splitting the event/target/heartbeat lifecycle would force callers to take 3+ deps for one logical domain.
type CurtailmentStore interface {
	// GetOrgConfig: always returns a row for any valid org_id. Migration
	// seeds one per existing org; SQL store lazily upserts on miss for
	// orgs created post-migration. NotFound only on invalid org_id (FK).
	GetOrgConfig(ctx context.Context, orgID int64) (*models.OrgConfig, error)

	// Selector exclusion sets — org-scoped device IDs subtracted from candidates.
	ListActiveCurtailedDevices(ctx context.Context, orgID int64) ([]string, error)
	ListActiveCurtailmentTargetDevices(ctx context.Context, orgID int64) ([]string, error)
	ListRecentlyResolvedCurtailedDevices(ctx context.Context, params ListRecentlyResolvedCurtailedDevicesParams) ([]string, error)
	SiteBelongsToOrg(ctx context.Context, orgID, siteID int64) (bool, error)

	GetEventByUUID(ctx context.Context, orgID int64, eventUUID uuid.UUID) (*models.Event, error)
	GetEventDetailByUUID(ctx context.Context, orgID int64, eventUUID uuid.UUID) (*models.Event, error)

	// ListActiveEvents returns every non-terminal event for the org,
	// most-recent first.
	ListActiveEvents(ctx context.Context, orgID int64) ([]*models.Event, error)

	// GetEventByIdempotencyKey returns the event a prior Start persisted
	// against (org_id, idempotency_key), or nil when no row matches.
	// Powers the webhook-replay path.
	GetEventByIdempotencyKey(ctx context.Context, orgID int64, idempotencyKey string) (*models.Event, error)

	// GetEventByExternalReference returns the event a prior Start persisted
	// against (org_id, external_source, external_reference), or nil.
	GetEventByExternalReference(ctx context.Context, orgID int64, externalSource, externalReference string) (*models.Event, error)

	// ListEvents returns cursor-paginated history (newest-first).
	// PageToken empty = first page; returned cursor empty = end.
	ListEvents(ctx context.Context, params ListEventsParams) ([]*models.Event, string, error)

	// UpdateOperatorFields patches the operator-safe fields on a pending /
	// active event. The SQL re-asserts the state predicate, so a concurrent
	// advance surfaces as ErrCurtailmentEventStateRaceLoss.
	UpdateOperatorFields(ctx context.Context, eventID, orgID int64, params UpdateOperatorFieldsParams) (*models.Event, error)

	// RecordCurtailPendingDispatch durably reserves the pacing slot for a fresh
	// pending curtail wave before its command is sent. Recovery without durable
	// evidence of a prior enqueue also reserves a slot; ordinary retries do not.
	// A reservation may consume the interval even if the later enqueue fails.
	RecordCurtailPendingDispatch(ctx context.Context, eventID int64, expectedState models.EventState, dispatchedAt time.Time) error

	// AdminTerminateEvent forces a non-terminal event to CANCELLED or
	// FAILED and sweeps non-terminal targets to RESTORE_FAILED in one
	// transaction. Idempotent: an already-terminal event in the same
	// target state returns transitioned=false (caller suppresses side
	// effects); a different terminal state surfaces
	// ErrCurtailmentAdminTerminateStateConflict.
	AdminTerminateEvent(ctx context.Context, orgID int64, eventUUID uuid.UUID, targetState models.EventState, reason string) (event *models.Event, transitioned bool, err error)

	// ForceReleaseEvent immediately moves any existing event to CANCELLED and
	// sweeps non-terminal targets to RELEASED in one transaction. It is a
	// last-resort ownership release path, not graceful restore.
	ForceReleaseEvent(ctx context.Context, orgID int64, eventUUID uuid.UUID, reason string) (ForceReleaseEventResult, error)

	ListTargetsByEvent(ctx context.Context, orgID int64, eventUUID uuid.UUID) ([]*models.Target, error)
	ListTargetsByEventPage(ctx context.Context, params ListTargetsByEventPageParams) ([]*models.Target, string, error)
	// ListTargetSiteCoverageByEvent returns distinct mapped target sites and
	// whether site coverage is complete. Events with zero target rows are
	// complete; callers can then derive any required site context from the
	// persisted event scope.
	ListTargetSiteCoverageByEvent(ctx context.Context, orgID int64, eventUUID uuid.UUID) (models.TargetSiteCoverage, error)
	ListTargetSiteCoverageByEvents(ctx context.Context, orgID int64, eventUUIDs []uuid.UUID) (map[uuid.UUID]models.TargetSiteCoverage, error)
	GetTargetRollupByEvent(ctx context.Context, orgID int64, eventUUID uuid.UUID) (*models.TargetRollup, error)

	// InsertEventWithTargets writes the event + every target row in one
	// transaction. Callers leave CurtailmentEventID zero (store fills it)
	// and pre-validate non-empty / no-duplicate identifiers.
	InsertEventWithTargets(
		ctx context.Context,
		event models.InsertEventParams,
		targets []models.InsertTargetParams,
	) (*models.InsertEventResult, error)

	// ClaimClosedLoopFullFleetTargets inserts missing closed-loop FULL_FLEET
	// targets as DISPATCHING while the parent event is still pending/active.
	// Existing same-event rows and cross-event conflicts are skipped so
	// reconciliation can retry later.
	ClaimClosedLoopFullFleetTargets(
		ctx context.Context,
		eventID int64,
		orgID int64,
		cooldownSec int32,
		targets []models.InsertTargetParams,
	) ([]*models.Target, error)

	// ClaimAllPairedPolicyTargets inserts or reopens durable all-paired
	// FULL_FLEET policy targets in their computed state. Unlike closed-loop
	// dispatch claims, this does not pre-claim rows as DISPATCHING. It skips
	// earlier reservations before selecting at most maxTargets, so a reserved
	// prefix cannot starve the bounded admission batch.
	ClaimAllPairedPolicyTargets(
		ctx context.Context,
		eventID int64,
		orgID int64,
		maxTargets int,
		targets []models.InsertTargetParams,
	) (int64, error)

	// BulkRefreshAllPairedTargetReadiness applies batched readiness flips to
	// all-paired curtail rows and topology restore obligations. Rows whose
	// state or desired_state advanced concurrently — and every row when the
	// parent event left expectedEventState — are skipped, not clobbered; the
	// reconciler re-reads them next tick. Returns the device identifiers of
	// the rows actually updated so callers mirror only applied flips.
	BulkRefreshAllPairedTargetReadiness(
		ctx context.Context,
		eventID int64,
		orgID int64,
		expectedEventState models.EventState,
		updates []AllPairedReadinessUpdate,
	) ([]string, error)

	// Heartbeat singleton row used by liveness alerts.
	GetHeartbeat(ctx context.Context) (*models.Heartbeat, error)

	// ListCandidates returns per-device state for the selector. Nil
	// deviceIdentifiers returns the whole org (callers normalize empty
	// slice → nil). SiteID restricts candidates to one live site in the
	// org. Devices without recent telemetry return nil power / hash; the
	// service treats those as stale.
	ListCandidates(ctx context.Context, params ListCandidatesParams) ([]*models.Candidate, error)

	// ListNonTerminalEvents returns pending/active/restoring events across
	// all orgs. Reconciler-only — MUST NOT be exposed through any RPC handler.
	ListNonTerminalEvents(ctx context.Context) ([]*models.Event, error)

	// UpdateEventState transitions an event row from expectedState. Nil
	// startedAt/endedAt preserves the column. Returns
	// ErrCurtailmentEventStateRaceLoss if the row advanced out of the expected
	// non-terminal phase.
	UpdateEventState(ctx context.Context, eventID int64, expectedState models.EventState, state models.EventState, startedAt *time.Time, endedAt *time.Time) error

	// UpdateTargetState patches the (eventID, deviceIdentifier) row.
	// Non-state fields use COALESCE: nil preserves the existing column.
	UpdateTargetState(ctx context.Context, eventID int64, deviceIdentifier string, params UpdateCurtailmentTargetStateParams) error

	// BumpTargetRetry increments retry_count without touching state or
	// last_error. Fallback for recordDispatchFailure when the rich
	// UpdateTargetState fails non-race-loss. Returns
	// ErrCurtailmentEventStateRaceLoss on terminal parent.
	BumpTargetRetry(ctx context.Context, eventID int64, deviceIdentifier string) error

	// UpsertHeartbeat overwrites the singleton row at id=1.
	UpsertHeartbeat(ctx context.Context, params UpsertCurtailmentHeartbeatParams) error

	// BeginRestoreTransition flips pending/active → restoring and resets
	// every non-terminal target (desired_state='active', state='pending',
	// cleared cursors) in one transaction. Idempotent on already-restoring
	// events; terminal events return FailedPrecondition.
	BeginRestoreTransition(
		ctx context.Context,
		orgID int64,
		eventUUID uuid.UUID,
		params BeginRestoreTransitionParams,
	) (*models.Event, error)

	// BeginRecurtailTransition flips a restoring event back to pending and resets
	// restore targets for Curtail dispatch. Target overlap rolls back and returns
	// AlreadyExists.
	BeginRecurtailTransition(
		ctx context.Context,
		orgID int64,
		eventUUID uuid.UUID,
	) (*models.Event, error)
}
