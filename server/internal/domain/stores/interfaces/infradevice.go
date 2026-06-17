package interfaces

import (
	"context"

	"github.com/block/proto-fleet/server/internal/domain/infradevice/models"
)

//go:generate go run go.uber.org/mock/mockgen -source=infradevice.go -destination=mocks/mock_infradevice_store.go -package=mocks InfraDeviceStore

// InfraDeviceStore is the persistence boundary for the infradevice
// domain. All methods are org-scoped.
type InfraDeviceStore interface {
	// Create inserts a new infra device row. Maps a unique-violation on
	// (org_id, name) to AlreadyExists.
	Create(ctx context.Context, params models.CreateParams) (*models.InfraDevice, error)

	// Get returns the live infra device or NotFound.
	Get(ctx context.Context, orgID, id int64) (*models.InfraDevice, error)

	// List returns every live infra device in the org matching the
	// filter, ordered by name.
	List(ctx context.Context, filter models.ListFilter) ([]models.InfraDevice, error)

	// Count returns the number of live infra devices matching the
	// filter.
	Count(ctx context.Context, filter models.ListFilter) (int64, error)

	// Update mutates the row's mutable fields. Returns NotFound when
	// the row is missing or soft-deleted.
	Update(ctx context.Context, params models.UpdateParams) (*models.InfraDevice, error)

	// SoftDelete sets deleted_at on the infra device. Returns the
	// number of rows affected (0 when not found).
	SoftDelete(ctx context.Context, orgID, id int64) (int64, error)

	// BulkUpdateControlMode sets control_mode for every device matching
	// the supplied IDs in the org. Returns the count of rows updated.
	BulkUpdateControlMode(ctx context.Context, orgID int64, ids []int64, controlMode int16) (int64, error)

	// BulkSoftDelete sets deleted_at on every device matching the
	// supplied IDs in the org. Returns the count of rows affected.
	BulkSoftDelete(ctx context.Context, orgID int64, ids []int64) (int64, error)

	// GetStats returns aggregate counts across all live infra devices
	// in the org.
	GetStats(ctx context.Context, orgID int64) (*models.InfraDeviceStats, error)

	// ListByBuilding returns every live infra device assigned to the
	// given building, ordered by name.
	ListByBuilding(ctx context.Context, orgID, buildingID int64) ([]models.InfraDevice, error)
}
