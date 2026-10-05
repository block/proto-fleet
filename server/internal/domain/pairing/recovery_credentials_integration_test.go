package pairing_test

import (
	"context"
	"strings"
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/pairing"
	pairingmocks "github.com/block/proto-fleet/server/internal/domain/pairing/mocks"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/id"
	"github.com/block/proto-fleet/server/internal/infrastructure/secrets"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestIsSameDeviceStoredCredentials(t *testing.T) {
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	admin := testContext.DatabaseService.CreateSuperAdminUser()
	store := testContext.ServiceProvider.DeviceStore
	encryption := testContext.ServiceProvider.EncryptService
	for _, tc := range []struct {
		name               string
		corruptUsername    bool
		corruptPassword    bool
		missingCredentials bool
		identityMismatch   bool
		discoveryIdentity  string
		rejectDiscovery    bool
	}{
		{name: "matching miner receives decrypted credentials"},
		{name: "missing discovery identity never receives credentials", discoveryIdentity: "missing", rejectDiscovery: true},
		{name: "conflicting discovery MAC never receives credentials", discoveryIdentity: "conflicting MAC", rejectDiscovery: true},
		{name: "conflicting discovery serial never receives credentials", discoveryIdentity: "conflicting serial", rejectDiscovery: true},
		{name: "invalid discovery identity never receives credentials", discoveryIdentity: "invalid", rejectDiscovery: true},
		{name: "MAC-only discovery identity permits recovery", discoveryIdentity: "MAC only"},
		{name: "serial-only discovery identity permits recovery", discoveryIdentity: "serial only"},
		{name: "normalized discovery MAC permits recovery", discoveryIdentity: "normalized MAC"},
		{name: "another miner cannot inherit the identity", identityMismatch: true},
		{name: "corrupt username never reaches the driver", corruptUsername: true},
		{name: "corrupt password never reaches the driver", corruptPassword: true},
		{name: "missing credentials never reach the driver", missingCredentials: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
			if tc.corruptUsername {
				usernameEnc = "invalid-ciphertext"
			}
			if tc.corruptPassword {
				passwordEnc = "invalid-ciphertext"
			}
			if !tc.missingCredentials {
				require.NoError(t, store.UpsertMinerCredentials(t.Context(), device, admin.OrganizationID, usernameEnc, secrets.NewText(passwordEnc)))
			}
			candidate := &discoverymodels.DiscoveredDevice{Device: pb.Device{
				IpAddress: "192.168.94.15", Port: "8080", UrlScheme: "http", DriverName: "proto",
				MacAddress: device.MacAddress, SerialNumber: device.SerialNumber,
			}}
			switch tc.discoveryIdentity {
			case "missing":
				candidate.MacAddress, candidate.SerialNumber = "", ""
			case "conflicting MAC":
				candidate.MacAddress = "02:00:00:00:00:99"
			case "conflicting serial":
				candidate.SerialNumber = "unrelated-miner"
			case "invalid":
				candidate.MacAddress, candidate.SerialNumber = "invalid-mac", ""
			case "MAC only":
				candidate.SerialNumber = ""
			case "serial only":
				candidate.MacAddress = ""
			case "normalized MAC":
				candidate.MacAddress = strings.ToLower(device.MacAddress)
			}
			ctrl := gomock.NewController(t)
			pairer := pairingmocks.NewMockPairer(ctrl)
			valid := !tc.rejectDiscovery && !tc.corruptUsername && !tc.corruptPassword && !tc.missingCredentials
			if valid {
				pairer.EXPECT().GetDeviceInfo(gomock.Any(), candidate, gomock.Any()).DoAndReturn(
					func(_ context.Context, _ *discoverymodels.DiscoveredDevice, credentials *pb.Credentials) (*pb.Device, error) {
						require.Equal(t, "admin", credentials.GetUsername())
						require.Equal(t, "existing-miner-password", credentials.GetPassword())
						mac := device.MacAddress
						if tc.identityMismatch {
							mac = "02:00:00:00:00:99"
						}
						return &pb.Device{MacAddress: mac, SerialNumber: device.SerialNumber}, nil
					})
			}
			service, ctx := setupTestService(t, testContext, admin, pairer, &MockDiscoverer{})
			require.Equal(t, valid && !tc.identityMismatch, service.IsSameDevice(ctx, candidate, created.ID, admin.OrganizationID))
			status, err := store.GetDevicePairingStatusByIdentifier(t.Context(), created.ID, admin.OrganizationID)
			require.NoError(t, err)
			require.Equal(t, pairing.StatusPaired, status, "local credential failures must not demote the miner")
			if !tc.missingCredentials {
				stored, err := store.GetMinerCredentials(t.Context(), device, admin.OrganizationID)
				require.NoError(t, err)
				require.Equal(t, usernameEnc, stored.GetUsername())
				require.Equal(t, passwordEnc, stored.GetPassword(), "credentials must stay encrypted in storage")
			}
		})
	}
}

func TestIsSameDeviceAuthenticatedPartialIdentity(t *testing.T) {
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	admin := testContext.DatabaseService.CreateSuperAdminUser()
	store := testContext.ServiceProvider.DeviceStore
	encryption := testContext.ServiceProvider.EncryptService
	discoveredStore := sqlstores.NewSQLDiscoveredDeviceStore(testContext.ServiceProvider.DB)
	const mac = "AA:BB:CC:DD:EE:01"
	const serial = "recovery-serial"
	for _, tc := range []struct {
		name, storedMAC, storedSerial, probeMAC, probeSerial string
		want                                                 bool
	}{
		{name: "stored MAC gains serial", storedMAC: mac, probeMAC: mac, probeSerial: serial, want: true},
		{name: "stored serial gains MAC", storedSerial: serial, probeMAC: mac, probeSerial: serial, want: true},
		{name: "probe reports only matching MAC", storedMAC: mac, storedSerial: serial, probeMAC: mac, want: true},
		{name: "probe reports only matching serial", storedMAC: mac, storedSerial: serial, probeSerial: serial, want: true},
		{name: "probe normalizes identity", storedMAC: mac, storedSerial: serial, probeMAC: "aa-bb-cc-dd-ee-01", probeSerial: " recovery-serial ", want: true},
		{name: "matching MAC with conflicting serial", storedMAC: mac, storedSerial: serial, probeMAC: mac, probeSerial: "another-serial"},
		{name: "matching serial with conflicting MAC", storedMAC: mac, storedSerial: serial, probeMAC: "AA:BB:CC:DD:EE:02", probeSerial: serial},
		{name: "no shared identifier", storedMAC: mac, probeSerial: serial},
		{name: "no returned identity", storedMAC: mac, storedSerial: serial},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deviceID := id.GenerateID()
			storedSerial := strings.ReplaceAll(tc.storedSerial, serial, serial+"-"+deviceID)
			probeSerial := strings.ReplaceAll(tc.probeSerial, serial, serial+"-"+deviceID)
			device := &pb.Device{DeviceIdentifier: deviceID, MacAddress: tc.storedMAC, SerialNumber: storedSerial,
				IpAddress: "192.168.94.5", Port: "8080", UrlScheme: "http", DriverName: "proto"}
			_, err := discoveredStore.Save(t.Context(), discoverymodels.DeviceOrgIdentifier{DeviceIdentifier: deviceID, OrgID: admin.OrganizationID},
				&discoverymodels.DiscoveredDevice{Device: pb.Device{
					DeviceIdentifier: deviceID, MacAddress: tc.storedMAC, SerialNumber: storedSerial,
					IpAddress: "192.168.94.5", Port: "8080", UrlScheme: "http", DriverName: "proto",
				}, OrgID: admin.OrganizationID})
			require.NoError(t, err)
			require.NoError(t, store.InsertDevice(t.Context(), device, admin.OrganizationID, deviceID))
			require.NoError(t, store.UpsertDevicePairing(t.Context(), device, admin.OrganizationID, pairing.StatusPaired))
			persisted, err := store.GetDeviceByDeviceIdentifier(t.Context(), deviceID, admin.OrganizationID)
			require.NoError(t, err)
			require.Equal(t, tc.storedMAC, persisted.MacAddress)
			require.Equal(t, storedSerial, persisted.SerialNumber)

			usernameEnc, err := encryption.Encrypt([]byte("admin"))
			require.NoError(t, err)
			passwordEnc, err := encryption.Encrypt([]byte("existing-miner-password"))
			require.NoError(t, err)
			require.NoError(t, store.UpsertMinerCredentials(t.Context(), device, admin.OrganizationID, usernameEnc, secrets.NewText(passwordEnc)))
			candidate := &discoverymodels.DiscoveredDevice{Device: pb.Device{
				IpAddress: "192.168.94.15", Port: "8080", UrlScheme: "http", DriverName: "proto",
				MacAddress: tc.storedMAC, SerialNumber: storedSerial,
			}}
			ctrl := gomock.NewController(t)
			pairer := pairingmocks.NewMockPairer(ctrl)
			pairer.EXPECT().GetDeviceInfo(gomock.Any(), candidate, gomock.Any()).DoAndReturn(
				func(_ context.Context, _ *discoverymodels.DiscoveredDevice, credentials *pb.Credentials) (*pb.Device, error) {
					require.Equal(t, "admin", credentials.GetUsername())
					require.Equal(t, "existing-miner-password", credentials.GetPassword())
					return &pb.Device{MacAddress: tc.probeMAC, SerialNumber: probeSerial}, nil
				})
			service, ctx := setupTestService(t, testContext, admin, pairer, &MockDiscoverer{})
			require.Equal(t, tc.want, service.IsSameDevice(ctx, candidate, deviceID, admin.OrganizationID))
			status, err := store.GetDevicePairingStatusByIdentifier(ctx, deviceID, admin.OrganizationID)
			require.NoError(t, err)
			require.Equal(t, pairing.StatusPaired, status)
		})
	}
}
