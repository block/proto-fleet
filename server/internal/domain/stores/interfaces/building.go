package interfaces

import (
	"context"

	"github.com/block/proto-fleet/server/internal/domain/buildings/models"
)

//go:generate go run go.uber.org/mock/mockgen -source=building.go -destination=mocks/mock_building_store.go -package=mocks BuildingStore

// BuildingStore is the persistence boundary for the buildings domain.
// All methods are org-scoped.
//
//nolint:interfacebloat // complete CRUD for buildings + rack-placement queries
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
	// site_id — that lives on SiteService.AssignBuildingsToSite for
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

	// ListBuildingRacks returns racks currently assigned to the
	// building with their grid placement, paginated by an opaque
	// cursor. Service layer clamps pageSize to the proto cap; an
	// empty pageToken starts at the first page. Returns the next
	// page token (empty when the caller has reached the last page).
	ListBuildingRacks(ctx context.Context, orgID, buildingID int64, pageSize int32, pageToken string) ([]models.BuildingRack, string, error)

	// ListRacksOutsideBuildingBounds returns racks whose grid
	// position would fall outside the proposed (aisles,
	// racksPerAisle) layout. Unbounded by design — used by
	// UpdateBuilding's shrink guard, where missing a tail row
	// would silently orphan it past the cap.
	ListRacksOutsideBuildingBounds(ctx context.Context, orgID, buildingID int64, newAisles, newRacksPerAisle int32) ([]models.BuildingRack, error)

	// SetRackBuildingPosition writes only the grid-position fields on
	// device_set_rack. Caller is expected to have already set
	// building_id via the collection store's UpdateRackPlacement in
	// the same transaction.
	SetRackBuildingPosition(ctx context.Context, orgID, rackID int64, aisleIndex, positionInAisle *int32) error

	// SetRackBuildingPositionBulkClear nulls (aisle_index,
	// position_in_aisle) for every rack in rackIDs in one statement.
	// Used by AssignRacksToBuilding's pass-1 vacate.
	SetRackBuildingPositionBulkClear(ctx context.Context, orgID int64, rackIDs []int64) error

	// SetRackBuildingPositionBulkPlace writes per-rack
	// (aisleIndexes[i], positionInAisles[i]) for every rack in
	// rackIDs (parallel-aligned arrays). Used by
	// AssignRacksToBuilding's pass-2 after pass-1 cleared cells.
	SetRackBuildingPositionBulkPlace(ctx context.Context, orgID int64, rackIDs []int64, aisleIndexes, positionInAisles []int32) error

	// AssignDevicesToBuilding bulk-updates device.building_id for the
	// given identifiers. The caller must have validated cross-building
	// conflicts (see FindDeviceBuildingConflicts) and that every
	// identifier exists (see ListExistingDeviceIdentifiers on SiteStore).
	// targetBuildingID == nil means "Unassigned".
	AssignDevicesToBuilding(ctx context.Context, orgID int64, targetBuildingID *int64, deviceIdentifiers []string) (int64, error)

	// CascadeDevicesSiteForBuilding sets device.site_id to the supplied
	// target_site_id for any device in @device_identifiers whose
	// site_id differs from target. Caller is responsible for matching
	// targetSiteID to the building's site. Returns the count of
	// devices actually cascaded.
	CascadeDevicesSiteForBuilding(ctx context.Context, orgID int64, deviceIdentifiers []string, targetSiteID *int64) (int64, error)

	// FindDeviceBuildingConflicts returns, for each requested device
	// that is in a rack with a building_id, the device identifier and
	// that rack's building_id. The caller compares against the
	// requested target.
	FindDeviceBuildingConflicts(ctx context.Context, orgID int64, deviceIdentifiers []string) (map[string]int64, error)

	// GetBuildingSiteID returns the building's site_id (nil when the
	// building is unassigned). Returns NotFound when the building is
	// missing / soft-deleted / cross-org.
	GetBuildingSiteID(ctx context.Context, orgID, buildingID int64) (*int64, error)

	// ClearDeviceBuildingsByBuilding nulls device.building_id for every
	// direct-FK device pointing at the given building. Called by
	// DeleteBuilding's soft-delete cascade so device references don't
	// outlive the soft-deleted row.
	ClearDeviceBuildingsByBuilding(ctx context.Context, orgID, buildingID int64) (int64, error)

	// ClearDeviceBuildingsBySite nulls device.building_id for every
	// direct-FK device whose building belongs to the given site.
	// Called by DeleteSite's cascade.
	ClearDeviceBuildingsBySite(ctx context.Context, orgID, siteID int64) (int64, error)

	// CascadeDirectDeviceSitesByBuildings rewrites device.site_id to
	// targetSiteID for devices joined to any building in buildingIDs
	// via device.building_id. Mirror of ReassignDevicesUnderBuildingsBulk
	// but for direct-FK devices (no rack involved).
	CascadeDirectDeviceSitesByBuildings(ctx context.Context, orgID int64, buildingIDs []int64, targetSiteID *int64) (int64, error)
}
