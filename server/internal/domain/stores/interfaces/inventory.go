package interfaces

import (
	"context"

	"github.com/block/proto-fleet/server/internal/domain/inventory/models"
)

//go:generate go run go.uber.org/mock/mockgen -source=inventory.go -destination=mocks/mock_inventory_store.go -package=mocks InventoryStore

// InventoryStore is the persistence boundary for the inventory domain.
// All methods are org-scoped.
type InventoryStore interface {
	// Create inserts a new inventory_part row. Maps a
	// unique-violation on (org_id, site_id, name) to AlreadyExists.
	Create(ctx context.Context, params models.CreateParams) (*models.InventoryPart, error)

	// Get returns the live part or NotFound.
	Get(ctx context.Context, orgID, id int64) (*models.InventoryPart, error)

	// List returns live parts matching the filter, ordered by name
	// then id, cursor-paginated.
	List(ctx context.Context, filter models.ListFilter) ([]models.InventoryPart, error)

	// Update mutates the row's mutable fields (on_hand,
	// reorder_point, bin_location). Returns NotFound when the row
	// is missing or soft-deleted.
	Update(ctx context.Context, params models.UpdateParams) (*models.InventoryPart, error)

	// SoftDelete sets deleted_at on the part row. Returns the number
	// of rows affected (0 when the row is already gone).
	SoftDelete(ctx context.Context, orgID, id int64) (int64, error)

	// GetInsights returns aggregate inventory stats for the org.
	GetInsights(ctx context.Context, orgID int64) (*models.InventoryInsights, error)

	// ListPartsBySite returns in-stock parts at a given site for the
	// repair ticket part picker. Only parts with available stock
	// (on_hand - allocated > 0) are returned.
	ListPartsBySite(ctx context.Context, orgID, siteID int64) ([]models.InventoryPart, error)

	// DecrementPartStock decrements on_hand for a part when used
	// in a repair. Fails silently (no rows affected) when on_hand
	// would go negative.
	DecrementPartStock(ctx context.Context, orgID, id int64, quantity int32) error

	// IncrementPartAllocated allocates stock to an active repair.
	IncrementPartAllocated(ctx context.Context, orgID, id int64, quantity int32) error

	// DecrementPartAllocated releases allocated stock (repair
	// cancelled or completed). Clamps to zero.
	DecrementPartAllocated(ctx context.Context, orgID, id int64, quantity int32) error
}
