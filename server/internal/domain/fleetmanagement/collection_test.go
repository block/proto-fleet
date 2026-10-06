package fleetmanagement_test

import (
	"testing"

	collectionpb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	sitesmodels "github.com/block/proto-fleet/server/internal/domain/sites/models"
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

func TestService_ListMinerStateSnapshots_ShouldPopulateGroupRefs(t *testing.T) {
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

	// Build map of device -> group refs from placement refs.
	labelsByDevice := make(map[string][]string)
	idsByDevice := make(map[string][]int64)
	for _, m := range resp.Miners {
		if m.Placement == nil {
			continue
		}
		for _, group := range m.Placement.Groups {
			labelsByDevice[m.DeviceIdentifier] = append(labelsByDevice[m.DeviceIdentifier], group.Label)
			idsByDevice[m.DeviceIdentifier] = append(idsByDevice[m.DeviceIdentifier], group.Id)
		}
	}

	assert.Len(t, labelsByDevice[deviceIDs[0]], 2)
	assert.ElementsMatch(t, []string{"Alpha", "Beta"}, labelsByDevice[deviceIDs[0]])
	assert.ElementsMatch(t, []int64{groupA.Id, groupB.Id}, idsByDevice[deviceIDs[0]])
	assert.Equal(t, []string{"Alpha"}, labelsByDevice[deviceIDs[1]])
	assert.Equal(t, []int64{groupA.Id}, idsByDevice[deviceIDs[1]])
}

func TestService_ListMinerStateSnapshots_ShouldPopulateRackRef(t *testing.T) {
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
	rackIDsByDevice := make(map[string]int64)
	for _, m := range resp.Miners {
		if m.Placement != nil && m.Placement.Rack != nil {
			rackByDevice[m.DeviceIdentifier] = m.Placement.Rack.Label
			rackIDsByDevice[m.DeviceIdentifier] = m.Placement.Rack.Id
		}
	}

	assert.Equal(t, "Floor 1", rackByDevice[deviceIDs[0]])
	assert.Equal(t, rack.Id, rackIDsByDevice[deviceIDs[0]])
	assert.Empty(t, rackByDevice[deviceIDs[1]], "device not in a rack should have empty rack ref")
	assert.Zero(t, rackIDsByDevice[deviceIDs[1]], "device not in a rack should have empty rack ref")
}

// TestService_ListMinerStateSnapshots_ShouldPopulateSiteRef
// closes issue #197's acceptance criteria: "Integration test asserts a
// snapshot written after device site-assignment carries the right site."
// Creates a site, reassigns devices to it via the SiteStore bulk path,
// and verifies the snapshot response carries a placement site ref without
// a second round-trip.
func TestService_ListMinerStateSnapshots_ShouldPopulateSiteRef(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testUser := testContext.DatabaseService.CreateSuperAdminUser()
	orgID := testUser.OrganizationID

	deviceIDs := testContext.DatabaseService.CreateTestMiners(orgID, 2, "https://172.17.0.1:80")

	siteStore := sqlstores.NewSQLSiteStore(testContext.ServiceProvider.DB)
	site, err := siteStore.CreateSite(t.Context(), sitesmodels.CreateSiteParams{
		OrgID: orgID,
		Name:  "Austin",
	})
	require.NoError(t, err)

	// Reassign only device 0 to the site; device 1 stays unassigned.
	_, err = siteStore.AssignDevicesToSite(t.Context(), orgID, &site.ID, deviceIDs[:1])
	require.NoError(t, err)

	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, orgID)
	service := testContext.ServiceProvider.FleetManagementService

	resp, err := service.ListMinerStateSnapshots(ctx, &pb.ListMinerStateSnapshotsRequest{
		PageSize: 10,
		Filter: &pb.MinerListFilter{
			PairingStatuses: []pb.PairingStatus{pb.PairingStatus_PAIRING_STATUS_PAIRED},
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Miners, 2)

	byDevice := make(map[string]*pb.MinerStateSnapshot, len(resp.Miners))
	for _, m := range resp.Miners {
		byDevice[m.DeviceIdentifier] = m
	}

	assigned := byDevice[deviceIDs[0]]
	require.NotNil(t, assigned)
	require.NotNil(t, assigned.Placement, "assigned device must surface placement")
	require.NotNil(t, assigned.Placement.Site, "assigned device must surface site ref")
	assert.Equal(t, site.ID, assigned.Placement.Site.Id)
	assert.Equal(t, "Austin", assigned.Placement.Site.Label)

	unassigned := byDevice[deviceIDs[1]]
	require.NotNil(t, unassigned)
	assert.Nil(t, unassigned.Placement, "unassigned device must have nil placement")
}
