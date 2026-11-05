package fleetmanagement_test

import (
	"context"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleetmanagement"
	"github.com/block/proto-fleet/server/internal/domain/miner"
	minermodels "github.com/block/proto-fleet/server/internal/domain/miner/models"
	sitesmodels "github.com/block/proto-fleet/server/internal/domain/sites/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	sdkmocks "github.com/block/proto-fleet/server/sdk/v1/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type deletionPluginManager struct{ driver sdk.Driver }

func (m deletionPluginManager) HasPluginForDriverName(name string) bool {
	return name == minermodels.DriverNameProto
}

func (m deletionPluginManager) GetCapabilitiesForDriverName(string) sdk.Capabilities {
	return sdk.Capabilities{}
}

func (m deletionPluginManager) GetDriverByDriverName(string) (sdk.Driver, error) {
	return m.driver, nil
}

func TestService_DeleteMiners_UnpairsAfterCachedBindingChanges(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database test in short mode")
	}
	for _, change := range []string{"site", "endpoint"} {
		t.Run(change, func(t *testing.T) {
			testContext := testutil.InitializeDBServiceInfrastructure(t)
			deps := testContext.ServiceProvider
			user := testContext.DatabaseService.CreateSuperAdminUser()
			ctx := testutil.MockAuthContextForTesting(t.Context(), user.DatabaseID, user.OrganizationID)
			identifier := testContext.DatabaseService.CreateTestMiners(user.OrganizationID, 1, "https://172.17.0.1:80")[0]
			q := sqlc.New(deps.DB)
			var deviceID int64
			require.NoError(t, deps.DB.QueryRowContext(ctx, "SELECT id FROM device WHERE org_id=$1 AND device_identifier=$2", user.OrganizationID, identifier).Scan(&deviceID))
			_, err := deps.DB.ExecContext(ctx, "UPDATE discovered_device SET driver_name='proto' WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)", deviceID)
			require.NoError(t, err)
			credential, err := deps.EncryptService.Encrypt([]byte("deletion-test-credential"))
			require.NoError(t, err)
			require.NoError(t, q.UpsertMinerCredentials(ctx, sqlc.UpsertMinerCredentialsParams{DeviceID: deviceID, UsernameEnc: credential, PasswordEnc: credential}))

			ctrl := gomock.NewController(t)
			device := sdkmocks.NewMockDevice(ctrl)
			driver := sdkmocks.NewMockDriver(ctrl)
			creations := 1 // Site-only changes reuse the same SDK backend under a fresh facade.
			if change == "endpoint" {
				creations = 2
			}
			driver.EXPECT().NewDevice(gomock.Any(), identifier, gomock.Any(), gomock.Any()).Return(sdk.NewDeviceResult{Device: device}, nil).Times(creations)
			device.EXPECT().Unpair(gomock.Any()).Return(nil).Times(1)
			minerService := miner.NewMinerService(deps.DB, deps.UserStore, deps.EncryptService, deps.FilesService,
				deletionPluginManager{driver: &sdkmocks.ScopedDriverFixture{Driver: driver}})
			cached, err := minerService.GetMinerFromDeviceIdentifier(ctx, minermodels.DeviceIdentifier(identifier))
			require.NoError(t, err)

			if change == "site" {
				site, err := sqlstores.NewSQLSiteStore(deps.DB).CreateSite(ctx, sitesmodels.CreateSiteParams{OrgID: user.OrganizationID, Name: "Changed site"})
				require.NoError(t, err)
				_, err = deps.DB.ExecContext(ctx, "UPDATE device SET site_id=$1 WHERE id=$2", site.ID, deviceID)
				require.NoError(t, err)
			} else {
				require.NoError(t, q.UpdateDeviceIPAssignment(ctx, sqlc.UpdateDeviceIPAssignmentParams{ID: deviceID, IpAddress: "192.0.2.75", Port: "80", UrlScheme: "https"}))
			}
			stillCached, err := minerService.GetMinerFromDeviceIdentifier(ctx, minermodels.DeviceIdentifier(identifier))
			require.NoError(t, err)
			require.Same(t, cached, stillCached, "the ordinary lookup remains primed with the stale facade")

			service := fleetmanagement.NewService(deps.DeviceStore, nil, fleetmanagement.NewMockTelemetryCollector(), minerService, nil, nil, nil, nil, nil, nil)
			response, err := service.DeleteMiners(ctx, &pb.DeleteMinersRequest{DeviceSelector: &pb.DeviceSelector{
				SelectionType: &pb.DeviceSelector_IncludeDevices{IncludeDevices: &commonpb.DeviceIdentifierList{DeviceIdentifiers: []string{identifier}}},
			}})
			require.NoError(t, err)
			require.Equal(t, int32(1), response.DeletedCount)
			drainCtx, cancelDrain := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancelDrain()
			require.NoError(t, service.WaitForPendingUnpairs(drainCtx))
			var claimed bool
			require.NoError(t, deps.DB.QueryRowContext(ctx, "SELECT attempt_id IS NOT NULL FROM device_deletion_unpair_permit WHERE device_id=$1", deviceID).Scan(&claimed))
			require.True(t, claimed, "the matching current binding must consume the deletion permit and reach Unpair")
		})
	}
}
