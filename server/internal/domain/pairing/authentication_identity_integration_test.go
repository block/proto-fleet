package pairing_test

import (
	"context"
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	models "github.com/block/proto-fleet/server/internal/domain/miner/models"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/pairing"
	pairingmocks "github.com/block/proto-fleet/server/internal/domain/pairing/mocks"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestCredentialsRequiredPairingPreservesExistingIdentity(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   string
		macMatch bool
	}{
		{name: "replacement at paired endpoint", status: pairing.StatusPaired},
		{name: "replacement at default-password endpoint", status: pairing.StatusDefaultPassword},
		{name: "replacement at authentication-needed endpoint", status: pairing.StatusAuthenticationNeeded},
		{name: "MAC reconciliation cannot demote paired miner", status: pairing.StatusPaired, macMatch: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testContext := testutil.InitializeDBServiceInfrastructure(t)
			admin := testContext.DatabaseService.CreateSuperAdminUser()
			discoveredStore := sqlstores.NewSQLDiscoveredDeviceStore(testContext.ServiceProvider.DB)
			deviceStore := testContext.ServiceProvider.DeviceStore
			original := &discoverymodels.DiscoveredDevice{OrgID: admin.OrganizationID, Device: pb.Device{
				DeviceIdentifier: "original-miner", IpAddress: "192.168.94.20", Port: "80", UrlScheme: "http",
				DriverName: "antminer", Manufacturer: "TestCorp", FirmwareVersion: "1.0",
				SerialNumber: "original-serial", MacAddress: "02:00:00:00:00:20",
			}}
			doi := discoverymodels.DeviceOrgIdentifier{DeviceIdentifier: original.DeviceIdentifier, OrgID: admin.OrganizationID}
			_, err := discoveredStore.SaveDirect(t.Context(), doi, original)
			require.NoError(t, err)
			require.NoError(t, deviceStore.InsertDevice(t.Context(), &original.Device, admin.OrganizationID, original.DeviceIdentifier))
			require.NoError(t, deviceStore.UpdateDeviceInfo(t.Context(), &original.Device, admin.OrganizationID))
			require.NoError(t, deviceStore.UpsertDevicePairing(t.Context(), &original.Device, admin.OrganizationID, tc.status))
			require.NoError(t, deviceStore.UpdateWorkerName(t.Context(), models.DeviceIdentifier(original.DeviceIdentifier), "original-worker"))

			candidate := &discoverymodels.DiscoveredDevice{OrgID: admin.OrganizationID, Device: pb.Device{
				DeviceIdentifier: original.DeviceIdentifier, IpAddress: original.IpAddress, Port: original.Port,
				UrlScheme: "http", DriverName: "antminer", Manufacturer: "TestCorp", FirmwareVersion: "2.0",
				SerialNumber: "replacement-serial", MacAddress: "02:00:00:00:00:21",
			}}
			if tc.macMatch {
				candidate.DeviceIdentifier = "rediscovered-miner"
				candidate.IpAddress = "192.168.94.21"
				candidate.SerialNumber, candidate.MacAddress = original.SerialNumber, original.MacAddress
			}
			candidateDOI := discoverymodels.DeviceOrgIdentifier{DeviceIdentifier: candidate.DeviceIdentifier, OrgID: admin.OrganizationID}
			_, err = discoveredStore.SaveDirect(t.Context(), candidateDOI, candidate)
			require.NoError(t, err)
			ctrl := gomock.NewController(t)
			pairer := pairingmocks.NewMockPairer(ctrl)
			pairer.EXPECT().PairDevice(gomock.Any(), gomock.Any(), nil).DoAndReturn(
				func(_ context.Context, discovered *discoverymodels.DiscoveredDevice, _ *pb.Credentials) error {
					require.Equal(t, candidate.SerialNumber, discovered.SerialNumber, "pairing must load the new discovery evidence")
					require.Equal(t, candidate.MacAddress, discovered.MacAddress)
					return fleeterror.NewInvalidArgumentError("credentials are required but were not provided")
				})
			service, ctx := setupTestService(t, testContext, admin, pairer, &MockDiscoverer{})
			_, err = service.PairDevices(ctx, createPairRequest([]string{candidate.DeviceIdentifier}))
			require.NoError(t, err)
			stored, err := deviceStore.GetDeviceByDeviceIdentifier(ctx, original.DeviceIdentifier, admin.OrganizationID)
			require.NoError(t, err)
			require.Equal(t, original.SerialNumber, stored.SerialNumber)
			require.Equal(t, original.MacAddress, stored.MacAddress)
			status, err := deviceStore.GetDevicePairingStatusByIdentifier(ctx, original.DeviceIdentifier, admin.OrganizationID)
			require.NoError(t, err)
			require.Equal(t, tc.status, status)
			properties, err := deviceStore.GetDevicePropertiesForRename(ctx, admin.OrganizationID, []string{original.DeviceIdentifier}, false)
			require.NoError(t, err)
			require.Len(t, properties, 1)
			require.Equal(t, "original-worker", properties[0].WorkerName)
			if tc.macMatch {
				_, err = deviceStore.GetDeviceByDeviceIdentifier(ctx, candidate.DeviceIdentifier, admin.OrganizationID)
				require.True(t, fleeterror.IsNotFoundError(err), "MAC reconciliation must not create another miner")
			}
		})
	}
}
