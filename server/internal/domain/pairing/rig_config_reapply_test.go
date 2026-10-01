package pairing_test

import (
	"context"
	"fmt"
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/pairing"
	pairingMocks "github.com/block/proto-fleet/server/internal/domain/pairing/mocks"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestPairDevices_ReappliesOnlySuccessfulTargetsOnceAfterPartialFailure(t *testing.T) {
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	adminUser := testContext.DatabaseService.CreateSuperAdminUser()
	orgID := adminUser.OrganizationID
	ctx := testutil.MockAuthContextForTesting(t.Context(), adminUser.DatabaseID, orgID)
	discoveredStore := sqlstores.NewSQLDiscoveredDeviceStore(testContext.ServiceProvider.DB)
	deviceStore := sqlstores.NewSQLDeviceStore(testContext.ServiceProvider.DB)
	identifiers := []string{"existing", "new-proto", "failed", "new-bitmain"}
	for i, identifier := range identifiers {
		device := &discoverymodels.DiscoveredDevice{
			Device: pb.Device{
				DeviceIdentifier: identifier,
				IpAddress:        fmt.Sprintf("192.168.12.%d", i+1),
				Port:             "80",
				UrlScheme:        "http",
				DriverName:       "proto",
				FirmwareVersion:  "test-firmware",
				Manufacturer:     "Proto",
				Model:            "Proto Rig",
			},
			OrgID: orgID,
		}
		if identifier == "new-bitmain" {
			device.DriverName = "antminer"
			device.Manufacturer = "Bitmain"
			device.Model = "S19"
		}
		_, err := discoveredStore.Save(ctx, discoverymodels.DeviceOrgIdentifier{DeviceIdentifier: identifier, OrgID: orgID}, device)
		require.NoError(t, err)
		if identifier == "existing" {
			require.NoError(t, deviceStore.InsertDevice(ctx, &device.Device, orgID, identifier))
			require.NoError(t, deviceStore.UpsertDevicePairing(ctx, &device.Device, orgID, pairing.StatusPaired))
		}
	}

	ctrl := gomock.NewController(t)
	mockPairer := pairingMocks.NewMockPairer(ctrl)
	mockPairer.EXPECT().PairDevice(gomock.Any(), gomock.Any(), gomock.Any()).Times(3).
		DoAndReturn(func(ctx context.Context, device *discoverymodels.DiscoveredDevice, _ *pb.Credentials) error {
			if device.DeviceIdentifier == "failed" {
				return fmt.Errorf("device is unreachable")
			}
			if err := deviceStore.InsertDevice(ctx, &device.Device, orgID, device.DeviceIdentifier); err != nil {
				return err
			}
			return deviceStore.UpsertDevicePairing(ctx, &device.Device, orgID, pairing.StatusPaired)
		})
	svc, ctx := setupTestService(t, testContext, adminUser, mockPairer, &MockDiscoverer{})
	var requests [][]string
	svc.WithRigConfigReapplier(func(ctx context.Context, gotOrgID, userID int64, targets []string) {
		require.Equal(t, orgID, gotOrgID)
		require.Equal(t, adminUser.DatabaseID, userID)
		for _, identifier := range targets {
			status, err := deviceStore.GetDevicePairingStatusByIdentifier(ctx, identifier, orgID)
			require.NoError(t, err)
			require.Equal(t, pairing.StatusPaired, status)
		}
		requests = append(requests, targets)
	})

	response, err := svc.PairDevices(ctx, createPairRequest(identifiers[1:]))
	require.NoError(t, err)
	require.Equal(t, []string{"failed"}, response.FailedDeviceIds)
	require.Len(t, requests, 1)
	require.ElementsMatch(t, []string{"new-proto", "new-bitmain"}, requests[0])
}
