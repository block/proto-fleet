package fleetmanagement_test

import (
	"testing"

	collectionpb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestService_ListMinerStateSnapshots_ShouldFilterByGroupID(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	// Arrange
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testUser := testContext.DatabaseService.CreateSuperAdminUser()
	orgID := testUser.OrganizationID

	deviceIDs := testContext.DatabaseService.CreateTestMiners(orgID, 3, "https://172.17.0.1:80")

	// Create a group and add only the first 2 devices
	collectionStore := sqlstores.NewSQLCollectionStore(testContext.ServiceProvider.DB)
	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, orgID)

	group, err := collectionStore.CreateCollection(t.Context(), orgID, collectionpb.CollectionType_COLLECTION_TYPE_GROUP, "Floor A", "")
	require.NoError(t, err)
	_, err = collectionStore.AddDevicesToCollection(t.Context(), orgID, group.Id, deviceIDs[:2])
	require.NoError(t, err)

	service := testContext.ServiceProvider.FleetManagementService

	// Act - filter by the group
	resp, err := service.ListMinerStateSnapshots(ctx, &pb.ListMinerStateSnapshotsRequest{
		PageSize: 10,
		Filter: &pb.MinerListFilter{
			GroupIds: []int64{group.Id},
		},
	})

	// Assert
	require.NoError(t, err)
	assert.Len(t, resp.Miners, 2, "should return only the 2 devices in the group")
	assert.Equal(t, int32(2), resp.TotalMiners, "total count should match filtered list length")

	returnedIDs := make([]string, len(resp.Miners))
	for i, m := range resp.Miners {
		returnedIDs[i] = m.DeviceIdentifier
	}
	assert.ElementsMatch(t, deviceIDs[:2], returnedIDs)
}

func TestService_ListMinerStateSnapshots_ShouldReturnZeroTotalForEmptyGroupFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	// Arrange
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testUser := testContext.DatabaseService.CreateSuperAdminUser()
	orgID := testUser.OrganizationID

	// Create 3 miners but don't add any to the group
	testContext.DatabaseService.CreateTestMiners(orgID, 3, "https://172.17.0.1:80")

	collectionStore := sqlstores.NewSQLCollectionStore(testContext.ServiceProvider.DB)
	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, orgID)

	group, err := collectionStore.CreateCollection(t.Context(), orgID, collectionpb.CollectionType_COLLECTION_TYPE_GROUP, "Empty Group", "")
	require.NoError(t, err)

	service := testContext.ServiceProvider.FleetManagementService

	// Act - filter by the empty group
	resp, err := service.ListMinerStateSnapshots(ctx, &pb.ListMinerStateSnapshotsRequest{
		PageSize: 10,
		Filter: &pb.MinerListFilter{
			GroupIds: []int64{group.Id},
		},
	})

	// Assert - both list and total should be zero
	require.NoError(t, err)
	assert.Empty(t, resp.Miners, "should return no devices for empty group")
	assert.Equal(t, int32(0), resp.TotalMiners, "total count should be 0 for empty group filter")
}

func TestService_ListMinerStateSnapshots_ShouldFilterByGroupAndRackWithANDLogic(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	// Arrange
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testUser := testContext.DatabaseService.CreateSuperAdminUser()
	orgID := testUser.OrganizationID

	// Create 3 devices: A, B, C
	deviceIDs := testContext.DatabaseService.CreateTestMiners(orgID, 3, "https://172.17.0.1:80")

	collectionStore := sqlstores.NewSQLCollectionStore(testContext.ServiceProvider.DB)
	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, orgID)

	// Group contains A, B
	group, err := collectionStore.CreateCollection(t.Context(), orgID, collectionpb.CollectionType_COLLECTION_TYPE_GROUP, "Group 1", "")
	require.NoError(t, err)
	_, err = collectionStore.AddDevicesToCollection(t.Context(), orgID, group.Id, deviceIDs[:2])
	require.NoError(t, err)

	// Rack contains B, C
	rack, err := collectionStore.CreateCollection(t.Context(), orgID, collectionpb.CollectionType_COLLECTION_TYPE_RACK, "Rack 1", "")
	require.NoError(t, err)
	_, err = collectionStore.AddDevicesToCollection(t.Context(), orgID, rack.Id, deviceIDs[1:])
	require.NoError(t, err)

	service := testContext.ServiceProvider.FleetManagementService

	// Act - filter by both group AND rack (AND logic → only B matches both)
	resp, err := service.ListMinerStateSnapshots(ctx, &pb.ListMinerStateSnapshotsRequest{
		PageSize: 10,
		Filter: &pb.MinerListFilter{
			GroupIds: []int64{group.Id},
			RackIds:  []int64{rack.Id},
		},
	})

	// Assert - only device B is in both group and rack
	require.NoError(t, err)
	require.Len(t, resp.Miners, 1, "AND logic: only device in both group and rack should match")
	assert.Equal(t, deviceIDs[1], resp.Miners[0].DeviceIdentifier)
}

func TestService_ListMinerStateSnapshots_ShouldPopulateGroupLabels(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	// Arrange
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testUser := testContext.DatabaseService.CreateSuperAdminUser()
	orgID := testUser.OrganizationID

	deviceIDs := testContext.DatabaseService.CreateTestMiners(orgID, 2, "https://172.17.0.1:80")

	collectionStore := sqlstores.NewSQLCollectionStore(testContext.ServiceProvider.DB)
	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, orgID)

	// Device 0 in 2 groups, device 1 in 1 group
	groupA, err := collectionStore.CreateCollection(t.Context(), orgID, collectionpb.CollectionType_COLLECTION_TYPE_GROUP, "Alpha", "")
	require.NoError(t, err)
	groupB, err := collectionStore.CreateCollection(t.Context(), orgID, collectionpb.CollectionType_COLLECTION_TYPE_GROUP, "Beta", "")
	require.NoError(t, err)
	_, err = collectionStore.AddDevicesToCollection(t.Context(), orgID, groupA.Id, deviceIDs)
	require.NoError(t, err)
	_, err = collectionStore.AddDevicesToCollection(t.Context(), orgID, groupB.Id, deviceIDs[:1])
	require.NoError(t, err)

	service := testContext.ServiceProvider.FleetManagementService

	// Act
	resp, err := service.ListMinerStateSnapshots(ctx, &pb.ListMinerStateSnapshotsRequest{
		PageSize: 10,
		Filter: &pb.MinerListFilter{
			PairingStatuses: []pb.PairingStatus{pb.PairingStatus_PAIRING_STATUS_PAIRED},
		},
	})

	// Assert
	require.NoError(t, err)
	require.Len(t, resp.Miners, 2)

	// Build map of device -> group labels
	labelsByDevice := make(map[string][]string)
	for _, m := range resp.Miners {
		labelsByDevice[m.DeviceIdentifier] = m.GroupLabels
	}

	assert.Len(t, labelsByDevice[deviceIDs[0]], 2)
	assert.ElementsMatch(t, []string{"Alpha", "Beta"}, labelsByDevice[deviceIDs[0]])
	assert.Equal(t, []string{"Alpha"}, labelsByDevice[deviceIDs[1]])
}

func TestService_ListMinerStateSnapshots_ShouldPopulateRackLabel(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	// Arrange
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testUser := testContext.DatabaseService.CreateSuperAdminUser()
	orgID := testUser.OrganizationID

	deviceIDs := testContext.DatabaseService.CreateTestMiners(orgID, 2, "https://172.17.0.1:80")

	collectionStore := sqlstores.NewSQLCollectionStore(testContext.ServiceProvider.DB)
	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, orgID)

	// Only device 0 in a rack
	rack, err := collectionStore.CreateCollection(t.Context(), orgID, collectionpb.CollectionType_COLLECTION_TYPE_RACK, "Floor 1", "")
	require.NoError(t, err)
	_, err = collectionStore.AddDevicesToCollection(t.Context(), orgID, rack.Id, deviceIDs[:1])
	require.NoError(t, err)

	service := testContext.ServiceProvider.FleetManagementService

	// Act
	resp, err := service.ListMinerStateSnapshots(ctx, &pb.ListMinerStateSnapshotsRequest{
		PageSize: 10,
		Filter: &pb.MinerListFilter{
			PairingStatuses: []pb.PairingStatus{pb.PairingStatus_PAIRING_STATUS_PAIRED},
		},
	})

	// Assert
	require.NoError(t, err)
	require.Len(t, resp.Miners, 2)

	rackByDevice := make(map[string]string)
	for _, m := range resp.Miners {
		rackByDevice[m.DeviceIdentifier] = m.RackLabel
	}

	assert.Equal(t, "Floor 1", rackByDevice[deviceIDs[0]])
	assert.Empty(t, rackByDevice[deviceIDs[1]], "device not in a rack should have empty rack label")
}
