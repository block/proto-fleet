package targets

import (
	"context"
	"strconv"
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/schedule/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/sites"
	"github.com/block/proto-fleet/server/internal/domain/sites/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestExpandSiteTargetsFollowContainerPlacement(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	ctx := t.Context()
	orgID := database.CreateSuperAdminUser().OrganizationID
	controller := database.CreateDevice(orgID, "proto")
	miner := database.CreateDevice(orgID, "antminer")
	_, err = database.DB.ExecContext(ctx, `UPDATE discovered_device SET device_class=2 WHERE device_identifier=$1`, controller.ID)
	require.NoError(t, err)
	_, err = database.DB.ExecContext(ctx, `INSERT INTO device_pairing(device_id,pairing_status) VALUES($1,'PAIRED'),($2,'PAIRED')`, controller.DatabaseID, miner.DatabaseID)
	require.NoError(t, err)
	_, err = sqlc.New(database.DB).AdvanceContainerInventory(ctx, sqlc.AdvanceContainerInventoryParams{
		DeviceIdentifier: controller.ID,
		ObservedAt:       time.Now(),
	})
	require.NoError(t, err)
	store := sqlstores.NewSQLSiteStore(database.DB)
	source, err := store.CreateSite(ctx, models.CreateSiteParams{OrgID: orgID, Name: "Source"})
	require.NoError(t, err)
	destination, err := store.CreateSite(ctx, models.CreateSiteParams{OrgID: orgID, Name: "Destination"})
	require.NoError(t, err)
	_, err = store.AssignDevicesToSite(ctx, orgID, &source.ID, []string{miner.ID})
	require.NoError(t, err)
	service := sites.NewService(store, nil, nil, nil, nil, sqlstores.NewSQLTransactor(database.DB), nil)
	move := func(siteID *int64) {
		t.Helper()
		count, err := service.AssignContainersToSite(ctx, orgID, siteID, []string{controller.ID}, func(context.Context, *int64) error { return nil })
		require.NoError(t, err)
		require.EqualValues(t, 1, count)
	}
	deviceStore := sqlstores.NewSQLDeviceStore(database.DB)
	expand := func(siteID int64) []string {
		t.Helper()
		identifiers, err := Expand(ctx, nil, deviceStore, []*pb.ScheduleTarget{{
			TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_SITE,
			TargetId:   strconv.FormatInt(siteID, 10),
		}}, orgID, nil)
		require.NoError(t, err)
		return identifiers
	}

	move(&source.ID)
	require.ElementsMatch(t, []string{miner.ID, controller.ID}, expand(source.ID))
	require.Empty(t, expand(destination.ID))

	move(&destination.ID)
	require.Equal(t, []string{miner.ID}, expand(source.ID))
	require.Equal(t, []string{controller.ID}, expand(destination.ID))

	move(nil)
	require.Equal(t, []string{miner.ID}, expand(source.ID))
	require.Empty(t, expand(destination.ID))
}

func TestExpandMinerTargetsResolveOnlyWithinOrg(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	ctx := t.Context()
	orgID := database.CreateSuperAdminUser().OrganizationID
	otherOrgID := database.CreateSuperAdminUser2().OrganizationID
	owned := database.CreateDevice(orgID, "proto")
	deleted := database.CreateDevice(orgID, "proto")
	foreign := database.CreateDevice(otherOrgID, "proto")
	_, err = database.DB.ExecContext(ctx, `UPDATE device SET deleted_at = now() WHERE id = $1`, deleted.DatabaseID)
	require.NoError(t, err)

	minerTarget := func(identifier string) *pb.ScheduleTarget {
		return &pb.ScheduleTarget{TargetType: pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_MINER, TargetId: identifier}
	}
	identifiers, err := Expand(ctx, nil, sqlstores.NewSQLDeviceStore(database.DB), []*pb.ScheduleTarget{
		minerTarget(foreign.ID),
		minerTarget(owned.ID),
		minerTarget(deleted.ID),
		minerTarget("unknown-miner"),
	}, orgID, nil)
	require.NoError(t, err)
	require.Equal(t, []string{owned.ID}, identifiers)

	identifiers, err = Expand(ctx, nil, sqlstores.NewSQLDeviceStore(database.DB), []*pb.ScheduleTarget{minerTarget(foreign.ID)}, orgID, nil)
	require.NoError(t, err)
	require.Empty(t, identifiers)
}
