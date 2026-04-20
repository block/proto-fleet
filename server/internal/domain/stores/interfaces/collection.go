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

// CollectionStore provides database operations for device collections (groups and racks).
//
//nolint:interfacebloat // complete CRUD for collections with membership management
type CollectionStore interface {
	// CreateCollection creates a new collection and returns it with device_count = 0.
	CreateCollection(ctx context.Context, orgID int64, collectionType pb.CollectionType, label, description string) (*pb.DeviceCollection, error)

	// CreateRackExtension creates the rack extension record with dimensions.
	// Must be called after CreateCollection for rack-type collections.
	CreateRackExtension(ctx context.Context, collectionID int64, zone string, rows, columns int32, orderIndex, coolingType int32, orgID int64) error

	// GetCollection retrieves a collection by ID with its device count.
	GetCollection(ctx context.Context, orgID int64, collectionID int64) (*pb.DeviceCollection, error)

	// GetRackInfo retrieves rack-specific info for a collection.
	// Returns nil if the collection is not a rack.
	GetRackInfo(ctx context.Context, collectionID int64, orgID int64) (*pb.RackInfo, error)

	// UpdateCollection updates a collection's label and/or description.
	// Only non-nil values are updated.
	UpdateCollection(ctx context.Context, orgID int64, collectionID int64, label, description *string) error

	// UpdateRackInfo updates rack-specific info.
	UpdateRackInfo(ctx context.Context, collectionID int64, zone string, rows, columns int32, orderIndex, coolingType int32, orgID int64) error

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
