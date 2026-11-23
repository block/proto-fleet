package pairing_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	tmodels "github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	"github.com/block/proto-fleet/server/internal/testutil"
)

// Telemetry writes the version of a device that it polls only when the version
// changes. A direct probe can read the version before such a write and save its
// result after the write.
func TestDirectRediscoveryKeepsFirmwareVersionOnlyForTrustedPolledRow(t *testing.T) {
	const ip, port, mac, oldVersion, newVersion = "172.16.31.10", "8080", "AA:BB:CC:DD:EE:31", "1.0.0", "2.0.0"
	for _, tc := range []struct {
		name, status, scannedMAC, initial, telemetry, want string
	}{
		{"a MAC identity with a PAIRED device keeps the version from telemetry", "PAIRED", mac, oldVersion, newVersion, newVersion},
		{"a MAC identity with a DEFAULT_PASSWORD device keeps the version from telemetry", "DEFAULT_PASSWORD", mac, oldVersion, newVersion, newVersion},
		{"a device that telemetry does not poll takes the scanned version", "AUTHENTICATION_NEEDED", mac, oldVersion, newVersion, oldVersion},
		{"a row found by address takes the scanned version", "PAIRED", "AA:BB:CC:DD:EE:32", oldVersion, newVersion, oldVersion},
		{"a row with no stored version takes the scanned version", "PAIRED", mac, "", "", oldVersion},
	} {
		t.Run(tc.name, func(t *testing.T) {
			infra := testutil.InitializeDBServiceInfrastructure(t)
			admin := infra.DatabaseService.CreateSuperAdminUser()
			discoverer := &MockDiscoverer{}
			service, ctx := setupTestService(t, infra, admin, nil, discoverer)
			deviceStore := sqlstores.NewSQLDeviceStore(infra.ServiceProvider.DB)
			discoveredStore := sqlstores.NewSQLDiscoveredDeviceStore(infra.ServiceProvider.DB)
			probe := func(mac, version string) *discoverymodels.DiscoveredDevice {
				return &discoverymodels.DiscoveredDevice{Device: pb.Device{
					IpAddress: ip, Port: port, UrlScheme: "http", DriverName: "proto", MacAddress: mac, FirmwareVersion: version,
				}}
			}
			discover := func() string {
				results, err := service.DiscoverWithIPList(ctx, &pb.IPListModeRequest{IpAddresses: []string{ip}, Ports: []string{port}})
				require.NoError(t, err)
				var devices []*pb.Device
				for result := range results {
					devices = append(devices, result.Devices...)
				}
				require.Len(t, devices, 1)
				return devices[0].DeviceIdentifier
			}

			discoverer.On("Discover", mock.Anything, ip, port).Return(probe(mac, tc.initial), nil).Once()
			identifier := discover()
			doi := discoverymodels.DeviceOrgIdentifier{OrgID: admin.OrganizationID, DeviceIdentifier: identifier}
			discovered, err := discoveredStore.GetDevice(ctx, doi)
			require.NoError(t, err)
			require.NoError(t, deviceStore.InsertDevice(ctx, &discovered.Device, admin.OrganizationID, identifier))
			require.NoError(t, deviceStore.UpsertDevicePairing(ctx, &discovered.Device, admin.OrganizationID, tc.status))

			discoverer.On("Discover", mock.Anything, ip, port).Run(func(mock.Arguments) {
				if tc.telemetry != "" {
					assert.NoError(t, deviceStore.UpdateFirmwareVersion(ctx, tmodels.DeviceIdentifier(identifier), tc.telemetry))
				}
			}).Return(probe(tc.scannedMAC, oldVersion), nil).Once()
			require.Equal(t, identifier, discover())

			saved, err := discoveredStore.GetDevice(ctx, doi)
			require.NoError(t, err)
			require.Equal(t, tc.want, saved.FirmwareVersion)
		})
	}
}
