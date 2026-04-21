package sqlstores_test

import (
	"database/sql"
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	"github.com/block/proto-fleet/server/internal/domain/diagnostics/models"
	sqlstoresinterfaces "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupCollectionTestData(t *testing.T, deviceCount int) (db *sql.DB, orgID int64, deviceIDs []string) {
	t.Helper()
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	adminUser := testContext.DatabaseService.CreateSuperAdminUser()
	deviceIDs = make([]string, deviceCount)
	for i := range deviceCount {
		device := testContext.DatabaseService.CreateDevice(adminUser.OrganizationID, "proto")
		deviceIDs[i] = device.ID
	}
	return testContext.DatabaseService.DB, adminUser.OrganizationID, deviceIDs
}

func newCollectionStore(db *sql.DB) *sqlstores.SQLCollectionStore {
	return sqlstores.NewSQLCollectionStore(db)
}

func createCollectionIssue(deviceID string, minerError models.MinerError, componentType models.ComponentType) *models.ErrorMessage {
	errMsg := createTestErrorMessage(deviceID)
	errMsg.MinerError = minerError
	errMsg.ComponentType = componentType
	return errMsg
}

func TestCollectionStore_CreateAndGet(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, _ := setupCollectionTestData(t, 0)
	store := newCollectionStore(db)
	ctx := t.Context()

	// Act
	created, err := store.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_GROUP, "My Group", "A test group")

	// Assert
	require.NoError(t, err)
	assert.Equal(t, "My Group", created.Label)
	assert.Equal(t, "A test group", created.Description)
	assert.Equal(t, pb.CollectionType_COLLECTION_TYPE_GROUP, created.Type)
	assert.Equal(t, int32(0), created.DeviceCount)

	// Act - retrieve it
	fetched, err := store.GetCollection(ctx, orgID, created.Id)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, created.Id, fetched.Id)
	assert.Equal(t, "My Group", fetched.Label)
	assert.Equal(t, int32(0), fetched.DeviceCount)
}

func TestCollectionStore_GetCollection_NotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, _ := setupCollectionTestData(t, 0)
	store := newCollectionStore(db)
	ctx := t.Context()

	// Act
	_, err := store.GetCollection(ctx, orgID, 999999)

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestCollectionStore_GetCollection_WrongOrg(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, _ := setupCollectionTestData(t, 0)
	store := newCollectionStore(db)
	ctx := t.Context()

	// Arrange
	created, err := store.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_GROUP, "Org1 Group", "")
	require.NoError(t, err)

	// Act - try to fetch with a different org ID
	_, err = store.GetCollection(ctx, orgID+999, created.Id)

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestCollectionStore_AddAndListMembers(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupCollectionTestData(t, 3)
	store := newCollectionStore(db)
	ctx := t.Context()

	// Arrange
	collection, err := store.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_GROUP, "Group", "")
	require.NoError(t, err)

	// Act
	added, err := store.AddDevicesToCollection(ctx, orgID, collection.Id, deviceIDs)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, int64(3), added)

	// Verify device count via GetCollection
	fetched, err := store.GetCollection(ctx, orgID, collection.Id)
	require.NoError(t, err)
	assert.Equal(t, int32(3), fetched.DeviceCount)

	// Verify members list
	members, _, err := store.ListCollectionMembers(ctx, orgID, collection.Id, 100, "")
	require.NoError(t, err)
	assert.Len(t, members, 3)
}

func TestCollectionStore_AddDevices_Idempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupCollectionTestData(t, 2)
	store := newCollectionStore(db)
	ctx := t.Context()

	// Arrange
	collection, err := store.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_GROUP, "Group", "")
	require.NoError(t, err)

	// Act - add same devices twice
	added1, err := store.AddDevicesToCollection(ctx, orgID, collection.Id, deviceIDs)
	require.NoError(t, err)
	added2, err := store.AddDevicesToCollection(ctx, orgID, collection.Id, deviceIDs)
	require.NoError(t, err)

	// Assert - second add should be a no-op
	assert.Equal(t, int64(2), added1)
	assert.Equal(t, int64(0), added2)
}

func TestCollectionStore_RemoveDevices(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupCollectionTestData(t, 3)
	store := newCollectionStore(db)
	ctx := t.Context()

	// Arrange
	collection, err := store.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_GROUP, "Group", "")
	require.NoError(t, err)
	_, err = store.AddDevicesToCollection(ctx, orgID, collection.Id, deviceIDs)
	require.NoError(t, err)

	// Act - remove first two
	removed, err := store.RemoveDevicesFromCollection(ctx, orgID, collection.Id, deviceIDs[:2])

	// Assert
	require.NoError(t, err)
	assert.Equal(t, int64(2), removed)

	fetched, err := store.GetCollection(ctx, orgID, collection.Id)
	require.NoError(t, err)
	assert.Equal(t, int32(1), fetched.DeviceCount)
}

func TestCollectionStore_SoftDelete(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, _ := setupCollectionTestData(t, 0)
	store := newCollectionStore(db)
	ctx := t.Context()

	// Arrange
	collection, err := store.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_GROUP, "To Delete", "")
	require.NoError(t, err)

	// Act
	rows, err := store.SoftDeleteCollection(ctx, orgID, collection.Id)
	require.NoError(t, err)
	assert.Equal(t, int64(1), rows)

	// Assert - should no longer be findable
	_, err = store.GetCollection(ctx, orgID, collection.Id)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")

	// Assert - double delete returns 0
	rows, err = store.SoftDeleteCollection(ctx, orgID, collection.Id)
	require.NoError(t, err)
	assert.Equal(t, int64(0), rows)
}

func TestCollectionStore_SoftDeletedCollection_CannotAddDevices(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupCollectionTestData(t, 1)
	store := newCollectionStore(db)
	ctx := t.Context()

	// Arrange - create and soft-delete
	collection, err := store.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_GROUP, "Deleted Group", "")
	require.NoError(t, err)
	_, err = store.SoftDeleteCollection(ctx, orgID, collection.Id)
	require.NoError(t, err)

	// Act - try to add devices to the deleted collection
	added, err := store.AddDevicesToCollection(ctx, orgID, collection.Id, deviceIDs)

	// Assert - should add 0 because the deleted_at check prevents the CROSS JOIN from matching
	require.NoError(t, err)
	assert.Equal(t, int64(0), added)
}

func TestCollectionStore_ListCollections_FiltersByType(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, _ := setupCollectionTestData(t, 0)
	store := newCollectionStore(db)
	ctx := t.Context()

	// Arrange
	_, err := store.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_GROUP, "Group A", "")
	require.NoError(t, err)
	_, err = store.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_GROUP, "Group B", "")
	require.NoError(t, err)
	_, err = store.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_RACK, "Rack A", "")
	require.NoError(t, err)

	// Act + Assert - filter by group
	groups, _, _, err := store.ListCollections(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_GROUP, 100, "", nil, nil, nil)
	require.NoError(t, err)
	assert.Len(t, groups, 2)

	// Act + Assert - filter by rack
	racks, _, _, err := store.ListCollections(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_RACK, 100, "", nil, nil, nil)
	require.NoError(t, err)
	assert.Len(t, racks, 1)

	// Act + Assert - unspecified returns all
	all, _, _, err := store.ListCollections(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_UNSPECIFIED, 100, "", nil, nil, nil)
	require.NoError(t, err)
	assert.Len(t, all, 3)
}

func TestCollectionStore_ListCollections_ExcludesSoftDeleted(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, _ := setupCollectionTestData(t, 0)
	store := newCollectionStore(db)
	ctx := t.Context()

	// Arrange
	kept, err := store.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_GROUP, "Keep", "")
	require.NoError(t, err)
	deleted, err := store.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_GROUP, "Delete", "")
	require.NoError(t, err)
	_, err = store.SoftDeleteCollection(ctx, orgID, deleted.Id)
	require.NoError(t, err)

	// Act
	collections, _, _, err := store.ListCollections(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_UNSPECIFIED, 100, "", nil, nil, nil)

	// Assert
	require.NoError(t, err)
	require.Len(t, collections, 1)
	assert.Equal(t, kept.Id, collections[0].Id)
}

func TestCollectionStore_GetDeviceCollections(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupCollectionTestData(t, 1)
	store := newCollectionStore(db)
	ctx := t.Context()

	// Arrange - device in 2 groups and 1 rack
	group1, err := store.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_GROUP, "Group 1", "")
	require.NoError(t, err)
	group2, err := store.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_GROUP, "Group 2", "")
	require.NoError(t, err)
	rack, err := store.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_RACK, "Rack 1", "")
	require.NoError(t, err)

	_, err = store.AddDevicesToCollection(ctx, orgID, group1.Id, deviceIDs)
	require.NoError(t, err)
	_, err = store.AddDevicesToCollection(ctx, orgID, group2.Id, deviceIDs)
	require.NoError(t, err)
	_, err = store.AddDevicesToCollection(ctx, orgID, rack.Id, deviceIDs)
	require.NoError(t, err)

	// Act + Assert - all types
	all, err := store.GetDeviceCollections(ctx, orgID, deviceIDs[0], pb.CollectionType_COLLECTION_TYPE_UNSPECIFIED)
	require.NoError(t, err)
	assert.Len(t, all, 3)

	// Act + Assert - groups only
	groups, err := store.GetDeviceCollections(ctx, orgID, deviceIDs[0], pb.CollectionType_COLLECTION_TYPE_GROUP)
	require.NoError(t, err)
	assert.Len(t, groups, 2)

	// Act + Assert - racks only
	racks, err := store.GetDeviceCollections(ctx, orgID, deviceIDs[0], pb.CollectionType_COLLECTION_TYPE_RACK)
	require.NoError(t, err)
	assert.Len(t, racks, 1)
}

func TestCollectionStore_GetGroupLabelsForDevices(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupCollectionTestData(t, 2)
	store := newCollectionStore(db)
	ctx := t.Context()

	// Arrange - device[0] in 2 groups, device[1] in 1 group
	groupA, err := store.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_GROUP, "Alpha", "")
	require.NoError(t, err)
	groupB, err := store.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_GROUP, "Beta", "")
	require.NoError(t, err)
	_, err = store.AddDevicesToCollection(ctx, orgID, groupA.Id, deviceIDs)
	require.NoError(t, err)
	_, err = store.AddDevicesToCollection(ctx, orgID, groupB.Id, deviceIDs[:1])
	require.NoError(t, err)

	// Also add device[0] to a rack - should NOT appear in group labels
	rack, err := store.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_RACK, "Rack 1", "")
	require.NoError(t, err)
	_, err = store.AddDevicesToCollection(ctx, orgID, rack.Id, deviceIDs[:1])
	require.NoError(t, err)

	// Act
	labels, err := store.GetGroupLabelsForDevices(ctx, orgID, deviceIDs)

	// Assert
	require.NoError(t, err)
	assert.Len(t, labels[deviceIDs[0]], 2)
	assert.Len(t, labels[deviceIDs[1]], 1)
	assert.ElementsMatch(t, []string{"Alpha", "Beta"}, labels[deviceIDs[0]])
	assert.Equal(t, []string{"Alpha"}, labels[deviceIDs[1]])
}

func TestCollectionStore_GetRackDetailsForDevices(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupCollectionTestData(t, 2)
	store := newCollectionStore(db)
	ctx := t.Context()

	rack, err := store.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_RACK, "Floor 1", "")
	require.NoError(t, err)
	err = store.CreateRackExtension(ctx, rack.Id, "Floor 1", 12, 12, 2, 0, orgID)
	require.NoError(t, err)
	_, err = store.AddDevicesToCollection(ctx, orgID, rack.Id, deviceIDs)
	require.NoError(t, err)

	err = store.SetRackSlotPosition(ctx, rack.Id, deviceIDs[0], 0, 0, orgID)
	require.NoError(t, err)
	err = store.SetRackSlotPosition(ctx, rack.Id, deviceIDs[1], 8, 3, orgID)
	require.NoError(t, err)

	details, err := store.GetRackDetailsForDevices(ctx, orgID, deviceIDs)
	require.NoError(t, err)
	require.Len(t, details, 2)
	assert.Equal(t, "Floor 1", details[deviceIDs[0]].Label)
	assert.Equal(t, "01", details[deviceIDs[0]].Position)
	assert.Equal(t, "Floor 1", details[deviceIDs[1]].Label)
	assert.Equal(t, "100", details[deviceIDs[1]].Position)
}

func TestCollectionStore_GetRackDetailsForDevices_LeavesPositionBlankForUnspecifiedOrderIndex(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupCollectionTestData(t, 1)
	store := newCollectionStore(db)
	ctx := t.Context()

	rack, err := store.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_RACK, "Floor 1", "")
	require.NoError(t, err)
	err = store.CreateRackExtension(ctx, rack.Id, "Floor 1", 12, 12, 0, 0, orgID)
	require.NoError(t, err)
	_, err = store.AddDevicesToCollection(ctx, orgID, rack.Id, deviceIDs)
	require.NoError(t, err)

	err = store.SetRackSlotPosition(ctx, rack.Id, deviceIDs[0], 0, 0, orgID)
	require.NoError(t, err)

	details, err := store.GetRackDetailsForDevices(ctx, orgID, deviceIDs)
	require.NoError(t, err)
	require.Len(t, details, 1)
	assert.Equal(t, "Floor 1", details[deviceIDs[0]].Label)
	assert.Empty(t, details[deviceIDs[0]].Position)
}

func TestCollectionStore_UpdateCollection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, _ := setupCollectionTestData(t, 0)
	store := newCollectionStore(db)
	ctx := t.Context()

	// Arrange
	collection, err := store.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_GROUP, "Original", "Old desc")
	require.NoError(t, err)

	// Act - update only label
	newLabel := "Updated"
	err = store.UpdateCollection(ctx, orgID, collection.Id, &newLabel, nil)
	require.NoError(t, err)

	// Assert
	fetched, err := store.GetCollection(ctx, orgID, collection.Id)
	require.NoError(t, err)
	assert.Equal(t, "Updated", fetched.Label)
	assert.Equal(t, "Old desc", fetched.Description)
}

func TestCollectionStore_CreateCollection_DuplicateName(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, _ := setupCollectionTestData(t, 0)
	store := newCollectionStore(db)
	ctx := t.Context()

	// Arrange - create a collection
	_, err := store.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_GROUP, "My Group", "")
	require.NoError(t, err)

	// Act - create another with the same name
	_, err = store.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_GROUP, "My Group", "")

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "a collection with this name already exists")
}

func TestCollectionStore_UpdateCollection_DuplicateName(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, _ := setupCollectionTestData(t, 0)
	store := newCollectionStore(db)
	ctx := t.Context()

	// Arrange - create two collections
	_, err := store.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_GROUP, "Group A", "")
	require.NoError(t, err)
	collB, err := store.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_GROUP, "Group B", "")
	require.NoError(t, err)

	// Act - rename B to A
	newLabel := "Group A"
	err = store.UpdateCollection(ctx, orgID, collB.Id, &newLabel, nil)

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "a collection with this name already exists")
}

func TestCollectionStore_RackSlotPositions(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupCollectionTestData(t, 2)
	store := newCollectionStore(db)
	ctx := t.Context()

	// Arrange - create rack with devices
	rack, err := store.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_RACK, "Rack", "")
	require.NoError(t, err)
	err = store.CreateRackExtension(ctx, rack.Id, "Floor 1", 4, 8, 0, 0, orgID)
	require.NoError(t, err)
	_, err = store.AddDevicesToCollection(ctx, orgID, rack.Id, deviceIDs)
	require.NoError(t, err)

	// Act - set positions
	err = store.SetRackSlotPosition(ctx, rack.Id, deviceIDs[0], 0, 0, orgID)
	require.NoError(t, err)
	err = store.SetRackSlotPosition(ctx, rack.Id, deviceIDs[1], 1, 3, orgID)
	require.NoError(t, err)

	// Assert - get slots
	slots, err := store.GetRackSlots(ctx, rack.Id, orgID)
	require.NoError(t, err)
	require.Len(t, slots, 2)
	assert.Equal(t, int32(0), slots[0].Position.Row)
	assert.Equal(t, int32(0), slots[0].Position.Column)
	assert.Equal(t, int32(1), slots[1].Position.Row)
	assert.Equal(t, int32(3), slots[1].Position.Column)

	// Act - clear one position
	err = store.ClearRackSlotPosition(ctx, rack.Id, deviceIDs[0], orgID)
	require.NoError(t, err)

	// Assert
	slots, err = store.GetRackSlots(ctx, rack.Id, orgID)
	require.NoError(t, err)
	assert.Len(t, slots, 1)
	assert.Equal(t, deviceIDs[1], slots[0].DeviceIdentifier)
}

func TestCollectionStore_ListCollectionMembers_IncludesSlotPositions(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupCollectionTestData(t, 2)
	store := newCollectionStore(db)
	ctx := t.Context()

	// Arrange - rack with one device positioned
	rack, err := store.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_RACK, "Rack", "")
	require.NoError(t, err)
	err = store.CreateRackExtension(ctx, rack.Id, "", 4, 8, 0, 0, orgID)
	require.NoError(t, err)
	_, err = store.AddDevicesToCollection(ctx, orgID, rack.Id, deviceIDs)
	require.NoError(t, err)
	err = store.SetRackSlotPosition(ctx, rack.Id, deviceIDs[0], 2, 5, orgID)
	require.NoError(t, err)

	// Act
	members, _, err := store.ListCollectionMembers(ctx, orgID, rack.Id, 100, "")
	require.NoError(t, err)
	require.Len(t, members, 2)

	// Assert - find the positioned member
	var positioned, unpositioned *pb.CollectionMember
	for _, m := range members {
		if m.DeviceIdentifier == deviceIDs[0] {
			positioned = m
		} else {
			unpositioned = m
		}
	}

	require.NotNil(t, positioned)
	rackDetails := positioned.GetRack()
	require.NotNil(t, rackDetails)
	assert.Equal(t, int32(2), rackDetails.SlotPosition.Row)
	assert.Equal(t, int32(5), rackDetails.SlotPosition.Column)

	require.NotNil(t, unpositioned)
	assert.Nil(t, unpositioned.GetRack())
}

func TestCollectionStore_ListCollections_Pagination(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, _ := setupCollectionTestData(t, 0)
	store := newCollectionStore(db)
	ctx := t.Context()

	// Arrange - create 5 collections with known labels (sorted alphabetically)
	labels := []string{"Alpha", "Bravo", "Charlie", "Delta", "Echo"}
	for _, label := range labels {
		_, err := store.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_GROUP, label, "")
		require.NoError(t, err)
	}

	// Act - page 1 (size 2)
	page1, token1, _, err := store.ListCollections(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_UNSPECIFIED, 2, "", nil, nil, nil)

	// Assert
	require.NoError(t, err)
	require.Len(t, page1, 2)
	assert.Equal(t, "Alpha", page1[0].Label)
	assert.Equal(t, "Bravo", page1[1].Label)
	assert.NotEmpty(t, token1)

	// Act - page 2
	page2, token2, _, err := store.ListCollections(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_UNSPECIFIED, 2, token1, nil, nil, nil)

	// Assert
	require.NoError(t, err)
	require.Len(t, page2, 2)
	assert.Equal(t, "Charlie", page2[0].Label)
	assert.Equal(t, "Delta", page2[1].Label)
	assert.NotEmpty(t, token2)

	// Act - page 3 (last page, 1 item)
	page3, token3, _, err := store.ListCollections(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_UNSPECIFIED, 2, token2, nil, nil, nil)

	// Assert
	require.NoError(t, err)
	require.Len(t, page3, 1)
	assert.Equal(t, "Echo", page3[0].Label)
	assert.Empty(t, token3)
}

func TestCollectionStore_ListCollections_PaginationWithTypeFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, _ := setupCollectionTestData(t, 0)
	store := newCollectionStore(db)
	ctx := t.Context()

	// Arrange - mix of groups and racks
	_, err := store.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_GROUP, "Group A", "")
	require.NoError(t, err)
	_, err = store.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_RACK, "Rack A", "")
	require.NoError(t, err)
	_, err = store.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_GROUP, "Group B", "")
	require.NoError(t, err)
	_, err = store.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_GROUP, "Group C", "")
	require.NoError(t, err)

	// Act - page through groups with page size 1
	page1, token1, _, err := store.ListCollections(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_GROUP, 1, "", nil, nil, nil)
	require.NoError(t, err)
	require.Len(t, page1, 1)
	assert.Equal(t, "Group A", page1[0].Label)
	assert.NotEmpty(t, token1)

	page2, token2, _, err := store.ListCollections(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_GROUP, 1, token1, nil, nil, nil)
	require.NoError(t, err)
	require.Len(t, page2, 1)
	assert.Equal(t, "Group B", page2[0].Label)
	assert.NotEmpty(t, token2)

	page3, token3, _, err := store.ListCollections(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_GROUP, 1, token2, nil, nil, nil)
	require.NoError(t, err)
	require.Len(t, page3, 1)
	assert.Equal(t, "Group C", page3[0].Label)
	assert.Empty(t, token3)
}

func TestCollectionStore_ListCollections_SortsByIssueCount(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	adminUser := testContext.DatabaseService.CreateSuperAdminUser()
	deviceIDs := testContext.DatabaseService.CreateTestMiners(adminUser.OrganizationID, 3, "https://127.0.0.1:8080")
	db := testContext.DatabaseService.DB
	store := newCollectionStore(db)
	errorStore := newErrorStore(db)
	ctx := t.Context()

	noIssues, err := store.CreateCollection(ctx, adminUser.OrganizationID, pb.CollectionType_COLLECTION_TYPE_GROUP, "No Issues", "")
	require.NoError(t, err)
	oneIssue, err := store.CreateCollection(ctx, adminUser.OrganizationID, pb.CollectionType_COLLECTION_TYPE_GROUP, "One Issue", "")
	require.NoError(t, err)
	threeIssues, err := store.CreateCollection(ctx, adminUser.OrganizationID, pb.CollectionType_COLLECTION_TYPE_GROUP, "Three Issues", "")
	require.NoError(t, err)

	_, err = store.AddDevicesToCollection(ctx, adminUser.OrganizationID, noIssues.Id, []string{deviceIDs[2]})
	require.NoError(t, err)
	_, err = store.AddDevicesToCollection(ctx, adminUser.OrganizationID, oneIssue.Id, []string{deviceIDs[0]})
	require.NoError(t, err)
	_, err = store.AddDevicesToCollection(ctx, adminUser.OrganizationID, threeIssues.Id, []string{deviceIDs[0], deviceIDs[1]})
	require.NoError(t, err)

	_, err = errorStore.UpsertError(ctx, adminUser.OrganizationID, deviceIDs[0], createCollectionIssue(deviceIDs[0], models.FanFailed, models.ComponentTypeFans))
	require.NoError(t, err)
	_, err = errorStore.UpsertError(ctx, adminUser.OrganizationID, deviceIDs[1], createCollectionIssue(deviceIDs[1], models.PSUNotPresent, models.ComponentTypePSU))
	require.NoError(t, err)
	_, err = errorStore.UpsertError(ctx, adminUser.OrganizationID, deviceIDs[1], createCollectionIssue(deviceIDs[1], models.HashboardOverTemperature, models.ComponentTypeHashBoards))
	require.NoError(t, err)

	sort := &sqlstoresinterfaces.SortConfig{
		Field:     sqlstoresinterfaces.SortFieldIssueCount,
		Direction: sqlstoresinterfaces.SortDirectionDesc,
	}

	page1, token, _, err := store.ListCollections(ctx, adminUser.OrganizationID, pb.CollectionType_COLLECTION_TYPE_GROUP, 2, "", sort, nil, nil)
	require.NoError(t, err)
	require.Len(t, page1, 2)
	assert.Equal(t, "Three Issues", page1[0].Label)
	assert.Equal(t, "One Issue", page1[1].Label)
	assert.NotEmpty(t, token)

	page2, token, _, err := store.ListCollections(ctx, adminUser.OrganizationID, pb.CollectionType_COLLECTION_TYPE_GROUP, 2, token, sort, nil, nil)
	require.NoError(t, err)
	require.Len(t, page2, 1)
	assert.Equal(t, "No Issues", page2[0].Label)
	assert.Empty(t, token)
}

func TestCollectionStore_ListCollectionMembers_Pagination(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupCollectionTestData(t, 5)
	store := newCollectionStore(db)
	ctx := t.Context()

	// Arrange
	group, err := store.CreateCollection(ctx, orgID, pb.CollectionType_COLLECTION_TYPE_GROUP, "Big Group", "")
	require.NoError(t, err)
	_, err = store.AddDevicesToCollection(ctx, orgID, group.Id, deviceIDs)
	require.NoError(t, err)

	// Act - page 1 (size 2)
	page1, token1, err := store.ListCollectionMembers(ctx, orgID, group.Id, 2, "")

	// Assert
	require.NoError(t, err)
	require.Len(t, page1, 2)
	assert.NotEmpty(t, token1)

	// Act - page 2
	page2, token2, err := store.ListCollectionMembers(ctx, orgID, group.Id, 2, token1)

	// Assert
	require.NoError(t, err)
	require.Len(t, page2, 2)
	assert.NotEmpty(t, token2)

	// Act - page 3 (last page)
	page3, token3, err := store.ListCollectionMembers(ctx, orgID, group.Id, 2, token2)

	// Assert
	require.NoError(t, err)
	require.Len(t, page3, 1)
	assert.Empty(t, token3)

	// Verify no duplicates across pages
	seen := make(map[string]bool)
	for _, pages := range [][]*pb.CollectionMember{page1, page2, page3} {
		for _, m := range pages {
			assert.False(t, seen[m.DeviceIdentifier], "duplicate device: %s", m.DeviceIdentifier)
			seen[m.DeviceIdentifier] = true
		}
	}
	assert.Len(t, seen, 5)
}
