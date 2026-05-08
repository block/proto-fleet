package interfaces

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
)

// UpdateCurtailmentTargetStateParams gathers the optional fields the
// reconciler may patch when transitioning a target. Nil pointers leave the
// underlying column unchanged via COALESCE in the SQL update.
type UpdateCurtailmentTargetStateParams struct {
	State            models.TargetState
	LastDispatchedAt *time.Time
	LastBatchUUID    *string
	ObservedPowerW   *float64
	ObservedAt       *time.Time
	ConfirmedAt      *time.Time
	RetryCount       *int32
	LastError        *string
}

// UpsertCurtailmentHeartbeatParams describes the singleton liveness row the
// reconciler upserts at the end of every successful tick.
type UpsertCurtailmentHeartbeatParams struct {
	LastTickAt         time.Time
	LastTickUUID       uuid.UUID
	LastTickDurationMS *int32
	ActiveEventCount   int32
}

// CurtailmentStore is the persistence boundary for the curtailment domain.
// All methods are org-scoped; cross-org reads must explicitly request a
// broader scope (none exist in v1).
//
//nolint:interfacebloat // The curtailment store covers the full event/target/heartbeat lifecycle in one boundary; splitting it would force the service and reconciler to take 3+ store deps for code that is logically a single domain.
type CurtailmentStore interface {
	// Org config — read at handler entry to resolve max-duration default,
	// candidate-power floor, and the cooldown window. Always returns a row
	// for any valid org_id: the migration seeds one per existing org, and
	// the SQL store lazily inserts a defaults row on miss for orgs created
	// post-migration. NotFound is reserved for invalid org_id (FK violation
	// against organization).
	GetOrgConfig(ctx context.Context, orgID int64) (*models.OrgConfig, error)

	// Selector exclusion sets: org-scoped device identifiers subtracted
	// from the candidate set.
	ListActiveCurtailedDevices(ctx context.Context, orgID int64) ([]string, error)
	ListRecentlyResolvedCurtailedDevices(ctx context.Context, orgID int64, cooldownSec int32) ([]string, error)

	GetEventByUUID(ctx context.Context, orgID int64, eventUUID uuid.UUID) (*models.Event, error)

	// GetEventByIdempotencyKey is the retry-safe path for Service.Start.
	// Returns the previously-created event when (orgID, idempotencyKey)
	// matches a persisted row, or NotFound when no match exists. Callers
	// use this to short-circuit a duplicate idempotency_key into the
	// original event's response shape rather than triggering the partial
	// unique index violation at insert time.
	GetEventByIdempotencyKey(ctx context.Context, orgID int64, idempotencyKey string) (*models.Event, error)

	ListTargetsByEvent(ctx context.Context, orgID int64, eventUUID uuid.UUID) ([]*models.Target, error)

	// InsertEventWithTargets writes the event row plus every per-target row
	// in a single transaction. The store sets each target's
	// CurtailmentEventID to the inserted event's id; callers leave that
	// field zero. Callers must validate the params shape (non-empty
	// targets, no duplicate device_identifiers) before invoking.
	InsertEventWithTargets(
		ctx context.Context,
		event models.InsertEventParams,
		targets []models.InsertTargetParams,
	) (*models.InsertEventResult, error)

	// Heartbeat singleton row used by liveness alerts.
	GetHeartbeat(ctx context.Context) (*models.Heartbeat, error)

	// ListCandidates returns per-device state for the selector's filter
	// + rank pipeline. Org-scoped; deviceIdentifiers narrows to the listed
	// devices when non-empty, or returns the whole org when nil. Callers
	// must normalize an empty slice to nil before invoking — Service.Preview
	// does this so internal callers see a single rule. Results are ordered
	// deterministically by device_identifier so two Previews against the
	// same inputs select the same miners. The query LEFT-JOINs telemetry
	// so devices with no recent samples come back with nil PowerW/HashRateHS
	// — the service layer interprets that as stale telemetry and emits the
	// skip reason.
	ListCandidates(ctx context.Context, orgID int64, deviceIdentifiers []string) ([]*models.Candidate, error)

	// ListNonTerminalEvents returns every pending/active/restoring event
	// across all orgs. The reconciler is a singleton process so this
	// method is intentionally not org-scoped; callers MUST NOT expose it
	// through any RPC handler.
	ListNonTerminalEvents(ctx context.Context) ([]*models.Event, error)

	// UpdateEventState transitions an event row. startedAt/endedAt are
	// optional — pass nil to leave the column unchanged. Setting either
	// to a non-nil time when it was already set will overwrite it.
	UpdateEventState(ctx context.Context, eventID int64, state models.EventState, startedAt *time.Time, endedAt *time.Time) error

	// UpdateTargetState patches a curtailment_target row keyed by
	// (eventID, deviceIdentifier). All non-state fields are optional;
	// nil pointers preserve the existing column value via COALESCE.
	UpdateTargetState(ctx context.Context, eventID int64, deviceIdentifier string, params UpdateCurtailmentTargetStateParams) error

	// UpsertHeartbeat overwrites the singleton row with the latest tick
	// metadata. Migration seeds the row at id=1; the upsert is robust
	// against accidental deletion.
	UpsertHeartbeat(ctx context.Context, params UpsertCurtailmentHeartbeatParams) error
}
