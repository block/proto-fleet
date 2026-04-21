package fleetmanagement_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	capabilitiespb "github.com/block/proto-fleet/server/generated/grpc/capabilities/v1"
	commonv1 "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	errorsv1 "github.com/block/proto-fleet/server/generated/grpc/errors/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	diagnosticsmodels "github.com/block/proto-fleet/server/internal/domain/diagnostics/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetmanagement"
	minermodels "github.com/block/proto-fleet/server/internal/domain/miner/models"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	pairingmocks "github.com/block/proto-fleet/server/internal/domain/pairing/mocks"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
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
				DriverName:       "ANTMINER",
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
	testContext.DatabaseService.CreateTestMiners(testUser.OrganizationID, 2, "https://172.17.0.1:80")

	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)
	service := testContext.ServiceProvider.FleetManagementService

	req := &pb.ListMinerStateSnapshotsRequest{
		PageSize: 10,
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
						DriverName:       "ANTMINER",
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
			testContext.DatabaseService.CreateTestMiners(testUser.OrganizationID, 2, "https://172.17.0.1:80")

			ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)
			service := testContext.ServiceProvider.FleetManagementService

			req := &pb.ListMinerStateSnapshotsRequest{
				PageSize: 10,
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

func TestService_AddMinersFlowShouldUseUnpairedFilterForDiscoveredCount(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testUser := testContext.DatabaseService.CreateSuperAdminUser()

	// Existing fleet miners that should not be counted by the add-miners CTA.
	testContext.DatabaseService.CreateTestMiners(testUser.OrganizationID, 4, "https://172.17.0.1:80")

	// Newly discovered miners that are candidates for the add-miners flow.
	discoveredDeviceStore := sqlstores.NewSQLDiscoveredDeviceStore(testContext.ServiceProvider.DB)
	for i := 1; i <= 4; i++ {
		deviceIdentifier := fmt.Sprintf("newly-discovered-device-%d", i)
		doi := discoverymodels.DeviceOrgIdentifier{
			DeviceIdentifier: deviceIdentifier,
			OrgID:            testUser.OrganizationID,
		}
		device := &discoverymodels.DiscoveredDevice{
			Device: pairingpb.Device{
				DeviceIdentifier: deviceIdentifier,
				Model:            "Proto Rig",
				Manufacturer:     "Proto",
				DriverName:       "proto",
				IpAddress:        fmt.Sprintf("192.168.50.%d", 100+i),
				Port:             "80",
				UrlScheme:        "https",
			},
			IsActive: true,
			OrgID:    testUser.OrganizationID,
		}
		_, err := discoveredDeviceStore.Save(t.Context(), doi, device)
		require.NoError(t, err)
	}

	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)
	service := testContext.ServiceProvider.FleetManagementService

	discoveredCount, err := discoveredDeviceStore.CountActiveUnpairedDevices(ctx, testUser.OrganizationID)
	require.NoError(t, err)

	allDevicesResp, err := service.ListMinerStateSnapshots(ctx, &pb.ListMinerStateSnapshotsRequest{
		PageSize: 20,
	})
	require.NoError(t, err)

	unpairedResp, err := service.ListMinerStateSnapshots(ctx, &pb.ListMinerStateSnapshotsRequest{
		PageSize: 20,
		Filter: &pb.MinerListFilter{
			PairingStatuses: []pb.PairingStatus{pb.PairingStatus_PAIRING_STATUS_UNPAIRED},
		},
	})
	require.NoError(t, err)

	assert.Equal(t, int64(4), discoveredCount, "discovery should report only newly found miners")
	assert.Equal(t, int32(4), unpairedResp.TotalMiners, "UNPAIRED filter matches the discovery count")
	assert.Equal(t, int32(8), allDevicesResp.TotalMiners, "unfiltered miner list should include both fleet and newly discovered miners")
}

func TestService_ListMinerStateSnapshots_ShouldFilterByDeviceStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	// Arrange
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testUser := testContext.DatabaseService.CreateSuperAdminUser()

	// Create paired devices with different statuses
	deviceIDs := testContext.DatabaseService.CreateTestMiners(testUser.OrganizationID, 3, "https://172.17.0.1:80")
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
	testContext.DatabaseService.CreateTestMiners(testUser.OrganizationID, 5, "https://172.17.0.1:80")

	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)
	service := testContext.ServiceProvider.FleetManagementService

	// Request with page size of 2
	req := &pb.ListMinerStateSnapshotsRequest{
		PageSize: 2,
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
	testContext.DatabaseService.CreateTestMiners(testUser.OrganizationID, 3, "https://172.17.0.1:80")

	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)
	service := testContext.ServiceProvider.FleetManagementService

	// Request with page size of 0 (should use default of 50)
	req := &pb.ListMinerStateSnapshotsRequest{
		PageSize: 0,
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
	testContext.DatabaseService.CreateTestMiners(testUser.OrganizationID, 2, "https://172.17.0.1:80")

	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)
	service := testContext.ServiceProvider.FleetManagementService

	// Request with very large page size (should be capped to max of 1000)
	req := &pb.ListMinerStateSnapshotsRequest{
		PageSize: 5000,
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
	deviceIDs := testContext.DatabaseService.CreateTestMiners(testUser.OrganizationID, 2, "https://172.17.0.1:80")
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
			DriverName:       "ANTMINER",
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

func TestService_ListMinerStateSnapshots_ShouldFilterByErrorComponentTypes(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	testCases := []struct {
		name                string
		errorComponentTypes []errorsv1.ComponentType
		expectedCount       int
		expectedDescription string
	}{
		{
			name:                "Filter for PSU errors only",
			errorComponentTypes: []errorsv1.ComponentType{errorsv1.ComponentType_COMPONENT_TYPE_PSU},
			expectedCount:       1,
			expectedDescription: "Should return only devices with PSU errors",
		},
		{
			name:                "Filter for FAN errors only",
			errorComponentTypes: []errorsv1.ComponentType{errorsv1.ComponentType_COMPONENT_TYPE_FAN},
			expectedCount:       1,
			expectedDescription: "Should return only devices with FAN errors",
		},
		{
			name:                "Filter for HASH_BOARD errors only",
			errorComponentTypes: []errorsv1.ComponentType{errorsv1.ComponentType_COMPONENT_TYPE_HASH_BOARD},
			expectedCount:       1,
			expectedDescription: "Should return only devices with HASH_BOARD errors",
		},
		{
			name:                "Filter for multiple component types (PSU and FAN)",
			errorComponentTypes: []errorsv1.ComponentType{errorsv1.ComponentType_COMPONENT_TYPE_PSU, errorsv1.ComponentType_COMPONENT_TYPE_FAN},
			expectedCount:       2,
			expectedDescription: "Should return devices with PSU or FAN errors",
		},
		{
			name:                "Filter for CONTROL_BOARD errors (no matching devices)",
			errorComponentTypes: []errorsv1.ComponentType{errorsv1.ComponentType_COMPONENT_TYPE_CONTROL_BOARD},
			expectedCount:       0,
			expectedDescription: "Should return no devices when no errors match",
		},
		{
			name:                "Empty filter",
			errorComponentTypes: []errorsv1.ComponentType{},
			expectedCount:       4,
			expectedDescription: "Should return all devices when no filter specified",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			testContext := testutil.InitializeDBServiceInfrastructure(t)
			testUser := testContext.DatabaseService.CreateSuperAdminUser()

			// Create 4 miners: 1 with PSU error, 1 with FAN error, 1 with HASH_BOARD error, 1 with no errors
			deviceIDs := testContext.DatabaseService.CreateTestMiners(testUser.OrganizationID, 4, "https://172.17.0.1:80")

			// Create error store
			transactor := sqlstores.NewSQLTransactor(testContext.ServiceProvider.DB)
			errorStore := sqlstores.NewSQLErrorStore(testContext.ServiceProvider.DB, transactor)
			ctx := t.Context()

			// Helper function to create component ID
			makeComponentID := func(deviceIdx int, componentType string, componentIdx int) string {
				return fmt.Sprintf("%d_%s_%d", deviceIdx, componentType, componentIdx)
			}

			// Create PSU error for device 0
			psuComponentID := makeComponentID(0, "psu", 0)
			_, err := errorStore.UpsertError(ctx, testUser.OrganizationID, deviceIDs[0], &diagnosticsmodels.ErrorMessage{
				MinerError:        diagnosticsmodels.PSUFaultGeneric,
				Severity:          diagnosticsmodels.SeverityMajor,
				Summary:           "PSU fault detected",
				Impact:            "Reduced power efficiency",
				CauseSummary:      "Power supply unit malfunction",
				RecommendedAction: "Check PSU connections",
				FirstSeenAt:       time.Now().Add(-time.Hour),
				LastSeenAt:        time.Now(),
				DeviceID:          deviceIDs[0],
				ComponentID:       &psuComponentID,
				ComponentType:     diagnosticsmodels.ComponentTypePSU,
			})
			require.NoError(t, err)

			// Create FAN error for device 1
			fanComponentID := makeComponentID(1, "fan", 0)
			_, err = errorStore.UpsertError(ctx, testUser.OrganizationID, deviceIDs[1], &diagnosticsmodels.ErrorMessage{
				MinerError:        diagnosticsmodels.FanFailed,
				Severity:          diagnosticsmodels.SeverityMajor,
				Summary:           "Fan failure detected",
				Impact:            "Increased temperature risk",
				CauseSummary:      "Fan motor failure",
				RecommendedAction: "Replace faulty fan",
				FirstSeenAt:       time.Now().Add(-time.Hour),
				LastSeenAt:        time.Now(),
				DeviceID:          deviceIDs[1],
				ComponentID:       &fanComponentID,
				ComponentType:     diagnosticsmodels.ComponentTypeFans,
			})
			require.NoError(t, err)

			// Create HASH_BOARD error for device 2
			hashboardComponentID := makeComponentID(2, "hashboard", 0)
			_, err = errorStore.UpsertError(ctx, testUser.OrganizationID, deviceIDs[2], &diagnosticsmodels.ErrorMessage{
				MinerError:        diagnosticsmodels.HashboardOverTemperature,
				Severity:          diagnosticsmodels.SeverityCritical,
				Summary:           "Hashboard over temperature",
				Impact:            "Reduced hashrate",
				CauseSummary:      "Cooling system inadequate",
				RecommendedAction: "Improve cooling",
				FirstSeenAt:       time.Now().Add(-time.Hour),
				LastSeenAt:        time.Now(),
				DeviceID:          deviceIDs[2],
				ComponentID:       &hashboardComponentID,
				ComponentType:     diagnosticsmodels.ComponentTypeHashBoards,
			})
			require.NoError(t, err)

			// Device 3 has no errors

			// Create auth context and service
			authCtx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)
			service := testContext.ServiceProvider.FleetManagementService

			// Act
			req := &pb.ListMinerStateSnapshotsRequest{
				PageSize: 10,
				Filter: &pb.MinerListFilter{
					ErrorComponentTypes: tc.errorComponentTypes,
				},
			}
			resp, err := service.ListMinerStateSnapshots(authCtx, req)

			// Assert
			require.NoError(t, err, tc.expectedDescription)
			require.NotNil(t, resp)
			assert.Len(t, resp.Miners, tc.expectedCount, tc.expectedDescription)

			// Verify the returned miners have the expected errors if filtering was applied
			if len(tc.errorComponentTypes) > 0 && tc.expectedCount > 0 {
				for _, miner := range resp.Miners {
					// The miner should have an error status since it has component errors
					// Note: The actual error details would be in the error service, not directly in the miner snapshot
					assert.NotNil(t, miner, "Returned miner should not be nil")
				}
			}
		})
	}
}

func TestService_ListMinerStateSnapshots_ShouldPopulateCapabilitiesForPairedDevices(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	// Arrange
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testUser := testContext.DatabaseService.CreateSuperAdminUser()

	testContext.DatabaseService.CreateTestMiners(testUser.OrganizationID, 2, "https://172.17.0.1:80")

	mockCapabilities := pairingmocks.NewMockCapabilitiesProvider(ctrl)

	// Expected capabilities for Proto miners
	protoCapabilities := &capabilitiespb.MinerCapabilities{
		Manufacturer: "Proto",
		Telemetry: &capabilitiespb.TelemetryCapabilities{
			HashrateReported:    true,
			PowerUsageReported:  true,
			TemperatureReported: true,
			EfficiencyReported:  true,
			FanSpeedReported:    true,
		},
		Commands: &capabilitiespb.CommandCapabilities{
			RebootSupported:      true,
			MiningStartSupported: true,
			MiningStopSupported:  true,
		},
	}

	mockCapabilities.EXPECT().
		GetMinerCapabilitiesForDevice(gomock.Any(), gomock.Any()).
		Return(protoCapabilities).
		Times(1) // Called once, then cached for second device with same manufacturer/model

	deviceStore := sqlstores.NewSQLDeviceStore(testContext.ServiceProvider.DB)
	discoveredDeviceStore := sqlstores.NewSQLDiscoveredDeviceStore(testContext.ServiceProvider.DB)
	poolStore := sqlstores.NewSQLPoolStore(testContext.ServiceProvider.DB, testContext.ServiceProvider.EncryptService)
	errorStore := sqlstores.NewSQLErrorStore(testContext.ServiceProvider.DB, sqlstores.NewSQLTransactor(testContext.ServiceProvider.DB))
	collectionStore := sqlstores.NewSQLCollectionStore(testContext.ServiceProvider.DB)
	activitySvc := activity.NewService(sqlstores.NewSQLActivityStore(testContext.ServiceProvider.DB))
	service := fleetmanagement.NewService(
		deviceStore,
		discoveredDeviceStore,
		fleetmanagement.NewMockTelemetryCollector(),
		testContext.ServiceProvider.MinerService,
		mockCapabilities,
		poolStore,
		errorStore,
		collectionStore,
		nil,
		activitySvc,
	)

	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)

	req := &pb.ListMinerStateSnapshotsRequest{
		PageSize: 10,
		Filter: &pb.MinerListFilter{
			PairingStatuses: []pb.PairingStatus{pb.PairingStatus_PAIRING_STATUS_PAIRED},
		},
	}

	// Act
	resp, err := service.ListMinerStateSnapshots(ctx, req)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Miners, 2, "Should return 2 paired devices")

	for _, miner := range resp.Miners {
		assert.Equal(t, pb.PairingStatus_PAIRING_STATUS_PAIRED, miner.PairingStatus)
		assert.NotNil(t, miner.Capabilities, "Capabilities should be populated for paired device %s", miner.DeviceIdentifier)

		// Verify telemetry capabilities
		require.NotNil(t, miner.Capabilities.Telemetry)
		assert.True(t, miner.Capabilities.Telemetry.HashrateReported, "Hashrate should be reported")
		assert.True(t, miner.Capabilities.Telemetry.PowerUsageReported, "Power usage should be reported")
		assert.True(t, miner.Capabilities.Telemetry.EfficiencyReported, "Efficiency should be reported")
		assert.True(t, miner.Capabilities.Telemetry.TemperatureReported, "Temperature should be reported")

		// Verify command capabilities
		require.NotNil(t, miner.Capabilities.Commands)
		assert.True(t, miner.Capabilities.Commands.RebootSupported, "Reboot should be supported")

		// Verify manufacturer
		assert.Equal(t, "Proto", miner.Capabilities.Manufacturer)
	}
}

func TestService_ListMinerStateSnapshots_ShouldPopulateCapabilitiesForUnpairedDevices(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	// Arrange
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testUser := testContext.DatabaseService.CreateSuperAdminUser()

	discoveredDeviceStore := sqlstores.NewSQLDiscoveredDeviceStore(testContext.ServiceProvider.DB)

	deviceIdentifier := "unpaired-antminer-1"
	doi := discoverymodels.DeviceOrgIdentifier{
		DeviceIdentifier: deviceIdentifier,
		OrgID:            testUser.OrganizationID,
	}
	device := &discoverymodels.DiscoveredDevice{
		Device: pairingpb.Device{
			DeviceIdentifier: deviceIdentifier,
			Model:            "S19 Pro",
			Manufacturer:     "Bitmain",
			DriverName:       "ANTMINER",
			IpAddress:        "192.168.1.100",
			Port:             "4028",
			UrlScheme:        "http",
		},
		IsActive: true,
		OrgID:    testUser.OrganizationID,
	}
	_, err := discoveredDeviceStore.Save(t.Context(), doi, device)
	require.NoError(t, err)

	mockCapabilities := pairingmocks.NewMockCapabilitiesProvider(ctrl)

	antminerCapabilities := &capabilitiespb.MinerCapabilities{
		Manufacturer: "Bitmain",
		Telemetry: &capabilitiespb.TelemetryCapabilities{
			HashrateReported:    true,
			PowerUsageReported:  false,
			TemperatureReported: true,
			EfficiencyReported:  false,
			FanSpeedReported:    true,
		},
		Commands: &capabilitiespb.CommandCapabilities{
			RebootSupported:           true,
			PoolSwitchingSupported:    true,
			MiningStartSupported:      true,
			MiningStopSupported:       true,
			AirCoolingSupported:       true,
			ImmersionCoolingSupported: false,
		},
	}

	mockCapabilities.EXPECT().
		GetMinerCapabilitiesForDevice(gomock.Any(), gomock.Any()).
		Return(antminerCapabilities).
		Times(1)

	// Create service with mock capabilities provider
	deviceStore := sqlstores.NewSQLDeviceStore(testContext.ServiceProvider.DB)
	poolStore := sqlstores.NewSQLPoolStore(testContext.ServiceProvider.DB, testContext.ServiceProvider.EncryptService)
	errorStore := sqlstores.NewSQLErrorStore(testContext.ServiceProvider.DB, sqlstores.NewSQLTransactor(testContext.ServiceProvider.DB))
	collectionStore := sqlstores.NewSQLCollectionStore(testContext.ServiceProvider.DB)
	activitySvc := activity.NewService(sqlstores.NewSQLActivityStore(testContext.ServiceProvider.DB))
	service := fleetmanagement.NewService(
		deviceStore,
		discoveredDeviceStore,
		fleetmanagement.NewMockTelemetryCollector(),
		testContext.ServiceProvider.MinerService,
		mockCapabilities,
		poolStore,
		errorStore,
		collectionStore,
		nil,
		activitySvc,
	)

	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)

	req := &pb.ListMinerStateSnapshotsRequest{
		PageSize: 10,
		Filter: &pb.MinerListFilter{
			PairingStatuses: []pb.PairingStatus{pb.PairingStatus_PAIRING_STATUS_UNPAIRED},
		},
	}

	// Act
	resp, err := service.ListMinerStateSnapshots(ctx, req)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Miners, 1, "Should return 1 unpaired device")

	miner := resp.Miners[0]
	assert.Equal(t, pb.PairingStatus_PAIRING_STATUS_UNPAIRED, miner.PairingStatus)
	assert.NotNil(t, miner.Capabilities, "Capabilities should be populated for unpaired device")

	// Verify capabilities structure
	require.NotNil(t, miner.Capabilities.Telemetry, "Telemetry capabilities should be present")
	assert.True(t, miner.Capabilities.Telemetry.HashrateReported)
	assert.False(t, miner.Capabilities.Telemetry.PowerUsageReported, "Antminers should not report power usage")
	assert.False(t, miner.Capabilities.Telemetry.EfficiencyReported, "Antminers should not report efficiency")

	require.NotNil(t, miner.Capabilities.Commands, "Command capabilities should be present")
	assert.True(t, miner.Capabilities.Commands.PoolSwitchingSupported)

	assert.Equal(t, "Bitmain", miner.Capabilities.Manufacturer, "Manufacturer should match")
}

func TestService_ListMinerStateSnapshots_ShouldCacheCapabilities(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testUser := testContext.DatabaseService.CreateSuperAdminUser()
	testContext.DatabaseService.CreateTestMiners(testUser.OrganizationID, 2, "https://172.17.0.1:80")

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockCapabilities := pairingmocks.NewMockCapabilitiesProvider(ctrl)

	protoCapabilities := &capabilitiespb.MinerCapabilities{
		Manufacturer: "Proto",
		Telemetry: &capabilitiespb.TelemetryCapabilities{
			HashrateReported:    true,
			PowerUsageReported:  true,
			TemperatureReported: true,
			EfficiencyReported:  true,
			FanSpeedReported:    true,
		},
	}

	mockCapabilities.EXPECT().
		GetMinerCapabilitiesForDevice(gomock.Any(), gomock.Any()).
		Return(protoCapabilities).
		Times(1) // Called only once, then cached

	deviceStore := sqlstores.NewSQLDeviceStore(testContext.ServiceProvider.DB)
	discoveredDeviceStore := sqlstores.NewSQLDiscoveredDeviceStore(testContext.ServiceProvider.DB)
	poolStore := sqlstores.NewSQLPoolStore(testContext.ServiceProvider.DB, testContext.ServiceProvider.EncryptService)
	errorStore := sqlstores.NewSQLErrorStore(testContext.ServiceProvider.DB, sqlstores.NewSQLTransactor(testContext.ServiceProvider.DB))
	collectionStore := sqlstores.NewSQLCollectionStore(testContext.ServiceProvider.DB)
	activitySvc := activity.NewService(sqlstores.NewSQLActivityStore(testContext.ServiceProvider.DB))
	service := fleetmanagement.NewService(
		deviceStore,
		discoveredDeviceStore,
		fleetmanagement.NewMockTelemetryCollector(),
		testContext.ServiceProvider.MinerService,
		mockCapabilities,
		poolStore,
		errorStore,
		collectionStore,
		nil,
		activitySvc,
	)

	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)

	req := &pb.ListMinerStateSnapshotsRequest{
		PageSize: 10,
	}

	// First call - should fetch from provider
	resp1, err := service.ListMinerStateSnapshots(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp1)
	require.Len(t, resp1.Miners, 2)
	require.NotNil(t, resp1.Miners[0].Capabilities)
	require.NotNil(t, resp1.Miners[1].Capabilities)

	// Second call - should use cache (mock expects only 1 call total)
	resp2, err := service.ListMinerStateSnapshots(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp2)
	require.Len(t, resp2.Miners, 2)
	require.NotNil(t, resp2.Miners[0].Capabilities)
	require.NotNil(t, resp2.Miners[1].Capabilities)

	assert.True(t, resp2.Miners[0].Capabilities.Telemetry.PowerUsageReported)
	assert.True(t, resp2.Miners[1].Capabilities.Telemetry.EfficiencyReported)
}

func TestService_ListMinerStateSnapshots_IncludesFirmwareVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	t.Run("includes firmware version in response when available", func(t *testing.T) {
		// Arrange
		testContext := testutil.InitializeDBServiceInfrastructure(t)
		testUser := testContext.DatabaseService.CreateSuperAdminUser()

		discoveredDeviceStore := sqlstores.NewSQLDiscoveredDeviceStore(testContext.ServiceProvider.DB)

		// Create device with firmware version
		deviceIdentifier := "device-with-firmware"
		expectedFirmwareVersion := "1.2.3"
		doi := discoverymodels.DeviceOrgIdentifier{
			DeviceIdentifier: deviceIdentifier,
			OrgID:            testUser.OrganizationID,
		}
		device := &discoverymodels.DiscoveredDevice{
			Device: pairingpb.Device{
				DeviceIdentifier: deviceIdentifier,
				Model:            "M100S",
				Manufacturer:     "Proto",
				DriverName:       "proto",
				IpAddress:        "192.168.1.100",
				Port:             "8080",
				UrlScheme:        "https",
				FirmwareVersion:  expectedFirmwareVersion,
			},
			IsActive: true,
			OrgID:    testUser.OrganizationID,
		}
		_, err := discoveredDeviceStore.Save(t.Context(), doi, device)
		require.NoError(t, err)

		ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)
		service := testContext.ServiceProvider.FleetManagementService

		req := &pb.ListMinerStateSnapshotsRequest{
			PageSize: 10,
		}

		// Act
		resp, err := service.ListMinerStateSnapshots(ctx, req)

		// Assert
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Len(t, resp.Miners, 1)

		// Verify firmware version is included in response
		miner := resp.Miners[0]
		assert.Equal(t, expectedFirmwareVersion, miner.FirmwareVersion, "firmware version should be included in MinerStateSnapshot")
	})

	t.Run("handles missing firmware version gracefully", func(t *testing.T) {
		// Arrange
		testContext := testutil.InitializeDBServiceInfrastructure(t)
		testUser := testContext.DatabaseService.CreateSuperAdminUser()

		discoveredDeviceStore := sqlstores.NewSQLDiscoveredDeviceStore(testContext.ServiceProvider.DB)

		// Create device without firmware version
		deviceIdentifier := "device-without-firmware"
		doi := discoverymodels.DeviceOrgIdentifier{
			DeviceIdentifier: deviceIdentifier,
			OrgID:            testUser.OrganizationID,
		}
		device := &discoverymodels.DiscoveredDevice{
			Device: pairingpb.Device{
				DeviceIdentifier: deviceIdentifier,
				Model:            "S19 Pro",
				Manufacturer:     "Bitmain",
				DriverName:       "antminer",
				IpAddress:        "192.168.1.101",
				Port:             "4028",
				UrlScheme:        "http",
				// FirmwareVersion intentionally not set
			},
			IsActive: true,
			OrgID:    testUser.OrganizationID,
		}
		_, err := discoveredDeviceStore.Save(t.Context(), doi, device)
		require.NoError(t, err)

		ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)
		service := testContext.ServiceProvider.FleetManagementService

		req := &pb.ListMinerStateSnapshotsRequest{
			PageSize: 10,
		}

		// Act
		resp, err := service.ListMinerStateSnapshots(ctx, req)

		// Assert
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Len(t, resp.Miners, 1)

		// Firmware version should be empty string when not set
		miner := resp.Miners[0]
		assert.Empty(t, miner.FirmwareVersion, "firmware version should be empty when not set in database")
	})
}

func TestService_ListMinerStateSnapshots_IncludesWorkerNameForPairedDevice(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testUser := testContext.DatabaseService.CreateSuperAdminUser()

	discoveredDeviceStore := sqlstores.NewSQLDiscoveredDeviceStore(testContext.ServiceProvider.DB)
	deviceStore := sqlstores.NewSQLDeviceStore(testContext.ServiceProvider.DB)

	deviceIdentifier := "device-with-worker-name"
	expectedWorkerName := "worker-01"
	doi := discoverymodels.DeviceOrgIdentifier{
		DeviceIdentifier: deviceIdentifier,
		OrgID:            testUser.OrganizationID,
	}
	device := &discoverymodels.DiscoveredDevice{
		Device: pairingpb.Device{
			DeviceIdentifier: deviceIdentifier,
			Model:            "M100S",
			Manufacturer:     "Proto",
			DriverName:       "proto",
			IpAddress:        "192.168.1.110",
			Port:             "2121",
			UrlScheme:        "https",
		},
		IsActive: true,
		OrgID:    testUser.OrganizationID,
	}
	_, err := discoveredDeviceStore.Save(t.Context(), doi, device)
	require.NoError(t, err)

	err = deviceStore.InsertDevice(
		t.Context(),
		&pairingpb.Device{
			DeviceIdentifier: deviceIdentifier,
			MacAddress:       "AA:BB:CC:DD:EE:01",
		},
		testUser.OrganizationID,
		deviceIdentifier,
	)
	require.NoError(t, err)

	_, err = testContext.ServiceProvider.DB.ExecContext(
		t.Context(),
		`INSERT INTO device_pairing (device_id, pairing_status, paired_at)
		SELECT id, 'PAIRED', NOW()
		FROM device
		WHERE org_id = $1 AND device_identifier = $2`,
		testUser.OrganizationID,
		deviceIdentifier,
	)
	require.NoError(t, err)

	_, err = testContext.ServiceProvider.DB.ExecContext(
		t.Context(),
		`UPDATE device SET worker_name = $1 WHERE org_id = $2 AND device_identifier = $3`,
		expectedWorkerName,
		testUser.OrganizationID,
		deviceIdentifier,
	)
	require.NoError(t, err)

	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)
	service := testContext.ServiceProvider.FleetManagementService

	resp, err := service.ListMinerStateSnapshots(ctx, &pb.ListMinerStateSnapshotsRequest{PageSize: 10})
	require.NoError(t, err)
	require.Len(t, resp.Miners, 1)
	assert.Equal(t, expectedWorkerName, resp.Miners[0].WorkerName)
}

func TestService_GetMinerCoolingMode_ShouldReturnNotFoundForNonexistentMiner(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	// Arrange
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testUser := testContext.DatabaseService.CreateSuperAdminUser()

	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)
	service := testContext.ServiceProvider.FleetManagementService

	req := &pb.GetMinerCoolingModeRequest{
		DeviceIdentifier: "nonexistent-miner-id",
	}

	// Act
	resp, err := service.GetMinerCoolingMode(ctx, req)

	// Assert
	require.Error(t, err)
	assert.Nil(t, resp)
	var fleetErr fleeterror.FleetError
	require.True(t, errors.As(err, &fleetErr), "expected FleetError")
	assert.Equal(t, connect.CodeNotFound, fleetErr.GRPCCode)
}

func TestService_GetMinerCoolingMode_ShouldDenyAccessToOtherOrgMiner(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	// Arrange
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user1 := testContext.DatabaseService.CreateSuperAdminUser()
	user2 := testContext.DatabaseService.CreateSuperAdminUser2()

	// Create a miner for user2's organization
	user2DeviceIDs := testContext.DatabaseService.CreateTestMiners(user2.OrganizationID, 1, "https://172.17.0.1:80")

	// Try to access user2's miner as user1
	ctx := testutil.MockAuthContextForTesting(t.Context(), user1.DatabaseID, user1.OrganizationID)
	service := testContext.ServiceProvider.FleetManagementService

	req := &pb.GetMinerCoolingModeRequest{
		DeviceIdentifier: user2DeviceIDs[0],
	}

	// Act
	resp, err := service.GetMinerCoolingMode(ctx, req)

	// Assert - should get an error (either NotFound or Internal, depending on how the miner
	// service handles cross-org device access). The key security requirement is that user1
	// cannot access user2's miner data.
	// Note: In CI, createMiner may fail before the org check (no real device at test IP),
	// returning Internal. With a real device, the org mismatch check returns NotFound.
	require.Error(t, err, "user should not be able to access another org's miner")
	assert.Nil(t, resp)
	var fleetErr fleeterror.FleetError
	require.True(t, errors.As(err, &fleetErr), "expected FleetError")
	assert.True(t,
		fleetErr.GRPCCode == connect.CodeNotFound || fleetErr.GRPCCode == connect.CodeInternal,
		"expected NotFound or Internal error code, got %v", fleetErr.GRPCCode,
	)
}

// --- DeleteMiners tests ---

func TestService_DeleteMiners_ShouldSoftDeleteSpecificDevices(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testUser := testContext.DatabaseService.CreateSuperAdminUser()

	deviceIDs := testContext.DatabaseService.CreateTestMiners(testUser.OrganizationID, 3, "https://172.17.0.1:80")

	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)
	service := testContext.ServiceProvider.FleetManagementService

	// Delete 2 of the 3 miners
	req := &pb.DeleteMinersRequest{
		DeviceSelector: &pb.DeviceSelector{
			SelectionType: &pb.DeviceSelector_IncludeDevices{
				IncludeDevices: &commonv1.DeviceIdentifierList{
					DeviceIdentifiers: deviceIDs[:2],
				},
			},
		},
	}
	resp, err := service.DeleteMiners(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, int32(2), resp.DeletedCount)

	// Verify only 1 miner remains in the fleet
	listResp, err := service.ListMinerStateSnapshots(ctx, &pb.ListMinerStateSnapshotsRequest{PageSize: 10})
	require.NoError(t, err)
	assert.Len(t, listResp.Miners, 1, "only 1 miner should remain after deleting 2")
	assert.Equal(t, deviceIDs[2], listResp.Miners[0].DeviceIdentifier)
}

func TestService_DeleteMiners_ShouldRejectEmptyRequestWithoutFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testUser := testContext.DatabaseService.CreateSuperAdminUser()

	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)
	service := testContext.ServiceProvider.FleetManagementService

	req := &pb.DeleteMinersRequest{}
	_, err := service.DeleteMiners(ctx, req)

	require.Error(t, err, "request without device_selector should be rejected")
}

func TestService_DeleteMiners_ShouldReturnZeroForEmptyFilterWithNoDevices(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testUser := testContext.DatabaseService.CreateSuperAdminUser()

	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)
	service := testContext.ServiceProvider.FleetManagementService

	req := &pb.DeleteMinersRequest{
		DeviceSelector: &pb.DeviceSelector{
			SelectionType: &pb.DeviceSelector_AllDevices{
				AllDevices: &pb.MinerListFilter{},
			},
		},
	}
	resp, err := service.DeleteMiners(ctx, req)

	require.NoError(t, err)
	assert.Equal(t, int32(0), resp.DeletedCount)
}

func TestService_DeleteMiners_ShouldDenyAccessToOtherOrgDevices(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user1 := testContext.DatabaseService.CreateSuperAdminUser()
	user2 := testContext.DatabaseService.CreateSuperAdminUser2()

	// Create a miner for user2
	user2DeviceIDs := testContext.DatabaseService.CreateTestMiners(user2.OrganizationID, 1, "https://172.17.0.2:80")

	// user1 attempts to delete user2's miner
	ctx := testutil.MockAuthContextForTesting(t.Context(), user1.DatabaseID, user1.OrganizationID)
	service := testContext.ServiceProvider.FleetManagementService

	req := &pb.DeleteMinersRequest{
		DeviceSelector: &pb.DeviceSelector{
			SelectionType: &pb.DeviceSelector_IncludeDevices{
				IncludeDevices: &commonv1.DeviceIdentifierList{
					DeviceIdentifiers: user2DeviceIDs,
				},
			},
		},
	}
	_, err := service.DeleteMiners(ctx, req)

	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.True(t, errors.As(err, &fleetErr), "expected FleetError")
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
}

func TestService_DeleteMiners_ShouldRejectAlreadyDeletedDevices(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testUser := testContext.DatabaseService.CreateSuperAdminUser()

	deviceIDs := testContext.DatabaseService.CreateTestMiners(testUser.OrganizationID, 1, "https://172.17.0.1:80")

	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)
	service := testContext.ServiceProvider.FleetManagementService

	// First delete
	req := &pb.DeleteMinersRequest{
		DeviceSelector: &pb.DeviceSelector{
			SelectionType: &pb.DeviceSelector_IncludeDevices{
				IncludeDevices: &commonv1.DeviceIdentifierList{
					DeviceIdentifiers: deviceIDs,
				},
			},
		},
	}
	resp, err := service.DeleteMiners(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, int32(1), resp.DeletedCount)

	// Soft-deleted devices are excluded from ownership checks, so the second
	// delete is rejected as if the devices don't belong to the org.
	_, err = service.DeleteMiners(ctx, req)
	require.Error(t, err, "deleting already-deleted device should fail ownership check")
}

func TestService_DeleteMiners_ShouldDeleteAllPairedDevicesWithEmptyFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testUser := testContext.DatabaseService.CreateSuperAdminUser()

	// Create 3 paired miners
	testContext.DatabaseService.CreateTestMiners(testUser.OrganizationID, 3, "https://172.17.0.1:80")

	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)
	service := testContext.ServiceProvider.FleetManagementService

	// Empty filter signals "delete all paired devices"
	req := &pb.DeleteMinersRequest{
		DeviceSelector: &pb.DeviceSelector{
			SelectionType: &pb.DeviceSelector_AllDevices{
				AllDevices: &pb.MinerListFilter{},
			},
		},
	}
	resp, err := service.DeleteMiners(ctx, req)

	require.NoError(t, err)
	assert.Equal(t, int32(3), resp.DeletedCount)

	// Verify all miners are gone
	listResp, err := service.ListMinerStateSnapshots(ctx, &pb.ListMinerStateSnapshotsRequest{PageSize: 10})
	require.NoError(t, err)
	assert.Empty(t, listResp.Miners, "no miners should remain after deleting all")
}

func TestService_DeleteMiners_ShouldAllowReDiscoveryAfterSoftDelete(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testUser := testContext.DatabaseService.CreateSuperAdminUser()

	deviceIDs := testContext.DatabaseService.CreateTestMiners(testUser.OrganizationID, 1, "https://172.17.0.1:80")

	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)
	service := testContext.ServiceProvider.FleetManagementService

	// Delete the miner
	req := &pb.DeleteMinersRequest{
		DeviceSelector: &pb.DeviceSelector{
			SelectionType: &pb.DeviceSelector_IncludeDevices{
				IncludeDevices: &commonv1.DeviceIdentifierList{
					DeviceIdentifiers: deviceIDs,
				},
			},
		},
	}
	resp, err := service.DeleteMiners(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, int32(1), resp.DeletedCount)

	// Re-create a miner with the same IP (simulating re-discovery and re-pairing)
	// This should succeed because partial unique indexes only enforce uniqueness
	// among non-deleted rows.
	newDeviceIDs := testContext.DatabaseService.CreateTestMiners(testUser.OrganizationID, 1, "https://172.17.0.5:80")
	require.Len(t, newDeviceIDs, 1)

	// Verify the new miner is visible
	listResp, err := service.ListMinerStateSnapshots(ctx, &pb.ListMinerStateSnapshotsRequest{PageSize: 10})
	require.NoError(t, err)
	assert.Len(t, listResp.Miners, 1, "re-discovered miner should be visible")
}

func TestService_DeleteMiners_ShouldWaitForPendingClearAuthKeys(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testUser := testContext.DatabaseService.CreateSuperAdminUser()

	testContext.DatabaseService.CreateTestMiners(testUser.OrganizationID, 1, "https://172.17.0.1:80")

	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)
	service := testContext.ServiceProvider.FleetManagementService

	req := &pb.DeleteMinersRequest{
		DeviceSelector: &pb.DeviceSelector{
			SelectionType: &pb.DeviceSelector_AllDevices{
				AllDevices: &pb.MinerListFilter{},
			},
		},
	}
	_, err := service.DeleteMiners(ctx, req)
	require.NoError(t, err)

	// WaitForPendingClearAuthKeys should return promptly (background ClearAuthKey
	// will fail since there's no real device, but that's expected — best-effort)
	done := make(chan struct{})
	go func() {
		service.WaitForPendingClearAuthKeys(1 * time.Minute)
		close(done)
	}()

	select {
	case <-done:
		// Expected: completed within timeout
	case <-time.After(1 * time.Minute):
		t.Fatal("WaitForPendingClearAuthKeys did not complete within timeout")
	}
}

// TestService_RenameMiners_PersistsCustomName verifies that the custom name is stored
// and returned by ListMinerStateSnapshots after a rename.
func TestService_RenameMiners_PersistsCustomName(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	// Arrange
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testUser := testContext.DatabaseService.CreateSuperAdminUser()
	deviceIDs := testContext.DatabaseService.CreateTestMiners(testUser.OrganizationID, 1, "https://172.17.0.1:80")

	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)
	service := testContext.ServiceProvider.FleetManagementService

	// Act — rename the single device to a static string
	renameReq := &pb.RenameMinersRequest{
		DeviceSelector: &pb.DeviceSelector{
			SelectionType: &pb.DeviceSelector_IncludeDevices{
				IncludeDevices: &commonv1.DeviceIdentifierList{
					DeviceIdentifiers: deviceIDs,
				},
			},
		},
		NameConfig: &pb.MinerNameConfig{
			Properties: []*pb.NameProperty{
				{Kind: &pb.NameProperty_StringValue{StringValue: &pb.StringProperty{Value: "my-miner"}}},
			},
			Separator: "",
		},
	}
	_, err := service.RenameMiners(ctx, renameReq)
	require.NoError(t, err)

	// Assert — the custom name appears in the list response
	listResp, err := service.ListMinerStateSnapshots(ctx, &pb.ListMinerStateSnapshotsRequest{PageSize: 10})
	require.NoError(t, err)
	require.Len(t, listResp.Miners, 1)
	assert.Equal(t, "my-miner", listResp.Miners[0].Name)
}

// TestService_RenameMiners_CounterOrderByRequestedSort verifies that counter values
// follow the caller-provided fleet sort instead of a backend-only ordering.
func TestService_RenameMiners_CounterOrderByRequestedSort(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	// Arrange
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testUser := testContext.DatabaseService.CreateSuperAdminUser()
	deviceIDs := testContext.DatabaseService.CreateTestMiners(testUser.OrganizationID, 3, "https://172.17.0.1:80")

	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)
	service := testContext.ServiceProvider.FleetManagementService

	err := testContext.ServiceProvider.DeviceStore.UpdateDeviceCustomNames(ctx, testUser.OrganizationID, map[string]string{
		deviceIDs[0]: "Zulu",
		deviceIDs[1]: "Alpha",
		deviceIDs[2]: "Beta",
	})
	require.NoError(t, err)

	// Act — rename all 3 devices with counter starting at 1, scale 1 using current name sort.
	renameReq := &pb.RenameMinersRequest{
		DeviceSelector: &pb.DeviceSelector{
			SelectionType: &pb.DeviceSelector_IncludeDevices{
				IncludeDevices: &commonv1.DeviceIdentifierList{
					DeviceIdentifiers: deviceIDs,
				},
			},
		},
		NameConfig: &pb.MinerNameConfig{
			Properties: []*pb.NameProperty{
				{Kind: &pb.NameProperty_Counter{Counter: &pb.CounterProperty{CounterStart: 1, CounterScale: 1}}},
			},
			Separator: "",
		},
		Sort: []*commonv1.SortConfig{{
			Field:     commonv1.SortField_SORT_FIELD_NAME,
			Direction: commonv1.SortDirection_SORT_DIRECTION_ASC,
		}},
	}
	_, err = service.RenameMiners(ctx, renameReq)
	require.NoError(t, err)

	// Assert — names follow Alpha -> Beta -> Zulu table ordering.
	listResp, err := service.ListMinerStateSnapshots(ctx, &pb.ListMinerStateSnapshotsRequest{PageSize: 10})
	require.NoError(t, err)
	require.Len(t, listResp.Miners, 3)

	namesByDeviceID := make(map[string]string, 3)
	for _, m := range listResp.Miners {
		namesByDeviceID[m.DeviceIdentifier] = m.Name
	}
	assert.Equal(t, "3", namesByDeviceID[deviceIDs[0]])
	assert.Equal(t, "1", namesByDeviceID[deviceIDs[1]])
	assert.Equal(t, "2", namesByDeviceID[deviceIDs[2]])
}

// TestService_RenameMiners_BlankGeneratedNamesAreSkipped verifies that an all-blank
// rename config behaves like a no-op batch.
func TestService_RenameMiners_BlankGeneratedNamesAreSkipped(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testUser := testContext.DatabaseService.CreateSuperAdminUser()
	deviceIDs := testContext.DatabaseService.CreateTestMiners(testUser.OrganizationID, 2, "https://172.17.0.1:80")

	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)
	service := testContext.ServiceProvider.FleetManagementService

	renameReq := &pb.RenameMinersRequest{
		DeviceSelector: &pb.DeviceSelector{
			SelectionType: &pb.DeviceSelector_IncludeDevices{
				IncludeDevices: &commonv1.DeviceIdentifierList{
					DeviceIdentifiers: deviceIDs,
				},
			},
		},
		NameConfig: &pb.MinerNameConfig{
			Properties: []*pb.NameProperty{
				{Kind: &pb.NameProperty_FixedValue{FixedValue: &pb.FixedValueProperty{Type: pb.FixedValueType_FIXED_VALUE_TYPE_LOCATION}}},
			},
			Separator: "",
		},
	}

	beforeResp, err := service.ListMinerStateSnapshots(ctx, &pb.ListMinerStateSnapshotsRequest{PageSize: 10})
	require.NoError(t, err)
	require.Len(t, beforeResp.Miners, 2)

	resp, err := service.RenameMiners(ctx, renameReq)
	require.NoError(t, err)
	require.Equal(t, int32(0), resp.RenamedCount)
	require.Equal(t, int32(2), resp.UnchangedCount)
	require.Equal(t, int32(0), resp.FailedCount)

	listResp, err := service.ListMinerStateSnapshots(ctx, &pb.ListMinerStateSnapshotsRequest{PageSize: 10})
	require.NoError(t, err)
	require.Len(t, listResp.Miners, 2)

	namesBefore := make(map[string]string, len(beforeResp.Miners))
	for _, miner := range beforeResp.Miners {
		namesBefore[miner.DeviceIdentifier] = miner.Name
	}

	for _, miner := range listResp.Miners {
		assert.Equal(t, namesBefore[miner.DeviceIdentifier], miner.Name)
	}
}

// TestService_RenameMiners_BlankGeneratedNamesDoNotBlockOtherRenames verifies that
// blank generated names are skipped without failing devices that have valid names.
func TestService_RenameMiners_BlankGeneratedNamesDoNotBlockOtherRenames(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testUser := testContext.DatabaseService.CreateSuperAdminUser()
	deviceIDs := testContext.DatabaseService.CreateTestMiners(testUser.OrganizationID, 2, "https://172.17.0.1:80")

	_, err := testContext.ServiceProvider.DB.ExecContext(
		t.Context(),
		`UPDATE device SET serial_number = $1 WHERE device_identifier = $2 AND org_id = $3`,
		"SERIAL-001",
		deviceIDs[0],
		testUser.OrganizationID,
	)
	require.NoError(t, err)

	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)
	service := testContext.ServiceProvider.FleetManagementService

	renameReq := &pb.RenameMinersRequest{
		DeviceSelector: &pb.DeviceSelector{
			SelectionType: &pb.DeviceSelector_IncludeDevices{
				IncludeDevices: &commonv1.DeviceIdentifierList{
					DeviceIdentifiers: deviceIDs,
				},
			},
		},
		NameConfig: &pb.MinerNameConfig{
			Properties: []*pb.NameProperty{
				{Kind: &pb.NameProperty_FixedValue{FixedValue: &pb.FixedValueProperty{Type: pb.FixedValueType_FIXED_VALUE_TYPE_SERIAL_NUMBER}}},
			},
			Separator: "",
		},
	}

	beforeResp, err := service.ListMinerStateSnapshots(ctx, &pb.ListMinerStateSnapshotsRequest{PageSize: 10})
	require.NoError(t, err)

	resp, err := service.RenameMiners(ctx, renameReq)
	require.NoError(t, err)
	require.Equal(t, int32(1), resp.RenamedCount)
	require.Equal(t, int32(1), resp.UnchangedCount)
	require.Equal(t, int32(0), resp.FailedCount)

	listResp, err := service.ListMinerStateSnapshots(ctx, &pb.ListMinerStateSnapshotsRequest{PageSize: 10})
	require.NoError(t, err)

	namesByDevice := make(map[string]string, len(listResp.Miners))
	for _, miner := range listResp.Miners {
		namesByDevice[miner.DeviceIdentifier] = miner.Name
	}

	serialByDevice := make(map[string]string, len(beforeResp.Miners))
	namesBefore := make(map[string]string, len(beforeResp.Miners))
	for _, miner := range beforeResp.Miners {
		serialByDevice[miner.DeviceIdentifier] = miner.SerialNumber
		namesBefore[miner.DeviceIdentifier] = miner.Name
	}

	require.Contains(t, namesByDevice, deviceIDs[0])
	require.Contains(t, namesByDevice, deviceIDs[1])
	assert.Equal(t, serialByDevice[deviceIDs[0]], namesByDevice[deviceIDs[0]])
	assert.Equal(t, namesBefore[deviceIDs[1]], namesByDevice[deviceIDs[1]])
}

// TestService_RenameMiners_IdenticalGeneratedNamesAreUnchanged verifies that
// renaming to the current persisted custom name is reported as unchanged.
func TestService_RenameMiners_IdenticalGeneratedNamesAreUnchanged(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testUser := testContext.DatabaseService.CreateSuperAdminUser()
	deviceIDs := testContext.DatabaseService.CreateTestMiners(testUser.OrganizationID, 1, "https://172.17.0.1:80")

	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)
	service := testContext.ServiceProvider.FleetManagementService

	err := testContext.ServiceProvider.DeviceStore.UpdateDeviceCustomNames(ctx, testUser.OrganizationID, map[string]string{
		deviceIDs[0]: "rig-01",
	})
	require.NoError(t, err)

	resp, err := service.RenameMiners(ctx, &pb.RenameMinersRequest{
		DeviceSelector: &pb.DeviceSelector{
			SelectionType: &pb.DeviceSelector_IncludeDevices{
				IncludeDevices: &commonv1.DeviceIdentifierList{
					DeviceIdentifiers: deviceIDs,
				},
			},
		},
		NameConfig: &pb.MinerNameConfig{
			Properties: []*pb.NameProperty{
				{Kind: &pb.NameProperty_StringValue{StringValue: &pb.StringProperty{Value: "rig-01"}}},
			},
			Separator: "",
		},
	})
	require.NoError(t, err)
	require.Equal(t, int32(0), resp.RenamedCount)
	require.Equal(t, int32(1), resp.UnchangedCount)
	require.Equal(t, int32(0), resp.FailedCount)

	listResp, err := service.ListMinerStateSnapshots(ctx, &pb.ListMinerStateSnapshotsRequest{PageSize: 10})
	require.NoError(t, err)
	require.Len(t, listResp.Miners, 1)
	assert.Equal(t, "rig-01", listResp.Miners[0].Name)
}

// TestService_RenameMiners_IdenticalDisplayedNamesAreUnchanged verifies that
// renaming to the current effective display name is treated as a no-op even
// when the device does not already have a persisted custom name.
func TestService_RenameMiners_IdenticalDisplayedNamesAreUnchanged(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testUser := testContext.DatabaseService.CreateSuperAdminUser()
	deviceIDs := testContext.DatabaseService.CreateTestMiners(testUser.OrganizationID, 1, "https://172.17.0.1:2121")

	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)
	service := testContext.ServiceProvider.FleetManagementService

	beforeResp, err := service.ListMinerStateSnapshots(ctx, &pb.ListMinerStateSnapshotsRequest{PageSize: 10})
	require.NoError(t, err)
	require.Len(t, beforeResp.Miners, 1)

	currentDisplayedName := beforeResp.Miners[0].Name

	resp, err := service.RenameMiners(ctx, &pb.RenameMinersRequest{
		DeviceSelector: &pb.DeviceSelector{
			SelectionType: &pb.DeviceSelector_IncludeDevices{
				IncludeDevices: &commonv1.DeviceIdentifierList{
					DeviceIdentifiers: deviceIDs,
				},
			},
		},
		NameConfig: &pb.MinerNameConfig{
			Properties: []*pb.NameProperty{
				{Kind: &pb.NameProperty_StringValue{StringValue: &pb.StringProperty{Value: currentDisplayedName}}},
			},
			Separator: "",
		},
	})
	require.NoError(t, err)
	require.Equal(t, int32(0), resp.RenamedCount)
	require.Equal(t, int32(1), resp.UnchangedCount)
	require.Equal(t, int32(0), resp.FailedCount)

	listResp, err := service.ListMinerStateSnapshots(ctx, &pb.ListMinerStateSnapshotsRequest{PageSize: 10})
	require.NoError(t, err)
	require.Len(t, listResp.Miners, 1)
	assert.Equal(t, currentDisplayedName, listResp.Miners[0].Name)
}

// TestService_RenameMiners_InvalidGeneratedNamesAreCountedAsFailures verifies that
// per-device name-generation errors are reported without failing the whole batch.
func TestService_RenameMiners_InvalidGeneratedNamesAreCountedAsFailures(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testUser := testContext.DatabaseService.CreateSuperAdminUser()
	deviceIDs := testContext.DatabaseService.CreateTestMiners(testUser.OrganizationID, 2, "https://172.17.0.1:80")

	_, err := testContext.ServiceProvider.DB.ExecContext(
		t.Context(),
		`UPDATE device SET serial_number = $1 WHERE device_identifier = $2 AND org_id = $3`,
		"SERIAL-001",
		deviceIDs[0],
		testUser.OrganizationID,
	)
	require.NoError(t, err)

	_, err = testContext.ServiceProvider.DB.ExecContext(
		t.Context(),
		`UPDATE device SET serial_number = $1 WHERE device_identifier = $2 AND org_id = $3`,
		strings.Repeat("x", 101),
		deviceIDs[1],
		testUser.OrganizationID,
	)
	require.NoError(t, err)

	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)
	service := testContext.ServiceProvider.FleetManagementService

	resp, err := service.RenameMiners(ctx, &pb.RenameMinersRequest{
		DeviceSelector: &pb.DeviceSelector{
			SelectionType: &pb.DeviceSelector_IncludeDevices{
				IncludeDevices: &commonv1.DeviceIdentifierList{
					DeviceIdentifiers: deviceIDs,
				},
			},
		},
		NameConfig: &pb.MinerNameConfig{
			Properties: []*pb.NameProperty{
				{Kind: &pb.NameProperty_FixedValue{FixedValue: &pb.FixedValueProperty{Type: pb.FixedValueType_FIXED_VALUE_TYPE_SERIAL_NUMBER}}},
			},
			Separator: "",
		},
	})
	require.NoError(t, err)
	require.Equal(t, int32(1), resp.RenamedCount)
	require.Equal(t, int32(0), resp.UnchangedCount)
	require.Equal(t, int32(1), resp.FailedCount)
}

// TestService_RenameMiners_UnknownDeviceReturnsForbidden verifies that referencing a
// device identifier that doesn't exist returns a permission-denied error.
// AllDevicesBelongToOrg intentionally returns permission_denied (not not_found) to
// avoid leaking whether a device ID exists in another org.
func TestService_RenameMiners_UnknownDeviceReturnsForbidden(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	// Arrange
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testUser := testContext.DatabaseService.CreateSuperAdminUser()

	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)
	service := testContext.ServiceProvider.FleetManagementService

	// Act — reference a device that does not exist
	renameReq := &pb.RenameMinersRequest{
		DeviceSelector: &pb.DeviceSelector{
			SelectionType: &pb.DeviceSelector_IncludeDevices{
				IncludeDevices: &commonv1.DeviceIdentifierList{
					DeviceIdentifiers: []string{"does-not-exist"},
				},
			},
		},
		NameConfig: &pb.MinerNameConfig{
			Properties: []*pb.NameProperty{
				{Kind: &pb.NameProperty_StringValue{StringValue: &pb.StringProperty{Value: "name"}}},
			},
			Separator: "",
		},
	}
	_, err := service.RenameMiners(ctx, renameReq)

	// Assert
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.True(t, errors.As(err, &fleetErr), "expected FleetError")
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
}

// TestService_RenameMiners_EmptySelectorReturnsSuccess verifies that a rename with a
// selector that matches no devices returns successfully without error.
func TestService_RenameMiners_EmptySelectorReturnsSuccess(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	// Arrange
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testUser := testContext.DatabaseService.CreateSuperAdminUser()
	// No devices created

	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)
	service := testContext.ServiceProvider.FleetManagementService

	// Act — all-devices selector with an empty fleet
	renameReq := &pb.RenameMinersRequest{
		DeviceSelector: &pb.DeviceSelector{
			SelectionType: &pb.DeviceSelector_AllDevices{
				AllDevices: &pb.MinerListFilter{},
			},
		},
		NameConfig: &pb.MinerNameConfig{
			Properties: []*pb.NameProperty{
				{Kind: &pb.NameProperty_StringValue{StringValue: &pb.StringProperty{Value: "name"}}},
			},
			Separator: "",
		},
	}
	_, err := service.RenameMiners(ctx, renameReq)

	// Assert — no error when fleet is empty
	require.NoError(t, err)
}
