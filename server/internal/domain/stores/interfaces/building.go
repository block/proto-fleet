package interfaces

import (
	"context"

	"github.com/block/proto-fleet/server/internal/domain/buildings/models"
)

//go:generate go run go.uber.org/mock/mockgen -source=building.go -destination=mocks/mock_building_store.go -package=mocks BuildingStore

// BuildingStore is the persistence boundary for the buildings domain.
// All methods are org-scoped.
type BuildingStore interface {
	// CreateBuilding inserts a new building row. Maps a
	// unique-violation on (site_id, name) to AlreadyExists.
	// SiteID == 0 means "unassigned"; the partial unique index
	// excludes those rows so create never collides on name when
	// unassigned.
	CreateBuilding(ctx context.Context, params models.CreateParams) (*models.Building, error)

	// GetBuilding returns the live building or NotFound.
	GetBuilding(ctx context.Context, orgID, id int64) (*models.Building, error)

	// ListBuildings returns every live building in the org with its
	// rack_count, ordered by name. Filter selects scope.
	ListBuildings(ctx context.Context, filter models.ListFilter) ([]models.BuildingWithCounts, error)

	// UpdateBuilding mutates the row's mutable fields (excluding
	// site_id — that lives on SiteService.AssignBuildingToSite for
	// cross-collection enforcement). Returns NotFound when row gone.
	UpdateBuilding(ctx context.Context, params models.UpdateParams) (*models.Building, error)

	// SoftDeleteBuilding sets deleted_at; caller is responsible for
	// the surrounding transaction and the cascade-unassign of racks
	// (UnassignRacksFromBuilding) in the same tx.
	SoftDeleteBuilding(ctx context.Context, orgID, id int64) (int64, error)

	// UnassignRacksFromBuilding sets device_set_rack.building_id =
	// NULL for every rack pointing at the building. Returns the
	// count.
	UnassignRacksFromBuilding(ctx context.Context, orgID, buildingID int64) (int64, error)

	// BuildingBelongsToOrg returns true when a live building with
	// the given id exists in the org.
	BuildingBelongsToOrg(ctx context.Context, orgID, id int64) (bool, error)

	// BuildingsByIDs returns the subset of the requested IDs that
	// correspond to live buildings in the org. Caller diffs against
	// the requested set to detect cross-org or missing IDs. Used by
	// parseFilter to bulk-validate building_ids and zone_keys
	// references in one round trip.
	BuildingsByIDs(ctx context.Context, orgID int64, ids []int64) ([]int64, error)
}
