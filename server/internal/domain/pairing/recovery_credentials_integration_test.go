package pairing_test

import (
	"context"
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/pairing"
	pairingmocks "github.com/block/proto-fleet/server/internal/domain/pairing/mocks"
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
	}{
		{name: "matching miner receives decrypted credentials"},
		{name: "another miner cannot inherit the identity", identityMismatch: true},
		{name: "corrupt username never reaches the driver", corruptUsername: true},
		{name: "corrupt password never reaches the driver", corruptPassword: true},
		{name: "missing credentials never reach the driver", missingCredentials: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			created := testContext.DatabaseService.CreateAndAssignDevices(1, admin.OrganizationID)[0]
			device, err := store.GetDeviceByDeviceIdentifier(t.Context(), created.ID, admin.OrganizationID)
			require.NoError(t, err)
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
			ctrl := gomock.NewController(t)
			pairer := pairingmocks.NewMockPairer(ctrl)
			valid := !tc.corruptUsername && !tc.corruptPassword && !tc.missingCredentials
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
