package fleetmanagement_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	capabilitiespb "github.com/btc-mining/proto-fleet/server/generated/grpc/capabilities/v1"
	errorsv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/errors/v1"
	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	pairingpb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	diagnosticsmodels "github.com/btc-mining/proto-fleet/server/internal/domain/diagnostics/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleetmanagement"
	minermodels "github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	discoverymodels "github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery/models"
	pairingmocks "github.com/btc-mining/proto-fleet/server/internal/domain/pairing/mocks"
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
			deviceIDs := testContext.DatabaseService.CreateTestMiners(testUser.OrganizationID, 4, "https://172.17.0.1:2121")

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

func TestService_GetBatchMinerTelemetry_ShouldReturnForbiddenForUnauthorizedDevices(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	// Arrange - create first user and their devices
	testContext1 := testutil.InitializeDBServiceInfrastructure(t)
	user1 := testContext1.DatabaseService.CreateSuperAdminUser()
	user1DeviceIDs := testContext1.DatabaseService.CreateTestMiners(user1.OrganizationID, 2, "https://172.17.0.1:2121")

	// Arrange - create second user in a different organization with their own devices
	user2 := testContext1.DatabaseService.CreateSuperAdminUser2()
	user2DeviceIDs := testContext1.DatabaseService.CreateTestMiners(user2.OrganizationID, 1, "https://172.17.0.2:2121")

	service := testContext1.ServiceProvider.FleetManagementService

	// Act - user1 tries to access user2's device
	ctx := testutil.MockAuthContextForTesting(t.Context(), user1.DatabaseID, user1.OrganizationID)
	req := &pb.GetBatchMinerTelemetryRequest{
		DeviceIdentifiers: []string{user2DeviceIDs[0]},
		DataMode:          pb.DataMode_DATA_MODE_SNAPSHOT,
	}
	_, err := service.GetBatchMinerTelemetry(ctx, req)

	// Assert - should get forbidden error
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.True(t, errors.As(err, &fleetErr), "expected FleetError")
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)

	// Act - user1 tries to access mix of own and user2's devices
	req = &pb.GetBatchMinerTelemetryRequest{
		DeviceIdentifiers: []string{user1DeviceIDs[0], user2DeviceIDs[0]},
		DataMode:          pb.DataMode_DATA_MODE_SNAPSHOT,
	}
	_, err = service.GetBatchMinerTelemetry(ctx, req)

	// Assert - should still get forbidden error (fail fast)
	require.Error(t, err)
	require.True(t, errors.As(err, &fleetErr), "expected FleetError")
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)

	// Act - user1 accesses only their own devices
	req = &pb.GetBatchMinerTelemetryRequest{
		DeviceIdentifiers: user1DeviceIDs,
		DataMode:          pb.DataMode_DATA_MODE_SNAPSHOT,
	}
	resp, err := service.GetBatchMinerTelemetry(ctx, req)

	// Assert - should succeed
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Miners, 2)
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

	testContext.DatabaseService.CreateTestMiners(testUser.OrganizationID, 2, "https://172.17.0.1:2121")

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
	service := fleetmanagement.NewService(
		deviceStore,
		discoveredDeviceStore,
		fleetmanagement.NewMockTelemetryCollector(),
		testContext.ServiceProvider.MinerService,
		mockCapabilities,
		poolStore,
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
			Type:             "ANTMINER",
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
	service := fleetmanagement.NewService(
		deviceStore,
		discoveredDeviceStore,
		fleetmanagement.NewMockTelemetryCollector(),
		testContext.ServiceProvider.MinerService,
		mockCapabilities,
		poolStore,
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
	testContext.DatabaseService.CreateTestMiners(testUser.OrganizationID, 2, "https://172.17.0.1:2121")

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
	service := fleetmanagement.NewService(
		deviceStore,
		discoveredDeviceStore,
		fleetmanagement.NewMockTelemetryCollector(),
		testContext.ServiceProvider.MinerService,
		mockCapabilities,
		poolStore,
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
