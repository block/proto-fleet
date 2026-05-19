package interfaces

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
)

// UpdateCurtailmentTargetStateParams: optional patch fields. Nil pointers
// leave the column unchanged via COALESCE in the SQL update.
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

// UpsertCurtailmentHeartbeatParams describes the singleton liveness row
// upserted at the end of every successful reconciler tick.
type UpsertCurtailmentHeartbeatParams struct {
	LastTickAt         time.Time
	LastTickUUID       uuid.UUID
	LastTickDurationMS *int32
	ActiveEventCount   int32
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
	// leaves the column unchanged; non-nil overwrites.
	UpdateEventState(ctx context.Context, eventID int64, state models.EventState, startedAt *time.Time, endedAt *time.Time) error

	// UpdateTargetState patches the (eventID, deviceIdentifier) row.
	// Non-state fields use COALESCE: nil preserves the existing column.
	UpdateTargetState(ctx context.Context, eventID int64, deviceIdentifier string, params UpdateCurtailmentTargetStateParams) error

	// UpsertHeartbeat overwrites the singleton row at id=1. Migration seeds
	// the row; upsert is robust against accidental deletion.
	UpsertHeartbeat(ctx context.Context, params UpsertCurtailmentHeartbeatParams) error
}
