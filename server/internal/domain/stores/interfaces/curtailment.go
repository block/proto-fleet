package interfaces

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
)

// ErrCurtailmentNonTerminalEventExists is returned by InsertEventWithTargets
// when the per-org partial unique index rejects an insert because another
// non-terminal event already exists for the same org. Callers map this to
// AlreadyExists and surface the existing event_uuid via GetActiveEvent.
var ErrCurtailmentNonTerminalEventExists = errors.New("non-terminal curtailment event already exists for this organization")

// ErrCurtailmentReplayRaceLoss is returned by InsertEventWithTargets when a
// concurrent first-time Start sharing the same idempotency key or
// (external_source, external_reference) tuple won the partial-unique-index
// race. Callers re-issue the matching lookup (GetEventByIdempotencyKey or
// GetEventByExternalReference) to fetch the winner's row and surface the
// same replay response a non-racing duplicate would have produced. The
// sentinel does not distinguish which channel raced — the Service.Start
// replay path tries both lookups in their canonical precedence order.
var ErrCurtailmentReplayRaceLoss = errors.New("curtailment event was inserted concurrently by a duplicate-protected channel; replay the persisted winner")

// ErrCurtailmentAdminTerminateStateConflict is returned by AdminTerminateEvent
// when the event already sits in a terminal state different from the one
// the caller requested. The service maps this to FailedPrecondition with a
// message that names the existing terminal state.
var ErrCurtailmentAdminTerminateStateConflict = errors.New("curtailment event is already terminal in a different state")

// ErrCurtailmentAdminTerminateActiveEvent is returned by AdminTerminateEvent
// when any target on the event has an in-flight Curtail command —
// state ∈ {dispatching, dispatched, confirmed, drifted} AND
// desired_state = 'curtailed'. Covers ACTIVE events (which always have
// CONFIRMED targets) and PENDING events whose tick already dispatched some
// commands. The desired_state scope means RESTORING events with in-flight
// Uncurtails do *not* trip this sentinel (those carry desired_state='active').
// Callers must Stop first so compensating Uncurtail commands fire instead
// of leaving miners curtailed.
var ErrCurtailmentAdminTerminateActiveEvent = errors.New("curtailment event has in-flight curtail commands; must be stopped before admin termination")

// ErrCurtailmentEventStateRaceLoss is returned by UpdateOperatorFields,
// UpdateEventState, and UpdateTargetState when the row's parent event state
// advanced out of the caller-visible non-terminal window between the
// caller's snapshot and the UPDATE. The SQL guard matches zero rows; the
// store maps that to this sentinel so callers can route by context: the
// reconciler logs + metrics the skip without treating it as Internal, the
// Update service path returns FailedPrecondition.
var ErrCurtailmentEventStateRaceLoss = errors.New("curtailment event state advanced before write")

// UpdateCurtailmentTargetStateParams: optional patch fields. Nil pointers
// leave the column unchanged via COALESCE in the SQL update.
//
// ExpectedDesiredState narrows the write to a target whose current
// desired_state still matches the caller's dispatch direction. Set this on
// post-cmd writes ('curtailed' for Curtail-phase, 'active' for
// Restore-phase) so a concurrent Stop that flipped desired_state to
// 'active' makes the Curtail-phase write race-lose instead of clobbering
// Stop's reset and stranding the miner curtailed. Leave nil on writes
// that legitimately apply across phases (confirmation, error bookkeeping).
type UpdateCurtailmentTargetStateParams struct {
	State                models.TargetState
	LastDispatchedAt     *time.Time
	LastBatchUUID        *string
	ObservedPowerW       *float64
	ObservedAt           *time.Time
	ConfirmedAt          *time.Time
	RetryCount           *int32
	LastError            *string
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

// ListEventsParams configures the org-scoped cursor-paginated history
// query. PageToken is empty for the first page; subsequent pages reuse
// the next-page token from the previous response. StateFilter is empty
// for "all states" or one of the canonical EventState values. PageSize
// is clamped at the service layer; the store treats <=0 as the default
// page size and applies its own upper cap as defense in depth.
type ListEventsParams struct {
	OrgID       int64
	PageSize    int32
	PageToken   string
	StateFilter models.EventState
}

// UpdateOperatorFieldsParams carries the optional patch fields for a
// partial event update. nil values preserve the existing column via
// COALESCE on the persistence side. effective_batch_size is deliberately
// not on this surface — recomputing it mid-event would race against an
// in-flight restore claim and v1 ships the simpler "stamped at Start"
// semantics. Operators who need a different batch size cancel and
// restart.
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
	GetActiveEvent(ctx context.Context, orgID int64) (*models.Event, error)

	// GetEventByIdempotencyKey returns the event a prior Start persisted
	// against (org_id, idempotency_key) — or nil when no row matches. The
	// service uses this for webhook-style replay: a re-issued call with
	// the same key returns the original event instead of double-inserting.
	GetEventByIdempotencyKey(ctx context.Context, orgID int64, idempotencyKey string) (*models.Event, error)

	// GetEventByExternalReference returns the event a prior Start persisted
	// against (org_id, external_source, external_reference) — or nil when
	// no row matches. Paired with GetEventByIdempotencyKey for the two
	// webhook-replay channels.
	GetEventByExternalReference(ctx context.Context, orgID int64, externalSource, externalReference string) (*models.Event, error)

	// ListEvents returns the cursor-paginated history for an org. The cursor
	// is an opaque token issued by an earlier call (empty for the first
	// page); the next-page cursor is returned alongside the slice and is
	// empty when no further pages remain. stateFilter is empty for "all
	// states" or one of the canonical EventState values. Results are ordered
	// newest-first by internal id.
	ListEvents(ctx context.Context, params ListEventsParams) ([]*models.Event, string, error)

	// UpdateOperatorFields patches the operator-safe fields of a curtailment
	// event. Caller has already validated org ownership + state ∈ {pending,
	// active}; the SQL re-asserts the state predicate as defense in depth,
	// so a state advance between the pre-read and the UPDATE surfaces as
	// ErrCurtailmentEventStateRaceLoss. Returns the updated row.
	UpdateOperatorFields(ctx context.Context, eventID, orgID int64, params UpdateOperatorFieldsParams) (*models.Event, error)

	// AdminTerminateEvent forces a non-terminal event to the operator-chosen
	// terminal state (CANCELLED or FAILED) and sweeps every non-terminal
	// target to RESTORE_FAILED in the same transaction. Returns the
	// updated event row. Idempotent: a re-issue against an already-terminal
	// event in the same target state echoes the row. A different terminal
	// state on the existing row surfaces ErrCurtailmentAdminTerminateStateConflict.
	// AdminTerminateEvent returns (event, transitioned, error). transitioned
	// is false when the call was an idempotent echo of an event already in
	// the requested terminal state — in that case no UPDATE ran, no targets
	// were swept, and the caller should suppress side effects like audit
	// emission. transitioned is true when the call performed a real
	// state transition + target sweep.
	AdminTerminateEvent(ctx context.Context, orgID int64, eventUUID uuid.UUID, targetState models.EventState, reason string) (event *models.Event, transitioned bool, err error)

	ListTargetsByEvent(ctx context.Context, orgID int64, eventUUID uuid.UUID) ([]*models.Target, error)

	// InsertEventWithTargets writes the event row + every target row in one
	// transaction. The store fills each target's CurtailmentEventID; callers
	// leave that field zero and pre-validate the params shape (non-empty
	// targets, no duplicate device_identifiers).
	InsertEventWithTargets(
		ctx context.Context,
		event models.InsertEventParams,
		targets []models.InsertTargetParams,
	) (*models.InsertEventResult, error)

	// Heartbeat singleton row used by liveness alerts.
	GetHeartbeat(ctx context.Context) (*models.Heartbeat, error)

	// ListCandidates returns per-device state for the selector. Org-scoped;
	// deviceIdentifiers narrows the result, nil returns the whole org (callers
	// must normalize empty-slice to nil). Order is deterministic. LEFT-JOINs
	// telemetry: devices without recent samples come back with nil
	// PowerW/HashRateHS, which the service treats as stale.
	ListCandidates(ctx context.Context, orgID int64, deviceIdentifiers []string) ([]*models.Candidate, error)

	// ListNonTerminalEvents returns pending/active/restoring events across
	// all orgs. Reconciler-only — MUST NOT be exposed through any RPC handler.
	ListNonTerminalEvents(ctx context.Context) ([]*models.Event, error)

	// UpdateEventState transitions an event row. nil startedAt/endedAt
	// leaves the column unchanged; non-nil overwrites. Returns
	// ErrCurtailmentEventStateRaceLoss when the row advanced out of
	// {pending, active, restoring} between the caller's snapshot and the
	// UPDATE; callers (the reconciler) treat that as a non-fatal race signal.
	UpdateEventState(ctx context.Context, eventID int64, state models.EventState, startedAt *time.Time, endedAt *time.Time) error

	// UpdateTargetState patches the (eventID, deviceIdentifier) row.
	// Non-state fields use COALESCE: nil preserves the existing column.
	UpdateTargetState(ctx context.Context, eventID int64, deviceIdentifier string, params UpdateCurtailmentTargetStateParams) error

	// UpsertHeartbeat overwrites the singleton row at id=1. Migration seeds
	// the row; upsert is robust against accidental deletion.
	UpsertHeartbeat(ctx context.Context, params UpsertCurtailmentHeartbeatParams) error

	// BeginRestoreTransition flips a non-terminal event from pending/active to
	// restoring and resets every non-terminal target (desired_state='active',
	// state='pending', cleared phase-local cursors) in one transaction.
	// effective_batch_size was stamped at Start; this call does not touch it.
	// Idempotent: an already-restoring event returns the current row without
	// writing. Terminal events return FailedPrecondition; cross-org lookups
	// return NotFound.
	BeginRestoreTransition(
		ctx context.Context,
		orgID int64,
		eventUUID uuid.UUID,
	) (*models.Event, error)
}
