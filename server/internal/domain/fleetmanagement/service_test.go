package fleetmanagement_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	pairingpb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	minermodels "github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	discoverymodels "github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/btc-mining/proto-fleet/server/internal/testutil"
)

func TestService_ListMinerStateSnapshots_ShouldReturnAllDevices(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	// Arrange
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testUser := testContext.DatabaseService.CreateSuperAdminUser()

	// Create some paired and unpaired devices
	discoveredDeviceStore := sqlstores.NewSQLDiscoveredDeviceStore(testContext.ServiceProvider.DB)

	// Create 2 unpaired devices
	for i := 1; i <= 2; i++ {
		deviceIdentifier := fmt.Sprintf("unpaired-device-%d", i)
		doi := discoverymodels.DeviceOrgIdentifier{
			DeviceIdentifier: deviceIdentifier,
			OrgID:            testUser.OrganizationID,
		}
		device := &discoverymodels.DiscoveredDevice{
			Device: pairingpb.Device{
				DeviceIdentifier: deviceIdentifier,
				Model:            "S19 Pro",
				Manufacturer:     "Bitmain",
				Type:             "ANTMINER",
				IpAddress:        fmt.Sprintf("192.168.1.%d", 100+i),
				Port:             "4028",
				UrlScheme:        "http",
			},
			IsActive: true,
			OrgID:    testUser.OrganizationID,
		}
		_, err := discoveredDeviceStore.Save(t.Context(), doi, device)
		require.NoError(t, err)
	}

	// Create 2 paired devices
	testContext.DatabaseService.CreateTestMiners(testUser.OrganizationID, 2, "https://172.17.0.1:2121")

	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)
	service := testContext.ServiceProvider.FleetManagementService

	req := &pb.ListMinerStateSnapshotsRequest{
		PageSize: 10,
		DataMode: pb.DataMode_DATA_MODE_METADATA,
		// No filter - should return all devices
	}

	// Act
	resp, err := service.ListMinerStateSnapshots(ctx, req)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Miners, 4, "Should return both paired and unpaired devices")
	assert.Equal(t, int32(4), resp.TotalMiners)
	assert.Empty(t, resp.Cursor) // No more pages
}

func TestService_ListMinerStateSnapshots_ShouldFilterByPairingStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	testCases := []struct {
		name                string
		pairingStatuses     []pb.PairingStatus
		expectedCount       int32
		expectedDescription string
	}{
		{
			name:                "Filter for PAIRED only",
			pairingStatuses:     []pb.PairingStatus{pb.PairingStatus_PAIRING_STATUS_PAIRED},
			expectedCount:       2,
			expectedDescription: "Should return only paired devices",
		},
		{
			name:                "Filter for UNPAIRED only",
			pairingStatuses:     []pb.PairingStatus{pb.PairingStatus_PAIRING_STATUS_UNPAIRED},
			expectedCount:       3,
			expectedDescription: "Should return only unpaired devices",
		},
		{
			name:                "Filter for PAIRED and UNPAIRED",
			pairingStatuses:     []pb.PairingStatus{pb.PairingStatus_PAIRING_STATUS_PAIRED, pb.PairingStatus_PAIRING_STATUS_UNPAIRED},
			expectedCount:       5,
			expectedDescription: "Should return both paired and unpaired devices",
		},
		{
			name:                "Empty filter",
			pairingStatuses:     []pb.PairingStatus{},
			expectedCount:       5,
			expectedDescription: "Should return all devices when no filter specified",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			testContext := testutil.InitializeDBServiceInfrastructure(t)
			testUser := testContext.DatabaseService.CreateSuperAdminUser()

			// Create unpaired devices
			discoveredDeviceStore := sqlstores.NewSQLDiscoveredDeviceStore(testContext.ServiceProvider.DB)
			for i := 1; i <= 3; i++ {
				deviceIdentifier := fmt.Sprintf("unpaired-device-%d", i)
				doi := discoverymodels.DeviceOrgIdentifier{
					DeviceIdentifier: deviceIdentifier,
					OrgID:            testUser.OrganizationID,
				}
				device := &discoverymodels.DiscoveredDevice{
					Device: pairingpb.Device{
						DeviceIdentifier: deviceIdentifier,
						Model:            "S19 Pro",
						Manufacturer:     "Bitmain",
						Type:             "ANTMINER",
						IpAddress:        fmt.Sprintf("192.168.1.%d", 100+i),
						Port:             "4028",
						UrlScheme:        "http",
					},
					IsActive: true,
					OrgID:    testUser.OrganizationID,
				}
				_, err := discoveredDeviceStore.Save(t.Context(), doi, device)
				require.NoError(t, err)
			}

			// Create paired devices
			testContext.DatabaseService.CreateTestMiners(testUser.OrganizationID, 2, "https://172.17.0.1:2121")

			ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)
			service := testContext.ServiceProvider.FleetManagementService

			req := &pb.ListMinerStateSnapshotsRequest{
				PageSize: 10,
				DataMode: pb.DataMode_DATA_MODE_METADATA,
				Filter: &pb.MinerListFilter{
					PairingStatuses: tc.pairingStatuses,
				},
			}

			// Act
			resp, err := service.ListMinerStateSnapshots(ctx, req)

			// Assert
			require.NoError(t, err, tc.expectedDescription)
			require.NotNil(t, resp)
			assert.Len(t, resp.Miners, int(tc.expectedCount), tc.expectedDescription)
			assert.Equal(t, tc.expectedCount, resp.TotalMiners)
		})
	}
}

func TestService_ListMinerStateSnapshots_ShouldFilterByDeviceStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	// Arrange
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testUser := testContext.DatabaseService.CreateSuperAdminUser()

	// Create paired devices with different statuses
	deviceIDs := testContext.DatabaseService.CreateTestMiners(testUser.OrganizationID, 3, "https://172.17.0.1:2121")
	deviceStore := sqlstores.NewSQLDeviceStore(testContext.ServiceProvider.DB)

	// Set different device statuses
	err := deviceStore.UpsertDeviceStatus(t.Context(), minermodels.DeviceIdentifier(deviceIDs[0]), minermodels.MinerStatusActive, "")
	require.NoError(t, err)
	err = deviceStore.UpsertDeviceStatus(t.Context(), minermodels.DeviceIdentifier(deviceIDs[1]), minermodels.MinerStatusOffline, "")
	require.NoError(t, err)
	err = deviceStore.UpsertDeviceStatus(t.Context(), minermodels.DeviceIdentifier(deviceIDs[2]), minermodels.MinerStatusError, "")
	require.NoError(t, err)

	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)
	service := testContext.ServiceProvider.FleetManagementService

	// Act - Filter for ONLINE devices only
	req := &pb.ListMinerStateSnapshotsRequest{
		PageSize: 10,
		DataMode: pb.DataMode_DATA_MODE_METADATA,
		Filter: &pb.MinerListFilter{
			DeviceStatus: []pb.DeviceStatus{pb.DeviceStatus_DEVICE_STATUS_ONLINE},
		},
	}
	resp, err := service.ListMinerStateSnapshots(ctx, req)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Miners, 1, "Should return only ONLINE devices")
	assert.Equal(t, pb.DeviceStatus_DEVICE_STATUS_ONLINE, resp.Miners[0].DeviceStatus)
}

func TestService_ListMinerStateSnapshots_ShouldSupportPagination(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	// Arrange
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testUser := testContext.DatabaseService.CreateSuperAdminUser()

	// Create 5 paired devices
	testContext.DatabaseService.CreateTestMiners(testUser.OrganizationID, 5, "https://172.17.0.1:2121")

	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)
	service := testContext.ServiceProvider.FleetManagementService

	// Request with page size of 2
	req := &pb.ListMinerStateSnapshotsRequest{
		PageSize: 2,
		DataMode: pb.DataMode_DATA_MODE_METADATA,
	}

	// Act - Get first page
	resp, err := service.ListMinerStateSnapshots(ctx, req)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Miners, 2, "Should return 2 devices")
	assert.Equal(t, int32(5), resp.TotalMiners, "Total should be 5")
	assert.NotEmpty(t, resp.Cursor, "Should have a cursor for next page")

	// Act - Get second page
	req.Cursor = resp.Cursor
	resp2, err := service.ListMinerStateSnapshots(ctx, req)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, resp2)
	assert.Len(t, resp2.Miners, 2, "Should return 2 more devices")
	assert.NotEmpty(t, resp2.Cursor, "Should have cursor for third page")

	// Verify different devices returned
	assert.NotEqual(t, resp.Miners[0].DeviceIdentifier, resp2.Miners[0].DeviceIdentifier)
}

func TestService_ListMinerStateSnapshots_ShouldUseDefaultPageSize(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	// Arrange
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testUser := testContext.DatabaseService.CreateSuperAdminUser()

	// Create 3 devices
	testContext.DatabaseService.CreateTestMiners(testUser.OrganizationID, 3, "https://172.17.0.1:2121")

	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)
	service := testContext.ServiceProvider.FleetManagementService

	// Request with page size of 0 (should use default of 50)
	req := &pb.ListMinerStateSnapshotsRequest{
		PageSize: 0,
		DataMode: pb.DataMode_DATA_MODE_METADATA,
	}

	// Act
	resp, err := service.ListMinerStateSnapshots(ctx, req)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Miners, 3, "Should return all 3 devices")
	assert.Empty(t, resp.Cursor, "Should not have a cursor (all fit in default page size)")
}

func TestService_ListMinerStateSnapshots_ShouldCapMaxPageSize(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	// Arrange
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testUser := testContext.DatabaseService.CreateSuperAdminUser()

	// Create 2 devices
	testContext.DatabaseService.CreateTestMiners(testUser.OrganizationID, 2, "https://172.17.0.1:2121")

	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)
	service := testContext.ServiceProvider.FleetManagementService

	// Request with very large page size (should be capped to max of 1000)
	req := &pb.ListMinerStateSnapshotsRequest{
		PageSize: 5000,
		DataMode: pb.DataMode_DATA_MODE_METADATA,
	}

	// Act
	resp, err := service.ListMinerStateSnapshots(ctx, req)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, resp)
	// Should successfully return results (not fail due to large page size)
	assert.Len(t, resp.Miners, 2)
}

func TestService_ListMinerStateSnapshots_ShouldReturnEmptyForNoDevices(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	// Arrange
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testUser := testContext.DatabaseService.CreateSuperAdminUser()
	// Don't create any devices

	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)
	service := testContext.ServiceProvider.FleetManagementService

	req := &pb.ListMinerStateSnapshotsRequest{
		PageSize: 10,
		DataMode: pb.DataMode_DATA_MODE_METADATA,
	}

	// Act
	resp, err := service.ListMinerStateSnapshots(ctx, req)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Empty(t, resp.Miners, "Should return empty list")
	assert.Equal(t, int32(0), resp.TotalMiners)
	assert.Empty(t, resp.Cursor)
}

func TestService_ListMinerStateSnapshots_ShouldCombineMultipleFilters(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	// Arrange
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testUser := testContext.DatabaseService.CreateSuperAdminUser()

	// Create paired Proto miners
	deviceIDs := testContext.DatabaseService.CreateTestMiners(testUser.OrganizationID, 2, "https://172.17.0.1:2121")
	deviceStore := sqlstores.NewSQLDeviceStore(testContext.ServiceProvider.DB)

	// Set device status to ONLINE
	err := deviceStore.UpsertDeviceStatus(t.Context(), minermodels.DeviceIdentifier(deviceIDs[0]), minermodels.MinerStatusActive, "")
	require.NoError(t, err)

	// Create unpaired Antminer
	discoveredDeviceStore := sqlstores.NewSQLDiscoveredDeviceStore(testContext.ServiceProvider.DB)
	deviceIdentifier := "antminer-unpaired"
	doi := discoverymodels.DeviceOrgIdentifier{
		DeviceIdentifier: deviceIdentifier,
		OrgID:            testUser.OrganizationID,
	}
	device := &discoverymodels.DiscoveredDevice{
		Device: pairingpb.Device{
			DeviceIdentifier: deviceIdentifier,
			Model:            "S19 Pro",
			Manufacturer:     "Bitmain",
			Type:             "ANTMINER",
			IpAddress:        "192.168.1.200",
			Port:             "4028",
			UrlScheme:        "http",
		},
		IsActive: true,
		OrgID:    testUser.OrganizationID,
	}
	_, err = discoveredDeviceStore.Save(t.Context(), doi, device)
	require.NoError(t, err)

	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)
	service := testContext.ServiceProvider.FleetManagementService

	// Act - Filter for PAIRED devices with ONLINE status
	req := &pb.ListMinerStateSnapshotsRequest{
		PageSize: 10,
		DataMode: pb.DataMode_DATA_MODE_METADATA,
		Filter: &pb.MinerListFilter{
			PairingStatuses: []pb.PairingStatus{pb.PairingStatus_PAIRING_STATUS_PAIRED},
			DeviceStatus:    []pb.DeviceStatus{pb.DeviceStatus_DEVICE_STATUS_ONLINE},
		},
	}
	resp, err := service.ListMinerStateSnapshots(ctx, req)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Miners, 1, "Should return only PAIRED devices with ONLINE status")
	assert.Equal(t, pb.PairingStatus_PAIRING_STATUS_PAIRED, resp.Miners[0].PairingStatus)
	assert.Equal(t, pb.DeviceStatus_DEVICE_STATUS_ONLINE, resp.Miners[0].DeviceStatus)
}
