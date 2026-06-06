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
type UpdateCurtailmentTargetStateParams struct {
	State                models.TargetState
	LastDispatchedAt     *time.Time
	LastBatchUUID        *string
	ObservedPowerW       *float64
	ObservedAt           *time.Time
	ConfirmedAt          *time.Time
	RetryCount           *int32
	LastError            *string
	ExpectedEventState   *models.EventState
	ExpectedDesiredState *string
}

// UpsertCurtailmentHeartbeatParams describes the singleton liveness row
// upserted at the end of every successful reconciler tick.
type UpsertCurtailmentHeartbeatParams struct {
	LastTickAt         time.Time
	LastTickUUID       uuid.UUID
	LastTickDurationMS *int32
	ActiveEventCount   int32
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
	ListRecentlyResolvedCurtailedDevices(ctx context.Context, orgID int64, cooldownSec int32) ([]string, error)

	GetEventByUUID(ctx context.Context, orgID int64, eventUUID uuid.UUID) (*models.Event, error)
	GetEventDetailByUUID(ctx context.Context, orgID int64, eventUUID uuid.UUID) (*models.Event, error)

	// GetActiveEvent returns the most-recent non-terminal event for the org,
	// or nil. Multiple non-terminal events can coexist (one per disjoint
	// device scope); ListActiveEvents returns all of them.
	GetActiveEvent(ctx context.Context, orgID int64) (*models.Event, error)

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

	// AdminTerminateEvent forces a non-terminal event to CANCELLED or
	// FAILED and sweeps non-terminal targets to RESTORE_FAILED in one
	// transaction. Idempotent: an already-terminal event in the same
	// target state returns transitioned=false (caller suppresses side
	// effects); a different terminal state surfaces
	// ErrCurtailmentAdminTerminateStateConflict.
	AdminTerminateEvent(ctx context.Context, orgID int64, eventUUID uuid.UUID, targetState models.EventState, reason string) (event *models.Event, transitioned bool, err error)

	ListTargetsByEvent(ctx context.Context, orgID int64, eventUUID uuid.UUID) ([]*models.Target, error)
	ListTargetsByEventPage(ctx context.Context, params ListTargetsByEventPageParams) ([]*models.Target, string, error)
	GetTargetRollupByEvent(ctx context.Context, orgID int64, eventUUID uuid.UUID) (*models.TargetRollup, error)

	// InsertEventWithTargets writes the event + every target row in one
	// transaction. Callers leave CurtailmentEventID zero (store fills it)
	// and pre-validate non-empty / no-duplicate identifiers.
	InsertEventWithTargets(
		ctx context.Context,
		event models.InsertEventParams,
		targets []models.InsertTargetParams,
	) (*models.InsertEventResult, error)

	// Heartbeat singleton row used by liveness alerts.
	GetHeartbeat(ctx context.Context) (*models.Heartbeat, error)

	// ListCandidates returns per-device state for the selector. Nil
	// deviceIdentifiers returns the whole org (callers normalize empty
	// slice → nil). Devices without recent telemetry return nil power /
	// hash; the service treats those as stale.
	ListCandidates(ctx context.Context, orgID int64, deviceIdentifiers []string) ([]*models.Candidate, error)

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
