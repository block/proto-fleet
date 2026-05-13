package interfaces

import (
	"context"

	pb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
)

//go:generate go run go.uber.org/mock/mockgen -source=collection.go -destination=mocks/mock_collection_store.go -package=mocks CollectionStore

type DeviceRackDetails struct {
	Label    string
	Position string
}

// RackPlacement captures the rack's current site/building/zone assignment.
// All three may be NULL/empty (fully-unassigned rack).
type RackPlacement struct {
	SiteID     *int64
	BuildingID *int64
	Zone       string
}

// AddedDeviceSiteConflict reports a device whose current site_id differs
// from the rack it is being added to. Used by the AddDevicesToDeviceSet
// cascade flow to capture the prior site for activity-log auditing.
type AddedDeviceSiteConflict struct {
	DeviceIdentifier string
	PriorSiteID      *int64
	TargetSiteID     int64
}

// CreateRackExtensionParams captures the inputs for inserting a rack
// extension row. Mirrors the sqlc-generated param shape so callers don't
// juggle ten positional arguments and so future fields land cleanly.
// SiteID / BuildingID may be nil for a rack with no site / no building.
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

	// UpdateRackInfo updates rack-specific layout (rows, columns, zone, etc.).
	// Use UpdateRackPlacement to change site_id / building_id with cascade.
	UpdateRackInfo(ctx context.Context, collectionID int64, zone string, rows, columns int32, orderIndex, coolingType int32, orgID int64) error

	// LockRackPlacementForWrite acquires a row-level lock on the rack
	// extension and returns the current placement. Must run inside a
	// transaction; returns NotFound when the rack does not exist (or has
	// been soft-deleted) so callers surface a stable RPC error.
	LockRackPlacementForWrite(ctx context.Context, collectionID, orgID int64) (RackPlacement, error)

	// UpdateRackPlacement rewrites the rack's site_id, building_id, and
	// zone together. Used by the rack edit/move cascade after the caller
	// has locked the rack, building, and site rows.
	UpdateRackPlacement(ctx context.Context, collectionID, orgID int64, siteID, buildingID *int64, zone string) error

	// UnassignDeviceSitesByRack nulls device.site_id for every paired
	// member of the rack. Called from DeleteCollection in the same tx
	// as the rack soft-delete so devices that entered the site via this
	// rack don't keep pointing at it. No-op when the rack has no site
	// stamped or has no members.
	UnassignDeviceSitesByRack(ctx context.Context, collectionID, orgID int64) (int64, error)

	// CascadeRackDeviceSites rewrites device.site_id to targetSiteID
	// (nil for unassigned) for every paired rack member whose current
	// site_id differs. Returns the number of devices actually rewritten
	// so callers can mark zero-impact moves as no-op cascades.
	CascadeRackDeviceSites(ctx context.Context, collectionID, orgID int64, targetSiteID *int64) (int64, error)

	// GetDeviceSiteIDsByMembership returns the device_identifier + current
	// site_id for every member of the rack. Used by the cascade flow to
	// capture each device's prior site for activity-log metadata before
	// the rewrite. Returns the empty slice when the rack has no members.
	GetDeviceSiteIDsByMembership(ctx context.Context, collectionID, orgID int64) (map[string]*int64, error)

	// GetBuildingSite returns the building's parent site_id, used to
	// derive a rack's effective site when only building_id is supplied.
	// Returns NotFound for missing or soft-deleted buildings.
	GetBuildingSite(ctx context.Context, orgID, buildingID int64) (*int64, error)

	// GetAddedDeviceSiteConflicts returns the prior site_id for each
	// device in identifiers whose current site differs from the target
	// rack's site_id. Returns the empty slice for group targets or when
	// the rack has no site stamped. Used to stamp prior_site on the
	// activity-log row before CascadeAddedDeviceSites fires.
	GetAddedDeviceSiteConflicts(ctx context.Context, orgID, deviceSetID int64, deviceIdentifiers []string) ([]AddedDeviceSiteConflict, error)

	// CascadeAddedDeviceSites rewrites device.site_id to the rack's
	// site for every supplied paired device whose current site differs.
	// No-op when the target is a group or the rack has no site stamped.
	CascadeAddedDeviceSites(ctx context.Context, orgID, deviceSetID int64, deviceIdentifiers []string) (int64, error)

	// SoftDeleteCollection marks a collection as deleted.
	// Returns the number of rows affected (0 if not found).
	SoftDeleteCollection(ctx context.Context, orgID int64, collectionID int64) (int64, error)

	// ListCollections returns paginated collections for an organization.
	// If collectionType is UNSPECIFIED, returns all types.
	// Sort controls ordering; nil defaults to name ascending.
	// Returns the collections, a next page token (empty if no more results), and the total count.
	ListCollections(ctx context.Context, orgID int64, collectionType pb.CollectionType, pageSize int32, pageToken string, sort *SortConfig, errorComponentTypes []int32, zones []string) ([]*pb.DeviceCollection, string, int32, error)

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

	// ListCollectionMembers returns paginated members of a collection ordered by when they were added (newest first).
	// Returns the members and a next page token (empty if no more results).
	ListCollectionMembers(ctx context.Context, orgID int64, collectionID int64, pageSize int32, pageToken string) ([]*pb.CollectionMember, string, error)

	// GetDeviceCollections returns all collections a device belongs to, ordered by label.
	// If collectionType is UNSPECIFIED, returns all types.
	GetDeviceCollections(ctx context.Context, orgID int64, deviceIdentifier string, collectionType pb.CollectionType) ([]*pb.DeviceCollection, error)

	// GetGroupLabelsForDevices returns a map of device_identifier -> slice of group labels.
	// Used for batch lookup when building MinerStateSnapshot list.
	GetGroupLabelsForDevices(ctx context.Context, orgID int64, deviceIdentifiers []string) (map[string][]string, error)

	// GetRackDetailsForDevices returns a map of device_identifier -> rack label and formatted position.
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
	ListRackZones(ctx context.Context, orgID int64) ([]string, error)

	// ListRackTypes returns all distinct rack types (row/column combinations) for an organization.
	ListRackTypes(ctx context.Context, orgID int64) ([]*pb.RackType, error)

	// GetDeviceIdentifiersByDeviceSetID returns all device identifiers belonging to a device set (rack or group).
	GetDeviceIdentifiersByDeviceSetID(ctx context.Context, deviceSetID, orgID int64) ([]string, error)
}
