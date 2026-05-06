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
	// candidate-power floor, and the cooldown window. Returns NotFound when
	// the org has no row (the migration seeds one per existing org so this
	// only fires for orgs created after the migration).
	GetOrgConfig(ctx context.Context, orgID int64) (*models.OrgConfig, error)

	// Selector exclusion sets. Both return device identifiers for the
	// caller's org, used to subtract from the candidate set.
	ListActiveCurtailedDevices(ctx context.Context, orgID int64) ([]string, error)
	ListRecentlyResolvedCurtailedDevices(ctx context.Context, orgID int64, cooldownSec int32) ([]string, error)

	// Event CRUD. BE-2 exposes the minimum needed for the Preview surface
	// to verify schema constraints round-trip; BE-3+ will broaden the
	// surface as Start / Update / Stop / read APIs land.
	InsertEvent(ctx context.Context, params models.InsertEventParams) (*models.InsertEventResult, error)
	GetEventByUUID(ctx context.Context, orgID int64, eventUUID uuid.UUID) (*models.Event, error)

	// Target CRUD. BE-3 will likely add a bulk-insert path; for now the
	// per-row insert is enough for store tests to verify constraints.
	InsertTarget(ctx context.Context, params models.InsertTargetParams) error
	ListTargetsByEvent(ctx context.Context, orgID int64, eventUUID uuid.UUID) ([]*models.Target, error)

	// Heartbeat singleton — used by the alert predicate evaluator and by
	// future reconciler liveness checks.
	GetHeartbeat(ctx context.Context) (*models.Heartbeat, error)
}
