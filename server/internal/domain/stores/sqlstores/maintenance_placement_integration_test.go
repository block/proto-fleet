package sqlstores_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/maintenance"
	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestMaintenanceCreateRechecksSiteAfterPhysicalPlacementLockWait(t *testing.T) {
	for _, kind := range []models.TicketCategory{models.TicketCategoryModule, models.TicketCategoryHashboard, models.TicketCategoryMiner} {
		t.Run(fmt.Sprintf("kind=%d", kind), func(t *testing.T) {
			db := testutil.GetTestDB(t)
			org := insertMaintenanceTestOrg(t, db, "target-lock")
			site := insertMaintenanceTestSite(t, db, org, "Original")
			destination := insertMaintenanceTestSite(t, db, org, "Destination")
			controller, module, board := insertMaintenancePhysicalTargets(t, db, org, site, "moving-target")
			selections := []models.TicketAssetSelection{{Kind: kind, ID: module}}
			if kind == models.TicketCategoryHashboard {
				selections[0].ID, selections[0].ParentModuleID = board, &module
			} else if kind == models.TicketCategoryMiner {
				selections = []models.TicketAssetSelection{{Kind: kind, ID: controller}}
			}
			store := sqlstores.NewSQLMaintenanceStore(db)
			service := maintenance.NewService(store, store, sqlstores.NewSQLInventoryStore(db), sqlstores.NewSQLTransactor(db), nil)
			moving, err := db.BeginTx(t.Context(), nil)
			require.NoError(t, err)
			defer func() { _ = moving.Rollback() }()
			var movingPID int
			require.NoError(t, moving.QueryRowContext(t.Context(), "SELECT pg_backend_pid()").Scan(&movingPID))
			_, err = moving.ExecContext(t.Context(), "UPDATE device SET site_id=$1 WHERE id=$2", destination, controller)
			require.NoError(t, err)
			ctx := authz.WithAuthorizedSiteScope(t.Context(), authz.AuthorizedSiteScope{SiteIDs: []int64{site}})
			result := make(chan error, 1)
			go func() {
				_, _, createErr := service.CreateRepairTicket(ctx, models.CreateParams{OrgID: org, IdempotencyKey: "placement-race", Category: kind, Component: "Power", Targets: selections})
				result <- createErr
			}()
			waitForMaintenancePlacementLock(t, db, movingPID)
			require.NoError(t, moving.Commit())
			select {
			case createErr := <-result:
				if kind == models.TicketCategoryMiner {
					require.True(t, fleeterror.IsFailedPreconditionError(createErr), "%v", createErr)
				} else {
					require.True(t, fleeterror.IsForbiddenError(createErr), "%v", createErr)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("ticket create did not finish after placement committed")
			}
			var count int
			require.NoError(t, db.QueryRowContext(t.Context(), "SELECT count(*) FROM repair_ticket WHERE org_id=$1", org).Scan(&count))
			require.Zero(t, count, "out-of-scope movement must not create a ticket or snapshot")
		})
	}
}

func TestMaintenanceReplacementModelRechecksSiteAfterPlacementLockWait(t *testing.T) {
	db := testutil.GetTestDB(t)
	org := insertMaintenanceTestOrg(t, db, "repl-lock")
	site := insertMaintenanceTestSite(t, db, org, "Original")
	destination := insertMaintenanceTestSite(t, db, org, "Destination")
	_, original, _ := insertMaintenancePhysicalTargets(t, db, org, site, "original-module")
	controller, replacement, _ := insertMaintenancePhysicalTargets(t, db, org, site, "replacement-module")
	_, err := db.ExecContext(t.Context(), "UPDATE container_module SET model='Opening replacement model' WHERE id=$1", replacement)
	require.NoError(t, err)
	store := sqlstores.NewSQLMaintenanceStore(db)
	service := maintenance.NewService(store, store, sqlstores.NewSQLInventoryStore(db), sqlstores.NewSQLTransactor(db), nil)
	ticket, _, err := service.CreateRepairTicket(t.Context(), models.CreateParams{OrgID: org, IdempotencyKey: "replacement-race", Category: models.TicketCategoryModule, Component: "Power", Targets: []models.TicketAssetSelection{{Kind: models.TicketCategoryModule, ID: original}}})
	require.NoError(t, err)
	moving, err := db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer func() { _ = moving.Rollback() }()
	var movingPID int
	require.NoError(t, moving.QueryRowContext(t.Context(), "SELECT pg_backend_pid()").Scan(&movingPID))
	_, err = moving.ExecContext(t.Context(), "UPDATE device SET site_id=$1 WHERE id=$2", destination, controller)
	require.NoError(t, err)
	ctx := authz.WithAuthorizedSiteScope(t.Context(), authz.AuthorizedSiteScope{SiteIDs: []int64{site}})
	result := make(chan error, 1)
	go func() {
		model := "BM1368"
		_, updateErr := service.UpdateTicketTarget(ctx, models.UpdateTargetParams{OrgID: org, TicketID: ticket.ID, Kind: models.TicketCategoryModule, AssetID: original, ExpectedUpdatedAt: ticket.UpdatedAt, ReplacementModuleID: &replacement, ReplacementModuleModel: &model})
		result <- updateErr
	}()
	waitForMaintenancePlacementLock(t, db, movingPID)
	require.NoError(t, moving.Commit())
	select {
	case updateErr := <-result:
		require.True(t, fleeterror.IsForbiddenError(updateErr), "%v", updateErr)
	case <-time.After(5 * time.Second):
		t.Fatal("replacement model update did not finish after placement committed")
	}
	var model string
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT model FROM container_module WHERE id=$1", replacement).Scan(&model))
	require.Equal(t, "Opening replacement model", model)
	current, err := store.GetRepairTicket(t.Context(), org, ticket.ID)
	require.NoError(t, err)
	require.Equal(t, ticket, current, "failed live inventory authorization preserves opening evidence")
}

func TestMaintenanceTargetPlacementLocksModuleAndBoardInstallation(t *testing.T) {
	db := testutil.GetTestDB(t)
	org := insertMaintenanceTestOrg(t, db, "installation-lock")
	site := insertMaintenanceTestSite(t, db, org, "Original")
	_, module, board := insertMaintenancePhysicalTargets(t, db, org, site, "installed-board")
	store := sqlstores.NewSQLMaintenanceStore(db)
	require.NoError(t, sqlstores.NewSQLTransactor(db).RunInTx(t.Context(), func(ctx context.Context) error {
		require.NoError(t, store.LockTicketAssetPlacements(ctx, org, []models.TicketAssetSelection{{Kind: models.TicketCategoryHashboard, ID: board, ParentModuleID: &module}}))
		for _, mutation := range []struct {
			query string
			id    int64
		}{
			{"UPDATE container_module SET retired_at=clock_timestamp() WHERE id=$1", module},
			{"UPDATE hashboard_installation SET removed_at=clock_timestamp() WHERE hashboard_id=$1 AND removed_at IS NULL", board},
		} {
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			_, err := db.ExecContext(ctx, mutation.query, mutation.id)
			cancel()
			require.Error(t, err, "placement mutation must wait for the ticket snapshot transaction")
		}
		return nil
	}))
}

func insertMaintenancePhysicalTargets(t *testing.T, db *sql.DB, org, site int64, name string) (controller, module, board int64) {
	t.Helper()
	identifier := insertMaintenanceTestDevice(t, db, org, name)
	require.NoError(t, db.QueryRowContext(t.Context(), "UPDATE device SET site_id=$1 WHERE org_id=$2 AND device_identifier=$3 RETURNING id", site, org, identifier).Scan(&controller))
	_, err := db.ExecContext(t.Context(), "INSERT INTO container_inventory(device_id,org_id,observed_at) VALUES($1,$2,NOW())", controller, org)
	require.NoError(t, err)
	require.NoError(t, db.QueryRowContext(t.Context(), `WITH tank AS (INSERT INTO container_tank(device_id,position,first_seen_at,last_seen_at) VALUES($1,1,NOW(),NOW()) RETURNING id) INSERT INTO container_module(tank_id,position,first_seen_at,last_seen_at,model) SELECT id,1,NOW(),NOW(),'BM1368' FROM tank RETURNING id`, controller).Scan(&module))
	require.NoError(t, db.QueryRowContext(t.Context(), "INSERT INTO hashboard(org_id,first_seen_at,last_seen_at) VALUES($1,NOW(),NOW()) RETURNING id", org).Scan(&board))
	_, err = db.ExecContext(t.Context(), "INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at) VALUES($1,$2,1,NOW(),NOW())", board, module)
	require.NoError(t, err)
	observeMaintenanceModule(t, db, controller, module, board)
	return controller, module, board
}

func waitForMaintenancePlacementLock(t *testing.T, db *sql.DB, blockingPID int) {
	t.Helper()
	require.Eventually(t, func() bool {
		var waiting bool
		err := db.QueryRowContext(t.Context(), "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1::integer=ANY(pg_blocking_pids(pid)))", blockingPID).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond, "ticket operation must wait for the physical placement lock")
}

// One installed board and two confirmed empty slots form a complete module.
func observeMaintenanceModule(t *testing.T, db *sql.DB, controller, module, board int64) {
	t.Helper()
	_, placementErr := db.ExecContext(t.Context(), `UPDATE hashboard_installation SET site_id=(SELECT site_id FROM device WHERE id=$1) WHERE module_id=$2`, controller, module)
	require.NoError(t, placementErr)
	_, err := db.ExecContext(t.Context(), `UPDATE container_module SET last_seen_at=(SELECT observed_at FROM container_inventory WHERE device_id=$1) WHERE id=$2`, controller, module)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `INSERT INTO hashboard_observation(hashboard_id,device_id,site_id,last_seen_at) SELECT $1,id,site_id,now() FROM device WHERE id=$2`, board, controller)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `INSERT INTO hashboard_slot(module_id,position,occupancy,hashboard_id,observed_at)
 SELECT $1,p,CASE WHEN p=1 THEN 'occupied' ELSE 'empty' END,CASE WHEN p=1 THEN $2::bigint END,last_seen_at
 FROM container_module CROSS JOIN generate_series(1,3) p WHERE id=$1`, module, board)
	require.NoError(t, err)
}
