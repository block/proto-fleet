package interfaces

import (
	"context"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
)

// CurtailmentStore is the persistence boundary for the curtailment domain.
// All methods are org-scoped; cross-org reads must explicitly request a
// broader scope (none exist in v1).
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

	InsertEvent(ctx context.Context, params models.InsertEventParams) (*models.InsertEventResult, error)
	GetEventByUUID(ctx context.Context, orgID int64, eventUUID uuid.UUID) (*models.Event, error)

	InsertTarget(ctx context.Context, params models.InsertTargetParams) error
	ListTargetsByEvent(ctx context.Context, orgID int64, eventUUID uuid.UUID) ([]*models.Target, error)

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
}
