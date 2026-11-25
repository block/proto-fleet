package pairing_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/pairing"
	pairingmocks "github.com/block/proto-fleet/server/internal/domain/pairing/mocks"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func TestPairIncludeDevicesNeverReachesAnotherOrgsDevices(t *testing.T) {
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	admin := testContext.DatabaseService.CreateSuperAdminUser()
	otherOrgID := testContext.DatabaseService.CreateSuperAdminUser2().OrganizationID
	discoveredStore := sqlstores.NewSQLDiscoveredDeviceStore(testContext.ServiceProvider.DB)
	deviceStore := testContext.ServiceProvider.DeviceStore

	saveForeignDiscovery := func(identifier, ip string) *discoverymodels.DiscoveredDevice {
		t.Helper()
		discovered := &discoverymodels.DiscoveredDevice{OrgID: otherOrgID, Device: pb.Device{
			DeviceIdentifier: identifier, IpAddress: ip, Port: "80", UrlScheme: "http",
			DriverName: "antminer", Manufacturer: "TestCorp", FirmwareVersion: "1.0",
		}}
		_, err := discoveredStore.SaveDirect(t.Context(), discoverymodels.DeviceOrgIdentifier{DeviceIdentifier: identifier, OrgID: otherOrgID}, discovered)
		require.NoError(t, err)
		return discovered
	}
	unpaired := saveForeignDiscovery("other-org-unpaired", "192.168.95.10")
	paired := saveForeignDiscovery("other-org-paired", "192.168.95.11")
	require.NoError(t, deviceStore.InsertDevice(t.Context(), &paired.Device, otherOrgID, paired.DeviceIdentifier))
	require.NoError(t, deviceStore.UpsertDevicePairing(t.Context(), &paired.Device, otherOrgID, pairing.StatusPaired))

	// No expectations: any plugin pairing attempt fails the test.
	pairer := pairingmocks.NewMockPairer(gomock.NewController(t))
	service, ctx := setupTestService(t, testContext, admin, pairer, &MockDiscoverer{})
	foreign := []string{unpaired.DeviceIdentifier, paired.DeviceIdentifier}

	resp, err := service.PairDevicesAllowAllFailed(ctx, createPairRequest(foreign))
	require.NoError(t, err)
	require.ElementsMatch(t, foreign, resp.GetFailedDeviceIds())

	_, err = service.PairDevices(ctx, createPairRequest(foreign))
	require.ErrorContains(t, err, "Failed to pair any devices")

	status, err := deviceStore.GetDevicePairingStatusByIdentifier(t.Context(), paired.DeviceIdentifier, otherOrgID)
	require.NoError(t, err)
	require.Equal(t, pairing.StatusPaired, status)
	for _, orgID := range []int64{admin.OrganizationID, otherOrgID} {
		_, err = deviceStore.GetDeviceByDeviceIdentifier(t.Context(), unpaired.DeviceIdentifier, orgID)
		require.True(t, fleeterror.IsNotFoundError(err), "pairing must not create a device for another org's discovery: %v", err)
	}
	_, err = deviceStore.GetDeviceByDeviceIdentifier(t.Context(), paired.DeviceIdentifier, admin.OrganizationID)
	require.True(t, fleeterror.IsNotFoundError(err), "another org's device must stay invisible to the caller: %v", err)
}
