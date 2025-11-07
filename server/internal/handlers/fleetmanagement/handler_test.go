package fleetmanagement_test

import (
	"fmt"
	"testing"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	pairingpb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	discoverymodels "github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/btc-mining/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandler_ListMinerStateSnapshots(t *testing.T) {
	tests := []struct {
		name         string
		minerURLs    []string
		dataMode     pb.DataMode
		expectedURLs []string
	}{
		{
			name: "Proto miner with HTTPS",
			minerURLs: []string{
				"https://172.17.0.1:2121",
			},
			dataMode: pb.DataMode_DATA_MODE_SNAPSHOT,
			expectedURLs: []string{
				"https://172.17.0.1",
			},
		},
		{
			name: "Miner with HTTP",
			minerURLs: []string{
				"http://172.17.0.2:2121",
			},
			dataMode: pb.DataMode_DATA_MODE_SNAPSHOT,
			expectedURLs: []string{
				"http://172.17.0.2",
			},
		},
		{
			name: "Antminer",
			minerURLs: []string{
				"http://172.17.0.3:4028",
			},
			dataMode: pb.DataMode_DATA_MODE_SNAPSHOT,
			expectedURLs: []string{
				"http://172.17.0.3",
			},
		},
		{
			name: "Multiple miners",
			minerURLs: []string{
				"https://172.17.0.1:2121",
				"http://172.17.0.2:2121",
				"http://172.17.0.3:4028",
			},
			dataMode: pb.DataMode_DATA_MODE_SNAPSHOT,
			expectedURLs: []string{
				"https://172.17.0.1",
				"http://172.17.0.2",
				"http://172.17.0.3",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			testContext := testutil.InitializeDBServiceInfrastructure(t)
			testUser := testContext.DatabaseService.CreateSuperAdminUser()

			minerIDs := make([]string, len(tc.minerURLs))
			for i, url := range tc.minerURLs {
				minerIDs[i] = testContext.DatabaseService.CreateTestMiners(testUser.OrganizationID, 1, url)[0]
			}

			ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)
			service := testContext.ServiceProvider.FleetManagementService

			req := &pb.ListMinerStateSnapshotsRequest{
				PageSize: 5,
				DataMode: tc.dataMode,
			}

			// Act
			resp, err := service.ListMinerStateSnapshots(ctx, req)

			// Assert
			require.NoError(t, err)
			require.Len(t, resp.Miners, len(tc.minerURLs))

			for i, miner := range resp.Miners {
				assert.Equal(t, miner.DeviceIdentifier, minerIDs[i])
				assert.Equal(t, tc.expectedURLs[i], miner.Url)
			}
		})
	}
}

func TestHandler_ListUnpairedDevices(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	// Arrange
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testUser := testContext.DatabaseService.CreateSuperAdminUser()

	// Create some unpaired discovered devices using the pairing service to discover them
	discoveredDeviceStore := sqlstores.NewSQLDiscoveredDeviceStore(testContext.ServiceProvider.DB)
	for i := 1; i <= 3; i++ {
		deviceIdentifier := fmt.Sprintf("test-device-%d", i)
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

	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)
	service := testContext.ServiceProvider.FleetManagementService

	req := &pb.ListUnpairedDevicesRequest{
		PageSize: 10,
	}

	// Act
	resp, err := service.ListUnpairedDevices(ctx, req)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Devices, 3)
	assert.Equal(t, int32(3), resp.TotalDevices)
	assert.Empty(t, resp.Cursor) // No more pages

	// Verify device fields are populated
	for _, device := range resp.Devices {
		assert.NotEmpty(t, device.IpAddress)
		assert.NotEmpty(t, device.Port)
		assert.NotEmpty(t, device.UrlScheme)
		assert.NotEmpty(t, device.Type)
	}
}

func TestHandler_ListUnpairedDevices_ShouldSupportPagination(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	// Arrange
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testUser := testContext.DatabaseService.CreateSuperAdminUser()

	// Create 5 unpaired discovered devices
	discoveredDeviceStore := sqlstores.NewSQLDiscoveredDeviceStore(testContext.ServiceProvider.DB)
	for i := 1; i <= 5; i++ {
		deviceIdentifier := fmt.Sprintf("page-device-%d", i)
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

	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)
	service := testContext.ServiceProvider.FleetManagementService

	// Request with page size of 2
	req := &pb.ListUnpairedDevicesRequest{
		PageSize: 2,
	}

	// Act - Get first page
	resp, err := service.ListUnpairedDevices(ctx, req)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Devices, 2, "Should return 2 devices")
	assert.Equal(t, int32(5), resp.TotalDevices)
	assert.NotEmpty(t, resp.Cursor, "Should have a cursor for next page")
}
