package pairing_test

import (
	"context"
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/pairing"
	pairingmocks "github.com/block/proto-fleet/server/internal/domain/pairing/mocks"
	plugins "github.com/block/proto-fleet/server/internal/domain/plugins"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/secrets"
	"github.com/block/proto-fleet/server/internal/testutil"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	sdkmocks "github.com/block/proto-fleet/server/sdk/v1/mocks"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestRecoverySpoofNeverReceivesStoredCredentials(t *testing.T) {
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	admin := testContext.DatabaseService.CreateSuperAdminUser()
	store := testContext.ServiceProvider.DeviceStore
	encryption := testContext.ServiceProvider.EncryptService
	for _, corrupt := range []bool{false, true} {
		t.Run(map[bool]string{false: "valid stored credentials", true: "corrupt stored credentials"}[corrupt], func(t *testing.T) {
			created := testContext.DatabaseService.CreateAndAssignDevices(1, admin.OrganizationID)[0]
			device, err := store.GetDeviceByDeviceIdentifier(t.Context(), created.ID, admin.OrganizationID)
			require.NoError(t, err)
			device.SerialNumber = "recovery-miner-" + created.ID
			require.NoError(t, store.UpdateDeviceInfo(t.Context(), device, admin.OrganizationID))
			require.NoError(t, store.UpsertDevicePairing(t.Context(), device, admin.OrganizationID, pairing.StatusPaired))
			usernameEnc, err := encryption.Encrypt([]byte("admin"))
			require.NoError(t, err)
			passwordEnc, err := encryption.Encrypt([]byte("existing-miner-password"))
			require.NoError(t, err)
			if corrupt {
				passwordEnc = "invalid-ciphertext"
			}
			require.NoError(t, store.UpsertMinerCredentials(t.Context(), device, admin.OrganizationID, usernameEnc, secrets.NewText(passwordEnc)))
			ctrl := gomock.NewController(t)
			pairer := pairingmocks.NewMockPairer(ctrl) // No driver calls permitted.
			service, ctx := setupTestService(t, testContext, admin, pairer, &MockDiscoverer{})
			spoof := &discoverymodels.DiscoveredDevice{Device: pb.Device{IpAddress: "192.168.94.15", Port: "8080", UrlScheme: "http", DriverName: "proto", MacAddress: device.MacAddress, SerialNumber: device.SerialNumber}}
			require.False(t, service.IsSameDevice(ctx, spoof, created.ID, admin.OrganizationID))
			stored, err := store.GetMinerCredentials(t.Context(), device, admin.OrganizationID)
			require.NoError(t, err)
			require.Equal(t, usernameEnc, stored.GetUsername())
			require.Equal(t, passwordEnc, stored.GetPassword())
			status, err := store.GetDevicePairingStatusByIdentifier(t.Context(), created.ID, admin.OrganizationID)
			require.NoError(t, err)
			require.Equal(t, pairing.StatusPaired, status)
			approved, err := store.GetDeviceByDeviceIdentifier(t.Context(), created.ID, admin.OrganizationID)
			require.NoError(t, err)
			require.Equal(t, device.IpAddress, approved.IpAddress)
		})
	}
}

func TestMovedCloudMinerRequiresExplicitAuthenticationBeforeEndpointApproval(t *testing.T) {
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	admin := testContext.DatabaseService.CreateSuperAdminUser()
	store := testContext.ServiceProvider.DeviceStore
	discoveredStore := sqlstores.NewSQLDiscoveredDeviceStore(testContext.ServiceProvider.DB)
	created := testContext.DatabaseService.CreateAndAssignDevices(1, admin.OrganizationID)[0]
	device, err := store.GetDeviceByDeviceIdentifier(t.Context(), created.ID, admin.OrganizationID)
	require.NoError(t, err)
	device.SerialNumber = "moved-miner"
	require.NoError(t, store.UpdateDeviceInfo(t.Context(), device, admin.OrganizationID))
	require.NoError(t, store.UpsertDevicePairing(t.Context(), device, admin.OrganizationID, pairing.StatusPaired))
	usernameEnc, err := testContext.ServiceProvider.EncryptService.Encrypt([]byte("admin"))
	require.NoError(t, err)
	passwordEnc, err := testContext.ServiceProvider.EncryptService.Encrypt([]byte("existing-password"))
	require.NoError(t, err)
	require.NoError(t, store.UpsertMinerCredentials(t.Context(), device, admin.OrganizationID, usernameEnc, secrets.NewText(passwordEnc)))
	ctrl := gomock.NewController(t)
	driver := sdkmocks.NewMockDriver(ctrl)
	manager := plugins.NewManager(&plugins.Config{})
	require.NoError(t, manager.RegisterPluginForTest(&plugins.LoadedPlugin{Name: "test-proto", Identifier: sdk.DriverIdentifier{DriverName: "proto"}, Driver: driver, Caps: sdk.Capabilities{sdk.CapabilityPairing: true}}))
	pairer := plugins.NewPairer(manager, sqlstores.NewSQLTransactor(testContext.ServiceProvider.DB), discoveredStore, store, testContext.ServiceProvider.EncryptService)
	discoverer := &MockDiscoverer{}
	newIP := "192.168.94.15"
	discoverer.On("Discover", mock.Anything, newIP, "8080").Return(&discoverymodels.DiscoveredDevice{Device: pb.Device{IpAddress: newIP, Port: "8080", UrlScheme: "http", DriverName: "proto", MacAddress: device.MacAddress, SerialNumber: device.SerialNumber}}, nil)
	service, ctx := setupTestService(t, testContext, admin, pairer, discoverer)
	discover := func() *pb.Device {
		results, err := service.DiscoverWithIPList(ctx, &pb.IPListModeRequest{IpAddresses: []string{newIP}, Ports: []string{"8080"}})
		require.NoError(t, err)
		var candidates []*pb.Device
		for result := range results {
			require.Empty(t, result.Warning)
			candidates = append(candidates, result.Devices...)
		}
		require.Len(t, candidates, 1)
		return candidates[0]
	}
	candidate := discover()
	require.NotEqual(t, created.ID, candidate.DeviceIdentifier)
	assertNotApproved := func() {
		approved, err := store.GetDeviceByDeviceIdentifier(ctx, created.ID, admin.OrganizationID)
		require.NoError(t, err)
		require.Equal(t, device.IpAddress, approved.IpAddress)
		stored, err := store.GetMinerCredentials(ctx, device, admin.OrganizationID)
		require.NoError(t, err)
		require.Equal(t, passwordEnc, stored.GetPassword())
		status, err := store.GetDevicePairingStatusByIdentifier(ctx, created.ID, admin.OrganizationID)
		require.NoError(t, err)
		require.Equal(t, pairing.StatusPaired, status)
	}
	assertNotApproved()
	// Adding without credentials stages a separate authentication-needed row.
	// No driver calls or stored-password reads are allowed before approval.
	_, err = service.PairDevices(ctx, createPairRequest([]string{candidate.DeviceIdentifier}))
	require.NoError(t, err)
	assertNotApproved()
	status, err := store.GetDevicePairingStatusByIdentifier(ctx, candidate.DeviceIdentifier, admin.OrganizationID)
	require.NoError(t, err)
	require.Equal(t, pairing.StatusAuthenticationNeeded, status)
	require.Equal(t, candidate.DeviceIdentifier, discover().DeviceIdentifier, "pending candidates remain discoverable")

	request := createPairRequest([]string{candidate.DeviceIdentifier})
	wrongPassword := "wrong-password"
	request.Credentials = &pb.Credentials{Username: "admin", Password: &wrongPassword}
	driver.EXPECT().PairDevice(gomock.Any(), gomock.Any(), gomock.Any()).Return(sdk.DeviceInfo{}, fleeterror.NewUnauthenticatedError("rejected"))
	_, err = service.PairDevices(ctx, request)
	require.Error(t, err)
	assertNotApproved()

	password := "existing-password"
	request.Credentials = &pb.Credentials{Username: "admin", Password: &password}
	driver.EXPECT().PairDevice(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, info sdk.DeviceInfo, bundle sdk.SecretBundle) (sdk.DeviceInfo, error) {
		require.Equal(t, sdk.UsernamePassword{Username: "admin", Password: password}, bundle.Kind)
		info.MacAddress = device.MacAddress
		info.SerialNumber = device.SerialNumber
		info.FirmwareVersion = "1.2.3"
		return info, nil
	})
	_, err = service.PairDevices(ctx, request)
	require.NoError(t, err)
	approved, err := store.GetDeviceByDeviceIdentifier(ctx, created.ID, admin.OrganizationID)
	require.NoError(t, err)
	require.Equal(t, created.ID, approved.DeviceIdentifier)
	require.Equal(t, newIP, approved.IpAddress)
	status, err = store.GetDevicePairingStatusByIdentifier(ctx, created.ID, admin.OrganizationID)
	require.NoError(t, err)
	require.Equal(t, pairing.StatusPaired, status)
	_, err = discoveredStore.GetDevice(ctx, discoverymodels.DeviceOrgIdentifier{DeviceIdentifier: candidate.DeviceIdentifier, OrgID: admin.OrganizationID})
	require.True(t, fleeterror.IsNotFoundError(err), "approved candidate must be folded into original miner")
}
