package interfaces

import (
	"context"

	pb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
)

//go:generate go run go.uber.org/mock/mockgen -source=collection.go -destination=mocks/mock_collection_store.go -package=mocks CollectionStore

type DeviceRackDetails struct {
	ID            int64
	Label         string
	Position      string
	BuildingID    *int64
	BuildingLabel string
}

type DeviceGroupRef struct {
	ID    int64
	Label string
}

// RackPlacement captures the rack's current site/building/zone assignment.
// All three may be NULL/empty (fully-unassigned rack).
type RackPlacement struct {
	SiteID     *int64
	BuildingID *int64
	Zone       string
}

// AddedDeviceSiteConflict reports a device whose current site_id differs
// from the rack it's being added to, captured for the cascade audit.
type AddedDeviceSiteConflict struct {
	DeviceIdentifier string
	PriorSiteID      *int64
	TargetSiteID     int64
}

// CreateRackExtensionParams captures the inputs for inserting a rack
// extension row. SiteID / BuildingID may be nil for unassigned racks.
type CreateRackExtensionParams struct {
	OrgID        int64
	CollectionID int64
	Rows         int32
	Columns      int32
	OrderIndex   int32
	CoolingType  int32
	Zone         string
	SiteID       *int64
	BuildingID   *int64
}

// ZoneRefRow is the domain shape returned by ListRackZoneRefs. Maps to
// common.v1.ZoneRef on the wire. BuildingID == 0 indicates a rack with
// NULL building_id (legacy / Phase 1 uncategorized).
type ZoneRefRow struct {
	BuildingID    int64
	BuildingLabel string
	SiteID        int64
	SiteLabel     string
	Zone          string
}

// DeviceSetFilter is the rack-list / collection-list filter input.
// Mirrors the MinerFilter shape but joined directly on
// device_set_rack — no device membership traversal needed since the
// list query already returns one row per rack.
type DeviceSetFilter struct {
	ErrorComponentTypes []int32   // OR across types; surfaces racks with any device having an open error of those types
	SiteIDs             []int64   // OR across sites. Only valid for RACK collections; ignored for GROUP.
	IncludeUnassigned   bool      // Include racks where dsr.site_id IS NULL. OR'd with SiteIDs.
	BuildingIDs         []int64   // OR across buildings. Only valid for RACK collections; ignored for GROUP.
	IncludeNoBuilding   bool      // Include racks where dsr.building_id IS NULL. OR'd with BuildingIDs.
	ZoneKeys            []ZoneKey // (building_id, zone) pairs. BuildingID == 0 is the wildcard sentinel.
	TelemetryRanges     []NumericRange
}

// CollectionStore provides database operations for device collections (groups and racks).
//
//nolint:interfacebloat // complete CRUD for collections with membership management
type CollectionStore interface {
	// CreateCollection creates a new collection and returns it with device_count = 0.
	CreateCollection(ctx context.Context, orgID int64, collectionType pb.CollectionType, label, description string) (*pb.DeviceCollection, error)

	// CreateRackExtension creates the rack extension record with dimensions and placement.
	// Must be called after CreateCollection for rack-type collections.
	CreateRackExtension(ctx context.Context, params CreateRackExtensionParams) error

	// GetCollection retrieves a collection by ID with its device count.
	GetCollection(ctx context.Context, orgID int64, collectionID int64) (*pb.DeviceCollection, error)

	// GetRackInfo retrieves rack-specific info for a collection.
	// Returns nil if the collection is not a rack.
	GetRackInfo(ctx context.Context, collectionID int64, orgID int64) (*pb.RackInfo, error)

	// UpdateCollection updates a collection's label and/or description.
	// Only non-nil values are updated.
	UpdateCollection(ctx context.Context, orgID int64, collectionID int64, label, description *string) error

	// UpdateRackInfo updates rack layout (rows, columns, zone, etc.).
	// Use UpdateRackPlacement to change site_id / building_id.
	UpdateRackInfo(ctx context.Context, collectionID int64, zone string, rows, columns int32, orderIndex, coolingType int32, orgID int64) error

	// LockRackPlacementForWrite locks the rack row FOR UPDATE and returns
	// the current placement. Returns NotFound for missing or soft-deleted
	// racks.
	LockRackPlacementForWrite(ctx context.Context, collectionID, orgID int64) (RackPlacement, error)

	// UpdateRackPlacement sets the rack's site_id, building_id, and zone
	// atomically.
	UpdateRackPlacement(ctx context.Context, collectionID, orgID int64, siteID, buildingID *int64, zone string) error

	// UpdateRackPlacementBulkForBuilding writes site_id, building_id,
	// and zone in one statement for every rack in rackIDs. Semantics
	// mirror the per-row UpdateRackPlacement with the
	// AssignRacksToBuilding-specific rules in SQL:
	//   * targetBuildingID == nil keeps each rack's current site_id.
	//   * Zone clears to NULL for racks transitioning to a different (or
	//     NULL) building; preserved otherwise. NULL (not '') matches the
	//     per-row path so collection_sort.go's "zone NULLS LAST"
	//     ordering keeps racks-without-a-building in the trailing
	//     bucket.
	//   * Grid position clears when building_id changes.
	// Caller is expected to have locked every rack via
	// LockRackPlacementForWrite before invoking. Returns the row count
	// the UPDATE touched so callers can verify every requested rack id
	// resolved (defense-in-depth against stale or cross-org ids).
	UpdateRackPlacementBulkForBuilding(ctx context.Context, orgID int64, rackIDs []int64, targetSiteID, targetBuildingID *int64) (int64, error)

	// UpdateRackPlacementBulkForSite stamps every rack in rackIDs with
	// the target site, clears building_id + grid placement. Zone clears
	// only for racks that were actually in a building (leaving or
	// crossing a building) — matching the per-row UpdateRackPlacement
	// semantics, since zone is building-scoped. Racks with building_id
	// IS NULL preserve their zone so building-less zone metadata (which
	// ListRackZoneRefs surfaces) isn't silently wiped, and NULL (not '')
	// preserves the collection_sort.go "zone NULLS LAST" ordering.
	// Caller is expected to pass only racks whose site is actually
	// changing.
	UpdateRackPlacementBulkForSite(ctx context.Context, orgID int64, rackIDs []int64, targetSiteID *int64) error

	// UnassignDeviceSitesByRack nulls device.site_id for paired rack
	// members that match the rack's stamped site. No-op when the rack
	// has no site or no members.
	UnassignDeviceSitesByRack(ctx context.Context, collectionID, orgID int64) (int64, error)

	// CascadeRackDeviceSites rewrites device.site_id to targetSiteID for
	// rack members where the value differs. Returns the affected count.
	CascadeRackDeviceSites(ctx context.Context, collectionID, orgID int64, targetSiteID *int64) (int64, error)

	// CascadeRackDeviceSitesBulk is the multi-rack variant: rewrites
	// device.site_id to targetSiteID for every paired member of every
	// rack in rackIDs where the current value differs.
	CascadeRackDeviceSitesBulk(ctx context.Context, orgID int64, rackIDs []int64, targetSiteID *int64) (int64, error)

	// UnassignDeviceBuildingsByRack is the building peer of
	// UnassignDeviceSitesByRack: nulls device.building_id for paired
	// rack members whose value matches the rack's stamped building.
	// Preserves direct "Add miners to building" assignments that
	// diverged from the rack.
	UnassignDeviceBuildingsByRack(ctx context.Context, collectionID, orgID int64) (int64, error)

	// CascadeRackDeviceBuildings is the building peer of
	// CascadeRackDeviceSites: rewrites device.building_id to
	// targetBuildingID for paired members of the rack.
	CascadeRackDeviceBuildings(ctx context.Context, collectionID, orgID int64, targetBuildingID *int64) (int64, error)

	// CascadeRackDeviceBuildingsBulk is the multi-rack building peer
	// of CascadeRackDeviceSitesBulk.
	CascadeRackDeviceBuildingsBulk(ctx context.Context, orgID int64, rackIDs []int64, targetBuildingID *int64) (int64, error)

	// CascadeAddedDeviceBuildings is the building peer of
	// CascadeAddedDeviceSites: rewrites device.building_id to
	// rack.building_id for newly added rack members where the value
	// differs. No-op for groups or building-less racks.
	CascadeAddedDeviceBuildings(ctx context.Context, orgID, deviceSetID int64, deviceIdentifiers []string) (int64, error)

	// GetDeviceSiteIDsByMembership returns device_identifier + current
	// site_id for every rack member.
	GetDeviceSiteIDsByMembership(ctx context.Context, collectionID, orgID int64) (map[string]*int64, error)

	// GetBuildingSite returns the building's parent site_id; NotFound
	// for missing or soft-deleted buildings.
	GetBuildingSite(ctx context.Context, orgID, buildingID int64) (*int64, error)

	// GetAddedDeviceSiteConflicts returns prior + target site_id for
	// devices whose current site differs from the target rack. Empty for
	// groups or site-less racks.
	GetAddedDeviceSiteConflicts(ctx context.Context, orgID, deviceSetID int64, deviceIdentifiers []string) ([]AddedDeviceSiteConflict, error)

	// CascadeAddedDeviceSites rewrites device.site_id to rack.site_id
	// for the supplied devices where the value differs. No-op for groups
	// or site-less racks.
	CascadeAddedDeviceSites(ctx context.Context, orgID, deviceSetID int64, deviceIdentifiers []string) (int64, error)

	// SoftDeleteCollection marks a collection as deleted.
	// Returns the number of rows affected (0 if not found).
	SoftDeleteCollection(ctx context.Context, orgID int64, collectionID int64) (int64, error)

	// ClearRackPlacementForSoftDelete nulls the device_set_rack
	// placement fields for a rack about to be soft-deleted. Callers
	// invoke this inside the same tx that fires SoftDeleteCollection
	// so the rack's (building_id, aisle_index, position_in_aisle)
	// row doesn't continue to occupy a building cell on the partial
	// unique index. No-op for non-rack collection types.
	ClearRackPlacementForSoftDelete(ctx context.Context, orgID, collectionID int64) error

	// ListCollections returns paginated collections for an organization.
	// If collectionType is UNSPECIFIED, returns all types.
	// Sort controls ordering; nil defaults to name ascending.
	// filter may be nil; ZoneKeys / BuildingIDs / IncludeNoBuilding are
	// applied only when collectionType is RACK.
	// Returns the collections, a next page token (empty if no more results), and the total count.
	ListCollections(ctx context.Context, orgID int64, collectionType pb.CollectionType, pageSize int32, pageToken string, sort *SortConfig, filter *DeviceSetFilter) ([]*pb.DeviceCollection, string, int32, error)

	// CollectionBelongsToOrg checks if a collection exists and belongs to the organization.
	CollectionBelongsToOrg(ctx context.Context, collectionID int64, orgID int64) (bool, error)

	// GetCollectionType returns the type of a collection.
	GetCollectionType(ctx context.Context, orgID int64, collectionID int64) (pb.CollectionType, error)

	// GetCollectionTypes returns the types for multiple collections in a single query.
	// Returns a map of collectionID -> CollectionType.
	GetCollectionTypes(ctx context.Context, orgID int64, collectionIDs []int64) (map[int64]pb.CollectionType, error)

	// AddDevicesToCollection adds devices to a collection.
	// Returns the number of devices actually added (excludes duplicates and non-existent devices).
	AddDevicesToCollection(ctx context.Context, orgID int64, collectionID int64, deviceIdentifiers []string) (int64, error)

	// RemoveAllDevicesFromCollection removes all devices from a collection.
	// Returns the number of devices removed.
	RemoveAllDevicesFromCollection(ctx context.Context, orgID int64, collectionID int64) (int64, error)

	// RemoveDevicesFromCollection removes devices from a collection.
	// Returns the number of devices actually removed.
	RemoveDevicesFromCollection(ctx context.Context, orgID int64, collectionID int64, deviceIdentifiers []string) (int64, error)

	// RemoveDevicesFromAnyRack deletes the given devices' rack
	// membership rows regardless of which rack they currently sit in,
	// EXCEPT the target rack (targetRackID). Used by AssignDevicesToRack
	// to clear prior rack membership inside the same transaction as the
	// new-rack insert, closing the orphan window the client-side
	// RemoveDevicesFromDeviceSet + AddDevicesToDeviceSet orchestration
	// had. Excluding targetRackID preserves the membership row (and its
	// rack_slot child) for devices already in the target rack -- a
	// re-add inside the same transaction would silently drop the
	// rack_slot via the FK cascade. Pass 0 to clear unconditionally
	// (caller intends to unassign).
	RemoveDevicesFromAnyRack(ctx context.Context, orgID int64, deviceIdentifiers []string, targetRackID int64) (int64, error)

	// FindDevicesWithSiteOrBuilding returns the requested identifiers
	// whose device.site_id OR device.building_id is currently non-NULL.
	// AssignDevicesToRack uses it to detect miners that would lose a
	// placement by joining a site-less rack (the force path clears both
	// columns), so the caller can confirm before stripping.
	FindDevicesWithSiteOrBuilding(ctx context.Context, orgID int64, deviceIdentifiers []string) ([]string, error)

	// ClearDeviceSitesAndBuildings nulls device.site_id and
	// device.building_id for the given identifiers (skipping rows already
	// fully cleared). AssignDevicesToRack's force path calls it when
	// adding miners to a site-less rack — the rack dictates no placement,
	// so members can't keep a direct site/building. Returns the count
	// actually stripped.
	ClearDeviceSitesAndBuildings(ctx context.Context, orgID int64, deviceIdentifiers []string) (int64, error)

	// LockRacksForReparent takes FOR UPDATE locks on every rack involved
	// in a reparent -- every source rack currently holding any of the
	// given devices PLUS targetRackID (when non-zero) -- in ascending
	// device_set_id order, and returns the locked ids.
	// AssignDevicesToRack calls this as the FIRST tx operation. Locking
	// source and target together in one globally sorted acquisition is
	// what prevents two concurrent reparent calls moving devices in
	// opposite directions between the same rack pair from deadlocking
	// (tx A locking source 1 then target 2 while tx B locks source 2
	// then target 1). Pass 0 for targetRackID in the clear-rack path
	// where there is no target. The subsequent
	// LockRackPlacementForWrite call on the target still happens for
	// its placement read; this query handles the rack-id locks.
	LockRacksForReparent(ctx context.Context, orgID int64, deviceIdentifiers []string, targetRackID int64) ([]int64, error)

	// ListCollectionMembers returns paginated members of a collection ordered by when they were added (newest first).
	// Returns the members and a next page token (empty if no more results).
	ListCollectionMembers(ctx context.Context, orgID int64, collectionID int64, pageSize int32, pageToken string) ([]*pb.CollectionMember, string, error)

	// GetDeviceCollections returns all collections a device belongs to, ordered by label.
	// If collectionType is UNSPECIFIED, returns all types.
	GetDeviceCollections(ctx context.Context, orgID int64, deviceIdentifier string, collectionType pb.CollectionType) ([]*pb.DeviceCollection, error)

	// GetGroupRefsForDevices returns a map of device_identifier -> slice of group refs.
	// Used for batch lookup when building MinerStateSnapshot list.
	GetGroupRefsForDevices(ctx context.Context, orgID int64, deviceIdentifiers []string) (map[string][]DeviceGroupRef, error)

	// GetRackDetailsForDevices returns a map of device_identifier -> rack ref, building ref, and formatted position.
	// Each device can only be in one rack due to the partial unique index.
	GetRackDetailsForDevices(ctx context.Context, orgID int64, deviceIdentifiers []string) (map[string]DeviceRackDetails, error)

	// SetRackSlotPosition assigns a device to a specific slot position in a rack.
	SetRackSlotPosition(ctx context.Context, collectionID int64, deviceIdentifier string, row, column int32, orgID int64) error

	// ClearRackSlotPosition removes a device's slot position assignment from a rack.
	ClearRackSlotPosition(ctx context.Context, collectionID int64, deviceIdentifier string, orgID int64) error

	// GetRackSlots returns all occupied slot positions in a rack.
	GetRackSlots(ctx context.Context, collectionID int64, orgID int64) ([]*pb.RackSlot, error)

	// GetRackSlotStatuses returns per-slot device status for rack-type collections.
	// Returns all rows×cols positions including empty slots, keyed by collection ID.
	GetRackSlotStatuses(ctx context.Context, orgID int64, collectionIDs []int64) (map[int64][]*pb.RackSlotStatus, error)

	// ListRackZones returns all distinct non-empty rack zones for an organization.
	//
	// Deprecated: returns org-wide flat zone strings that collapse zones with
	// the same label across buildings. New callers should use
	// ListRackZoneRefs which returns (building_id, zone) tuples with
	// denormalized building + site labels.
	ListRackZones(ctx context.Context, orgID int64) ([]string, error)

	// ListRackZoneRefs returns all distinct (building_id, zone) pairs across
	// the org's racks, with denormalized building and site labels for cheap
	// dropdown rendering. Sorted by site_label, then building_label, then zone.
	ListRackZoneRefs(ctx context.Context, orgID int64) ([]ZoneRefRow, error)

	// ListRackTypes returns all distinct rack types (row/column combinations) for an organization.
	ListRackTypes(ctx context.Context, orgID int64) ([]*pb.RackType, error)

	// GetDeviceIdentifiersByDeviceSetID returns all device identifiers belonging to a device set (rack or group).
	GetDeviceIdentifiersByDeviceSetID(ctx context.Context, deviceSetID, orgID int64) ([]string, error)
}
