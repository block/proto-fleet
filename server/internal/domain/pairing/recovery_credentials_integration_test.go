package pairing_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/ipscanner"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/pairing"
	pairingmocks "github.com/block/proto-fleet/server/internal/domain/pairing/mocks"
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

func TestStockAntminerRecoveryAtNewIP(t *testing.T) {
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	admin := testContext.DatabaseService.CreateSuperAdminUser()
	store := testContext.ServiceProvider.DeviceStore
	encryption := testContext.ServiceProvider.EncryptService
	discoveredStore := sqlstores.NewSQLDiscoveredDeviceStore(testContext.ServiceProvider.DB)
	for _, tc := range []struct {
		name                           string
		authFailure, identityMismatch  bool
		driverName                     string
		storedDriverName, discoveryMAC string
		want                           bool
	}{
		{name: "matching authenticated miner recovers", driverName: "antminer", want: true},
		{name: "different authenticated miner does not recover", driverName: "antminer", identityMismatch: true},
		{name: "unidentified rejection does not demote original", driverName: "antminer", authFailure: true},
		{name: "other drivers still require discovery identity", driverName: "proto"},
		{name: "stored driver must also be Antminer", driverName: "antminer", storedDriverName: "proto"},
		{name: "conflicting Antminer discovery remains rejected", driverName: "antminer", discoveryMAC: "AA:BB:CC:DD:EE:02"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deviceID := id.GenerateID()
			storedDriver := tc.driverName
			if tc.storedDriverName != "" {
				storedDriver = tc.storedDriverName
			}
			serial := "stock-antminer-" + deviceID
			const mac = "AA:BB:CC:DD:EE:01"
			device := &pb.Device{DeviceIdentifier: deviceID, MacAddress: mac, SerialNumber: serial,
				IpAddress: "192.168.94.5", Port: "4028", UrlScheme: "http", DriverName: storedDriver}
			_, err := discoveredStore.Save(t.Context(), discoverymodels.DeviceOrgIdentifier{DeviceIdentifier: deviceID, OrgID: admin.OrganizationID},
				&discoverymodels.DiscoveredDevice{Device: pb.Device{DeviceIdentifier: deviceID, MacAddress: mac, SerialNumber: serial,
					IpAddress: "192.168.94.5", Port: "4028", UrlScheme: "http", DriverName: storedDriver}, OrgID: admin.OrganizationID})
			require.NoError(t, err)
			require.NoError(t, store.InsertDevice(t.Context(), device, admin.OrganizationID, deviceID))
			require.NoError(t, store.UpsertDevicePairing(t.Context(), device, admin.OrganizationID, pairing.StatusPaired))
			userEnc, err := encryption.Encrypt([]byte("admin"))
			require.NoError(t, err)
			passwordEnc, err := encryption.Encrypt([]byte("existing-miner-password"))
			require.NoError(t, err)
			require.NoError(t, store.UpsertMinerCredentials(t.Context(), device, admin.OrganizationID, userEnc, secrets.NewText(passwordEnc)))
			ctrl := gomock.NewController(t)
			driver := sdkmocks.NewMockDriver(ctrl)
			// This is the stock Antminer DiscoverDevice contract: identity is unavailable
			// until authentication. The contract suite checks this against the real driver.
			driver.EXPECT().DiscoverDevice(gomock.Any(), "192.168.94.15", "4028").Return(sdk.DeviceInfo{
				Host: "192.168.94.15", Port: 4028, URLScheme: "http", Model: "Antminer S19", Manufacturer: "Bitmain", MacAddress: tc.discoveryMAC}, nil)
			if tc.driverName == "antminer" && tc.storedDriverName != "proto" && tc.discoveryMAC == "" {
				driver.EXPECT().NewDevice(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
					func(_ context.Context, probeID string, info sdk.DeviceInfo, access sdk.SecretBundle) (sdk.NewDeviceResult, error) {
						require.True(t, strings.HasPrefix(probeID, "pairing-info:"))
						require.Equal(t, "192.168.94.15", info.Host)
						require.Equal(t, sdk.UsernamePassword{Username: "admin", Password: "existing-miner-password"}, access.Kind)
						if tc.authFailure {
							return sdk.NewDeviceResult{}, sdk.NewErrorAuthenticationFailed(probeID)
						}
						handle := sdkmocks.NewMockDevice(ctrl)
						probeSerial := serial
						if tc.identityMismatch {
							probeSerial = "unrelated-miner"
						}
						handle.EXPECT().DescribeDevice(gomock.Any()).Return(sdk.DeviceInfo{MacAddress: mac, SerialNumber: probeSerial}, sdk.Capabilities{}, nil)
						handle.EXPECT().Close(gomock.Any()).Return(nil)
						return sdk.NewDeviceResult{Device: handle}, nil
					})
			}
			manager := plugins.NewManager(&plugins.Config{})
			require.NoError(t, manager.RegisterPluginForTest(&plugins.LoadedPlugin{Name: "stock-recovery", Identifier: sdk.DriverIdentifier{DriverName: tc.driverName},
				Driver: driver, Caps: sdk.Capabilities{sdk.CapabilityDiscovery: true, sdk.CapabilityPairing: true}}))
			pairer := plugins.NewPairer(manager, sqlstores.NewSQLTransactor(testContext.ServiceProvider.DB), discoveredStore, store, encryption)
			service, ctx := setupTestService(t, testContext, admin, pairer, &MockDiscoverer{})
			scanner := ipscanner.NewNetworkScanner(plugins.NewMultiTypeDiscoverer(manager), service, 1, slog.Default())
			matches, err := scanner.ScanSubnetForDevices(ctx, "192.168.94.15/32", []ipscanner.TargetDevice{{DeviceIdentifier: deviceID,
				DiscoveredDeviceIdentifier: deviceID, DriverName: tc.driverName, Port: "4028", OrgID: admin.OrganizationID}})
			require.NoError(t, err)
			if tc.want {
				require.Len(t, matches, 1)
				require.Equal(t, deviceID, matches[0].TargetDevice.DeviceIdentifier)
				require.Equal(t, "192.168.94.15", matches[0].DiscoveredIP)
			} else {
				require.Empty(t, matches)
			}
			status, err := store.GetDevicePairingStatusByIdentifier(ctx, deviceID, admin.OrganizationID)
			require.NoError(t, err)
			require.Equal(t, pairing.StatusPaired, status, "an identity-free probe cannot attribute an auth rejection to this miner")
		})
	}
}
