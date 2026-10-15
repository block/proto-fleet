package sites_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"connectrpc.com/connect"
	inventorypb "github.com/block/proto-fleet/server/generated/grpc/fleetinventory/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/sites/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/containers"
	"github.com/block/proto-fleet/server/internal/domain/fleetinventory"
	"github.com/block/proto-fleet/server/internal/domain/sites"
	"github.com/block/proto-fleet/server/internal/domain/sites/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/handlers/handlerstest"
	siteshandler "github.com/block/proto-fleet/server/internal/handlers/sites"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestContainerSiteAssignmentPermissionsPlacementAndReads(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	conn := database.DB
	org := database.CreateSuperAdminUser().OrganizationID
	controller := database.CreateDevice(org, "proto")
	_, err = conn.ExecContext(t.Context(), `UPDATE discovered_device SET device_class=2 WHERE device_identifier=$1`, controller.ID)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `INSERT INTO device_pairing(device_id,pairing_status) VALUES($1,'PAIRED')`, controller.DatabaseID)
	require.NoError(t, err)
	q := sqlc.New(conn)
	_, err = q.AdvanceContainerInventory(t.Context(), sqlc.AdvanceContainerInventoryParams{DeviceIdentifier: controller.ID, ObservedAt: time.Now()})
	require.NoError(t, err)
	var tankID, moduleID int64
	require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO container_tank(device_id,position,first_seen_at,last_seen_at) VALUES($1,1,now(),now()) RETURNING id`, controller.DatabaseID).Scan(&tankID))
	require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO container_module(tank_id,position,first_seen_at,last_seen_at) VALUES($1,1,now(),now()) RETURNING id`, tankID).Scan(&moduleID))
	store := sqlstores.NewSQLSiteStore(conn)
	service := sites.NewService(store, sqlstores.NewSQLBuildingStore(conn), sqlstores.NewSQLCollectionStore(conn), nil, nil, sqlstores.NewSQLTransactor(conn), activity.NewService(sqlstores.NewSQLActivityStore(conn)))
	handler := siteshandler.NewHandler(service)
	first, err := store.CreateSite(t.Context(), models.CreateSiteParams{OrgID: org, Name: "First"})
	require.NoError(t, err)
	second, err := store.CreateSite(t.Context(), models.CreateSiteParams{OrgID: org, Name: "Second"})
	require.NoError(t, err)
	ctx := handlerstest.CtxWithPermissions(t, org, authz.PermSiteManage)
	move := func(ctx context.Context, target *int64, ids ...string) error {
		_, err := handler.AssignContainersToSite(ctx, connect.NewRequest(&pb.AssignContainersToSiteRequest{ContainerIdentifiers: ids, TargetSiteId: target}))
		return err
	}
	currentSite := func() sql.NullInt64 {
		var id sql.NullInt64
		require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT site_id FROM device WHERE id=$1`, controller.DatabaseID).Scan(&id))
		return id
	}
	require.NoError(t, move(ctx, &first.ID, controller.ID, controller.ID))
	for _, assignments := range [][]authz.Assignment{
		{handlerstest.SiteAssignment(first.ID, authz.PermSiteManage)},
		{handlerstest.SiteAssignment(second.ID, authz.PermSiteManage)},
		{handlerstest.OrgAssignment(authz.PermSiteManage), handlerstest.SiteAssignment(first.ID, authz.PermSiteRead)},
		{handlerstest.OrgAssignment(authz.PermSiteManage), handlerstest.SiteAssignment(second.ID, authz.PermSiteRead)},
	} {
		err := move(handlerstest.CtxWithAssignments(t, org, assignments...), &second.ID, controller.ID)
		require.Error(t, err)
		require.Equal(t, first.ID, currentSite().Int64)
	}
	require.Error(t, move(ctx, &second.ID, controller.ID, "missing"))
	require.Equal(t, first.ID, currentSite().Int64, "batch must roll back")
	require.Error(t, move(handlerstest.CtxWithPermissions(t, org+1000, authz.PermSiteManage), nil, controller.ID))
	require.Error(t, move(handlerstest.CtxWithPermissions(t, org), &second.ID, controller.ID))
	scoped := handlerstest.CtxWithAssignments(t, org, handlerstest.SiteAssignment(first.ID, authz.PermSiteManage), handlerstest.SiteAssignment(second.ID, authz.PermSiteManage))
	require.NoError(t, move(scoped, &second.ID, controller.ID))
	require.Equal(t, second.ID, currentSite().Int64)
	_, err = conn.ExecContext(t.Context(), `UPDATE container_module SET state='hashing',state_observed_at=now() WHERE id=$1`, moduleID)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `INSERT INTO container_module_latest_metrics(module_id,time,collected_at,hashrate_hs,power_w,temperature_c,efficiency_jh) VALUES($1,now(),now(),300e12,6000,65,2e-11)`, moduleID)
	require.NoError(t, err)
	summaries, err := q.ListContainerSiteStats(t.Context(), sqlc.ListContainerSiteStatsParams{OrgID: org, SiteIds: []int64{first.ID, second.ID}})
	require.NoError(t, err)
	require.Len(t, summaries, 2)
	for _, summary := range summaries {
		if summary.SiteID == first.ID {
			require.Zero(t, summary.ModuleCount)
			require.Zero(t, summary.TotalHashrateThs)
		} else {
			require.EqualValues(t, 1, summary.ModuleCount)
			require.EqualValues(t, 1, summary.ContainerCount)
			require.EqualValues(t, 1, summary.TankCount)
			require.EqualValues(t, 1, summary.HashingCount)
			require.Equal(t, 300.0, summary.TotalHashrateThs)
			require.Equal(t, 6.0, summary.TotalPowerKw)
			require.InDelta(t, 20.0, summary.AvgEfficiencyJth, 0.0001)
		}
	}
	locked, err := q.LockContainerCommandTarget(t.Context(), sqlc.LockContainerCommandTargetParams{OrgID: org, DeviceIdentifier: controller.ID})
	require.NoError(t, err)
	require.Equal(t, second.ID, locked.SiteID.Int64, "new equipment commands authorize the new site")
	require.Error(t, move(scoped, nil, controller.ID), "unassigned requires org-wide authority")
	projected, err := q.GetContainerModuleScope(t.Context(), sqlc.GetContainerModuleScopeParams{ModuleID: moduleID, OrgID: org})
	require.NoError(t, err)
	require.Equal(t, second.ID, projected.SiteID.Int64)
	_, err = q.GetContainerModuleScope(t.Context(), sqlc.GetContainerModuleScopeParams{ModuleID: moduleID, OrgID: org + 1000})
	require.ErrorIs(t, err, sql.ErrNoRows)
	containerService := containers.NewService(q)
	_, err = containerService.Get(t.Context(), org, controller.ID, func(site *int64) (bool, error) { return site != nil && *site == first.ID, nil })
	require.Error(t, err, "old site loses access immediately")
	container, err := containerService.Get(t.Context(), org, controller.ID, func(site *int64) (bool, error) { return site != nil && *site == second.ID, nil })
	require.NoError(t, err)
	require.Equal(t, second.ID, container.Site.Id)
	inventory := fleetinventory.NewService(q)
	historyRequest := &inventorypb.ListModuleHistoryRequest{ModuleId: moduleID, StartTime: timestamppb.New(time.Now().Add(-time.Hour)), EndTime: timestamppb.Now()}
	_, err = inventory.History(t.Context(), org, historyRequest, []fleetinventory.Scope{{Sites: []int64{first.ID}}})
	require.ErrorContains(t, err, "not accessible")
	_, err = inventory.History(t.Context(), org, historyRequest, []fleetinventory.Scope{{Sites: []int64{second.ID}}})
	require.NoError(t, err)
	_, err = store.AssignDevicesToSite(t.Context(), org, &first.ID, []string{controller.ID})
	require.ErrorContains(t, err, "AssignContainersToSite", "generic miner assignment must not bypass source authorization")
	require.NoError(t, move(ctx, nil, controller.ID))
	require.False(t, currentSite().Valid)
	unassigned, err := containerService.List(t.Context(), org, nil, true, func(*int64) (bool, error) { return true, nil })
	require.NoError(t, err)
	require.Len(t, unassigned, 1)
	require.NoError(t, move(ctx, &second.ID, controller.ID))
	_, err = service.DeleteSite(ctx, org, second.ID)
	require.NoError(t, err)
	require.False(t, currentSite().Valid, "site deletion unassigns the container and its children")
	var events int
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE event_type='container.assigned_to_site' AND organization_id=$1`, org).Scan(&events))
	require.Equal(t, 4, events)
}

func TestContainerSiteMigrationOwnershipPlacementAndRollback(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	conn := database.DB
	org := database.CreateSuperAdminUser().OrganizationID
	controller := database.CreateDevice(org, "proto")
	_, err = conn.ExecContext(t.Context(), `UPDATE discovered_device SET device_class=2 WHERE device_identifier=$1`, controller.ID)
	require.NoError(t, err)
	down, err := migrations.Current.ReadFile("current/001023_internal_container_site_scope.down.sql")
	require.NoError(t, err)
	up, err := migrations.Current.ReadFile("current/001023_internal_container_site_scope.up.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), string(down))
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `INSERT INTO container_inventory(device_id,observed_at) VALUES($1,now())`, controller.DatabaseID)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), string(up))
	require.NoError(t, err)
	var owner int64
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT org_id FROM container_inventory WHERE device_id=$1`, controller.DatabaseID).Scan(&owner))
	require.Equal(t, org, owner)
	_, err = conn.ExecContext(t.Context(), `UPDATE container_inventory SET org_id=$1 WHERE device_id=$2`, org+1000, controller.DatabaseID)
	require.Error(t, err)
	var building, rack int64
	require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO building(org_id,name) VALUES($1,'Building') RETURNING id`, org).Scan(&building))
	_, err = conn.ExecContext(t.Context(), `UPDATE device SET building_id=$1 WHERE id=$2`, building, controller.DatabaseID)
	require.ErrorContains(t, err, "containers cannot be placed")
	require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO device_set(org_id,type,label) VALUES($1,'rack','Rack') RETURNING id`, org).Scan(&rack))
	_, err = conn.ExecContext(t.Context(), `INSERT INTO device_set_membership(org_id,device_set_id,device_set_type,device_id,device_identifier) VALUES($1,$2,'rack',$3,$4)`, org, rack, controller.DatabaseID, controller.ID)
	require.ErrorContains(t, err, "containers cannot be placed")
	_, err = sqlstores.NewSQLBuildingStore(conn).AssignDevicesToBuilding(t.Context(), org, &building, []string{controller.ID})
	require.ErrorContains(t, err, "facility peers")
	_, err = sqlstores.NewSQLCollectionStore(conn).AddDevicesToCollection(t.Context(), org, rack, []string{controller.ID})
	require.ErrorContains(t, err, "facility peers")
	_, err = conn.ExecContext(t.Context(), string(down))
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), string(up))
	require.NoError(t, err)
}

func TestContainerSiteMigrationIgnoresDeletedPlacements(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	conn := database.DB
	org := database.CreateSuperAdminUser().OrganizationID
	buildingContainer := database.CreateDevice(org, "proto")
	rackContainer := database.CreateDevice(org, "proto")
	identifiers := []string{buildingContainer.ID, rackContainer.ID}
	down, err := migrations.Current.ReadFile("current/001023_internal_container_site_scope.down.sql")
	require.NoError(t, err)
	up, err := migrations.Current.ReadFile("current/001023_internal_container_site_scope.up.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), string(down))
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `UPDATE discovered_device SET device_class=2 WHERE device_identifier=ANY($1)`, identifiers)
	require.NoError(t, err)
	var building, rack int64
	require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO building(org_id,name) VALUES($1,'Legacy building') RETURNING id`, org).Scan(&building))
	_, err = conn.ExecContext(t.Context(), `UPDATE device SET building_id=$1 WHERE id=$2`, building, buildingContainer.DatabaseID)
	require.NoError(t, err)
	require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO device_set(org_id,type,label) VALUES($1,'rack','Legacy rack') RETURNING id`, org).Scan(&rack))
	_, err = conn.ExecContext(t.Context(), `INSERT INTO device_set_membership(org_id,device_set_id,device_set_type,device_id,device_identifier) VALUES($1,$2,'rack',$3,$4)`, org, rack, rackContainer.DatabaseID, rackContainer.ID)
	require.NoError(t, err)
	// Live invalid placements must still reject the upgrade atomically.
	tx, err := conn.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(t.Context(), string(up))
	require.ErrorContains(t, err, "CT1 containers must be removed")
	require.NoError(t, tx.Rollback())
	q := sqlc.New(conn)
	deleted, err := q.SoftDeleteDevices(t.Context(), sqlc.SoftDeleteDevicesParams{OrgID: org, DeviceIdentifiers: identifiers})
	require.NoError(t, err)
	require.EqualValues(t, 2, deleted)
	// Exercise both a deleted device alone and a deleted discovery as well.
	require.NoError(t, q.SoftDeleteDiscoveredDevicesForDeletedDevices(t.Context(), sqlc.SoftDeleteDiscoveredDevicesForDeletedDevicesParams{OrgID: org, DeviceIdentifiers: []string{rackContainer.ID}}))
	_, err = conn.ExecContext(t.Context(), string(up))
	require.NoError(t, err)
	var retainedBuilding int64
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT building_id FROM device WHERE id=$1`, buildingContainer.DatabaseID).Scan(&retainedBuilding))
	require.Equal(t, building, retainedBuilding)
	var retainedMemberships int
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT count(*) FROM device_set_membership WHERE device_id=$1`, rackContainer.DatabaseID).Scan(&retainedMemberships))
	require.Equal(t, 1, retainedMemberships)
}
