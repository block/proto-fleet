package plugins_test

import (
	"context"
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/pairing"
	"github.com/block/proto-fleet/server/internal/domain/plugins"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/id"
	"github.com/block/proto-fleet/server/internal/infrastructure/secrets"
	"github.com/block/proto-fleet/server/internal/testutil"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	sdkmocks "github.com/block/proto-fleet/server/sdk/v1/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestPairingReconciliationPreservesEstablishedMiner(t *testing.T) {
	for _, tc := range []struct {
		name, originalStatus string
		selectPending        bool
	}{
		{name: "paired original", originalStatus: pairing.StatusPaired},
		{name: "default-password original", originalStatus: pairing.StatusDefaultPassword},
		{name: "previously paired original needing authentication", originalStatus: pairing.StatusAuthenticationNeeded},
		{name: "pending candidate reconciles into original", originalStatus: pairing.StatusPaired, selectPending: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testContext := testutil.InitializeDBServiceInfrastructure(t)
			admin := testContext.DatabaseService.CreateSuperAdminUser()
			store := testContext.ServiceProvider.DeviceStore
			encryption := testContext.ServiceProvider.EncryptService
			discoveredStore := sqlstores.NewSQLDiscoveredDeviceStore(testContext.ServiceProvider.DB)
			originalID, candidateID := id.GenerateID(), id.GenerateID()
			const mac = "AA:BB:CC:DD:EE:01"
			var selected *discoverymodels.DiscoveredDevice
			for _, fixture := range []struct {
				identifier, ip, status string
				credentials            bool
			}{
				{identifier: originalID, ip: "192.168.94.5", status: tc.originalStatus, credentials: true},
				{identifier: candidateID, ip: "192.168.94.15", status: pairing.StatusAuthenticationNeeded},
			} {
				discovery := &discoverymodels.DiscoveredDevice{Device: pb.Device{
					DeviceIdentifier: fixture.identifier, MacAddress: mac, IpAddress: fixture.ip, Port: "4028", UrlScheme: "http", DriverName: "antminer",
				}, OrgID: admin.OrganizationID}
				_, err := discoveredStore.Save(t.Context(), discoverymodels.DeviceOrgIdentifier{DeviceIdentifier: fixture.identifier, OrgID: admin.OrganizationID}, discovery)
				require.NoError(t, err)
				require.NoError(t, store.InsertDevice(t.Context(), &discovery.Device, admin.OrganizationID, fixture.identifier))
				require.NoError(t, store.UpsertDevicePairing(t.Context(), &discovery.Device, admin.OrganizationID, fixture.status))
				if fixture.credentials {
					username, err := encryption.Encrypt([]byte("admin"))
					require.NoError(t, err)
					password, err := encryption.Encrypt([]byte("existing-password"))
					require.NoError(t, err)
					require.NoError(t, store.UpsertMinerCredentials(t.Context(), &discovery.Device, admin.OrganizationID, username, secrets.NewText(password)))
				}
				if (!tc.selectPending && fixture.identifier == originalID) || (tc.selectPending && fixture.identifier == candidateID) {
					selected = discovery
				}
			}
			ctrl := gomock.NewController(t)
			driver := sdkmocks.NewMockDriver(ctrl)
			driver.EXPECT().PairDevice(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, info sdk.DeviceInfo, _ sdk.SecretBundle) (sdk.DeviceInfo, error) {
					return info, nil
				})
			manager := plugins.NewManager(&plugins.Config{})
			require.NoError(t, manager.RegisterPluginForTest(&plugins.LoadedPlugin{Name: "reconciliation", Identifier: sdk.DriverIdentifier{DriverName: "antminer"},
				Driver: driver, Caps: sdk.Capabilities{sdk.CapabilityPairing: true}}))
			pairer := plugins.NewPairer(manager, sqlstores.NewSQLTransactor(testContext.ServiceProvider.DB), discoveredStore, store, encryption)
			password := "new-password"
			err := pairer.PairDevice(t.Context(), selected, &pb.Credentials{Username: "admin", Password: &password})
			if tc.selectPending {
				require.NoError(t, err)
				require.Equal(t, originalID, selected.DeviceIdentifier)
			} else {
				require.ErrorContains(t, err, "multiple paired devices found")
				require.Equal(t, originalID, selected.DeviceIdentifier)
			}
			original, err := discoveredStore.GetDevice(t.Context(), discoverymodels.DeviceOrgIdentifier{DeviceIdentifier: originalID, OrgID: admin.OrganizationID})
			require.NoError(t, err, "original discovery must remain active")
			if tc.selectPending {
				require.Equal(t, "192.168.94.15", original.IpAddress)
			} else {
				require.Equal(t, "192.168.94.5", original.IpAddress)
			}
			status, err := store.GetDevicePairingStatusByIdentifier(t.Context(), originalID, admin.OrganizationID)
			require.NoError(t, err)
			if tc.selectPending {
				require.Equal(t, pairing.StatusPaired, status)
			} else {
				require.Equal(t, tc.originalStatus, status)
			}
		})
	}
}
