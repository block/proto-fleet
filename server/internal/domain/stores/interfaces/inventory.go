package interfaces

import (
	"context"

	"github.com/block/proto-fleet/server/internal/domain/inventory/models"
)

//go:generate go run go.uber.org/mock/mockgen -source=inventory.go -destination=mocks/mock_inventory_store.go -package=mocks InventoryStore

// InventoryStore is the organization-scoped persistence boundary for parts
// inventory. Every mutation uses an invariant predicate in SQL so concurrent
// callers cannot oversubscribe or consume stock twice.
// The cohesive boundary intentionally includes CRUD, stock transitions, and
// import resolution so one transaction-bound implementation owns every stock invariant.
type InventoryStore interface { //nolint:interfacebloat
	Create(ctx context.Context, params models.CreateParams) (*models.InventoryPart, error)
	Get(ctx context.Context, orgID, id int64) (*models.InventoryPart, error)
	GetForUpdate(ctx context.Context, orgID, id int64) (*models.InventoryPart, error)
	List(ctx context.Context, filter models.ListFilter) ([]models.InventoryPart, error)
	Update(ctx context.Context, params models.UpdateParams) (*models.InventoryPart, error)
	SoftDelete(ctx context.Context, orgID, id int64) (int64, error)
	GetInsights(ctx context.Context, orgID int64) (*models.InventoryInsights, error)
	ListPartsBySite(ctx context.Context, orgID, siteID int64) ([]models.InventoryPart, error)

	// Reserve moves available stock into the allocated quantity.
	Reserve(ctx context.Context, orgID, id int64, quantity int32) error
	// Release returns allocated stock to availability without changing on-hand.
	Release(ctx context.Context, orgID, id int64, quantity int32) error
	// ConsumeReserved decrements allocated and on-hand together.
	ConsumeReserved(ctx context.Context, orgID, id int64, quantity int32) error

	ResolveSiteByName(ctx context.Context, orgID int64, name string) (int64, error)
	// BulkCreate uses the transaction-bound query handle from ctx. Callers own
	// the transaction so any row failure rolls back the complete import.
	BulkCreate(ctx context.Context, orgID int64, rows []models.ResolvedCsvRow) (int32, error)
}
