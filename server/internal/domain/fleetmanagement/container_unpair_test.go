package fleetmanagement_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func TestService_DeleteMiners_ContainerActivity(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database test in short mode")
	}

	for _, tc := range []struct {
		name        string
		deviceClass commonpb.DeviceClass
		eventType   string
		description string
	}{
		{"container", commonpb.DeviceClass_DEVICE_CLASS_CONTAINER, "unpair_container", "Unpaired container"},
		{"miner", commonpb.DeviceClass_DEVICE_CLASS_RIG, "unpair_miners", "Unpaired miners"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			infra := testutil.InitializeDBServiceInfrastructure(t)
			user := infra.DatabaseService.CreateSuperAdminUser()
			ctx := testutil.MockAuthContextForTesting(t.Context(), user.DatabaseID, user.OrganizationID)
			db := infra.ServiceProvider.DB
			ids := infra.DatabaseService.CreateTestMiners(user.OrganizationID, 1, "https://172.17.0.1:80")
			_, err := db.ExecContext(ctx, `UPDATE discovered_device SET device_class = $1
				WHERE id = (SELECT discovered_device_id FROM device WHERE device_identifier = $2 AND org_id = $3)`,
				int32(tc.deviceClass), ids[0], user.OrganizationID)
			require.NoError(t, err)

			resp, err := infra.ServiceProvider.FleetManagementService.DeleteMiners(ctx, &pb.DeleteMinersRequest{
				DeviceSelector: &pb.DeviceSelector{
					SelectionType: &pb.DeviceSelector_IncludeDevices{
						IncludeDevices: &commonpb.DeviceIdentifierList{DeviceIdentifiers: ids},
					},
				},
			})
			require.NoError(t, err)
			require.Equal(t, int32(1), resp.DeletedCount)

			entries, err := sqlstores.NewSQLActivityStore(db).List(ctx, activitymodels.Filter{
				OrganizationID: user.OrganizationID,
				PageSize:       activitymodels.MaxPageSize,
			})
			require.NoError(t, err)
			for _, entry := range entries {
				if entry.Type != tc.eventType {
					continue
				}
				require.Equal(t, tc.description, entry.Description)
				require.NotNil(t, entry.ScopeCount)
				require.Equal(t, 1, *entry.ScopeCount)
				if tc.name == "container" {
					require.NotNil(t, entry.ScopeType)
					require.Equal(t, "container", *entry.ScopeType)
				}
				return
			}
			t.Fatalf("missing %s activity", tc.eventType)
		})
	}
}
