package fleetmanagement_test

import (
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/pairing"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestLookupMinerByInternalIdentifier(t *testing.T) {
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	admin := testContext.DatabaseService.CreateSuperAdminUser()
	otherAdmin := testContext.DatabaseService.CreateSuperAdminUser2()
	created := testContext.DatabaseService.CreateAndAssignDevices(1, admin.OrganizationID)[0]
	store := testContext.ServiceProvider.DeviceStore
	device, err := store.GetDeviceByDeviceIdentifier(t.Context(), created.ID, admin.OrganizationID)
	require.NoError(t, err)
	service := testContext.ServiceProvider.FleetManagementService
	ctx := testutil.MockAuthContextForTesting(t.Context(), admin.DatabaseID, admin.OrganizationID)
	request := &pb.LookupMinerByIdentifierRequest{
		Identifier: created.ID, IdentifierType: pb.MinerIdentifierType_MINER_IDENTIFIER_TYPE_DEVICE_IDENTIFIER,
	}
	for _, status := range []string{pairing.StatusPaired, pairing.StatusDefaultPassword, pairing.StatusAuthenticationNeeded} {
		t.Run(status, func(t *testing.T) {
			require.NoError(t, store.UpsertDevicePairing(t.Context(), device, admin.OrganizationID, status))
			result, err := service.LookupMinerByIdentifier(ctx, request)
			require.NoError(t, err)
			require.Equal(t, created.ID, result.GetSnapshot().GetDeviceIdentifier())
			if status != pairing.StatusAuthenticationNeeded {
				require.Equal(t, device.MacAddress, result.GetSnapshot().GetMacAddress())
			}
		})
	}
	otherCtx := testutil.MockAuthContextForTesting(t.Context(), otherAdmin.DatabaseID, otherAdmin.OrganizationID)
	_, err = service.LookupMinerByIdentifier(otherCtx, request)
	require.True(t, fleeterror.IsNotFoundError(err), "internal IDs must not escape organization scope")
	require.NoError(t, store.UpsertDevicePairing(t.Context(), device, admin.OrganizationID, pairing.StatusUnpaired))
	_, err = service.LookupMinerByIdentifier(ctx, request)
	require.True(t, fleeterror.IsNotFoundError(err), "an unpaired miner cannot resolve as a linked paired miner")
}
