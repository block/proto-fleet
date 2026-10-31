package sqlstores_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestMaintenanceOpeningSnapshotRetainsIdentityAndAudiences(t *testing.T) {
	conn := testutil.GetTestDB(t)
	org := insertMaintenanceTestOrg(t, conn, "opening-snapshot")
	site := insertMaintenanceTestSite(t, conn, org, "Opening site")
	controller, module, board := insertMaintenancePhysicalTargets(t, conn, org, site, "opening-controller")
	owner, subowner, creator := insertMaintenanceSnapshotAccounts(t, conn, org)
	_, err := conn.ExecContext(t.Context(), `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,device_id,module_id,client_id,subclient_id) VALUES($1,6,$2,$3,$2,$4,$5)`, org, module, controller, owner, subowner)
	require.NoError(t, err)
	var installation int64
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT id FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL`, board).Scan(&installation))
	store := sqlstores.NewSQLMaintenanceStore(conn)
	ticket, err := store.CreateRepairTicket(t.Context(), models.CreateParams{OrgID: org, Category: models.TicketCategoryHashboard, Component: "Board", CreatedByAccountID: &creator, IdempotencyKey: "snapshot", CreateRequestHash: maintenanceCreateRequestHash, Targets: []models.TicketAssetSelection{{Kind: models.TicketCategoryHashboard, ID: board, ParentModuleID: &module}}}, "TK-0001")
	require.NoError(t, err)
	require.Len(t, ticket.Targets, 1)
	require.Equal(t, &module, ticket.Targets[0].ModuleID)
	require.Equal(t, &installation, ticket.Targets[0].InstallationID)
	require.Equal(t, &controller, ticket.Targets[0].ContainerID)
	require.Equal(t, "BM1368", ticket.Targets[0].Model)
	require.Len(t, ticket.Audiences, 3, "creator and both opening owners must be retained")
	var capturedOwner, capturedSubowner int64
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT client_id,subclient_id FROM repair_ticket_target WHERE org_id=$1 AND ticket_id=$2`, org, ticket.ID).Scan(&capturedOwner, &capturedSubowner))
	require.Equal(t, owner, capturedOwner)
	require.Equal(t, subowner, capturedSubowner)
	_, err = conn.ExecContext(t.Context(), `UPDATE hosted_account_asset SET client_id=NULL,subclient_id=NULL,revision=revision+1 WHERE organization_id=$1 AND kind=6 AND asset_id=$2`, org, module)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `UPDATE container_module SET model='Next model' WHERE id=$1`, module)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `UPDATE hashboard_installation SET removed_at=clock_timestamp() WHERE id=$1`, installation)
	require.NoError(t, err)
	retained, err := store.GetRepairTicket(t.Context(), org, ticket.ID)
	require.NoError(t, err)
	require.Equal(t, ticket.Targets, retained.Targets)
	require.Equal(t, ticket.Audiences, retained.Audiences)
}

func TestMaintenanceOpeningSnapshotRetainsMinerLocation(t *testing.T) {
	conn := testutil.GetTestDB(t)
	org := insertMaintenanceTestOrg(t, conn, "miner-snapshot")
	site := insertMaintenanceTestSite(t, conn, org, "Opening site")
	building := insertMaintenanceTestBuilding(t, conn, org, site, "Opening building")
	identifier := insertMaintenanceTestDevice(t, conn, org, "opening-miner")
	var miner, rack int64
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT id FROM device WHERE org_id=$1 AND device_identifier=$2`, org, identifier).Scan(&miner))
	require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO device_set(org_id,type,label) VALUES($1,'rack','Opening rack') RETURNING id`, org).Scan(&rack))
	_, err := conn.ExecContext(t.Context(), `INSERT INTO device_set_rack(device_set_id,org_id,site_id,building_id,rows,columns) VALUES($1,$2,$3,$4,1,1)`, rack, org, site, building)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `INSERT INTO device_set_membership(org_id,device_set_id,device_set_type,device_id,device_identifier) VALUES($1,$2,'rack',$3,$4)`, org, rack, miner, identifier)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `UPDATE discovered_device dd SET model='Opening miner model' FROM device d WHERE d.id=$1 AND dd.id=d.discovered_device_id`, miner)
	require.NoError(t, err)
	store := sqlstores.NewSQLMaintenanceStore(conn)
	ticket, err := store.CreateRepairTicket(t.Context(), models.CreateParams{OrgID: org, Category: models.TicketCategoryMiner, Component: "Miner", MinerIdentifier: &identifier, IdempotencyKey: "miner-snapshot", CreateRequestHash: maintenanceCreateRequestHash}, "TK-0001")
	require.NoError(t, err)
	require.Len(t, ticket.Targets, 1)
	require.Equal(t, identifier, ticket.Targets[0].MinerIdentifier)
	require.Equal(t, "Opening miner model", ticket.Targets[0].Model)
	require.Equal(t, &site, ticket.Targets[0].SiteID)
	var capturedMiner, capturedBuilding, capturedRack int64
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT miner_id,building_id,rack_id FROM repair_ticket_target WHERE org_id=$1 AND ticket_id=$2`, org, ticket.ID).Scan(&capturedMiner, &capturedBuilding, &capturedRack))
	require.Equal(t, miner, capturedMiner)
	require.Equal(t, building, capturedBuilding)
	require.Equal(t, rack, capturedRack)
}

func TestMaintenanceOpeningSnapshotFailureRollsBackTicketAndAudience(t *testing.T) {
	conn := testutil.GetTestDB(t)
	org := insertMaintenanceTestOrg(t, conn, "snapshot-atomicity")
	site := insertMaintenanceTestSite(t, conn, org, "Opening site")
	_, module, _ := insertMaintenancePhysicalTargets(t, conn, org, site, "atomic-controller")
	_, _, creator := insertMaintenanceSnapshotAccounts(t, conn, org)
	store := sqlstores.NewSQLMaintenanceStore(conn)
	_, err := store.CreateRepairTicket(t.Context(), models.CreateParams{OrgID: org, Category: models.TicketCategoryModule, Component: "Module", CreatedByAccountID: &creator, IdempotencyKey: "snapshot-atomicity", CreateRequestHash: maintenanceCreateRequestHash, Targets: []models.TicketAssetSelection{{Kind: models.TicketCategoryModule, ID: module}}, ClientAccountIDs: []string{"missing-account"}}, "TK-0001")
	require.Error(t, err, "a failure after snapshot inserts must roll back the entire create")
	for _, table := range []string{"repair_ticket", "repair_ticket_target", "repair_ticket_audience"} {
		var count int
		//nolint:gosec // The table names are fixed fixtures above.
		require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT count(*) FROM `+table+` WHERE org_id=$1`, org).Scan(&count))
		require.Zero(t, count, table)
	}
}

func TestMaintenanceOpeningSnapshotLocksOwnershipThroughCallerTransaction(t *testing.T) {
	conn := testutil.GetTestDB(t)
	org := insertMaintenanceTestOrg(t, conn, "owner-lock")
	site := insertMaintenanceTestSite(t, conn, org, "Opening site")
	controller, module, _ := insertMaintenancePhysicalTargets(t, conn, org, site, "locked-owner-controller")
	owner, _, _ := insertMaintenanceSnapshotAccounts(t, conn, org)
	_, err := conn.ExecContext(t.Context(), `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,device_id,module_id,client_id) VALUES($1,6,$2,$3,$2,$4)`, org, module, controller, owner)
	require.NoError(t, err)
	store := sqlstores.NewSQLMaintenanceStore(conn)
	rollback := errors.New("abort opening transaction")
	err = sqlstores.NewSQLTransactor(conn).RunInTx(t.Context(), func(ctx context.Context) error {
		_, createErr := store.CreateRepairTicket(ctx, models.CreateParams{OrgID: org, Category: models.TicketCategoryModule, Component: "Module", IdempotencyKey: "owner-lock", CreateRequestHash: maintenanceCreateRequestHash, Targets: []models.TicketAssetSelection{{Kind: models.TicketCategoryModule, ID: module}}}, "TK-0001")
		require.NoError(t, createErr)
		waiting, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()
		_, updateErr := conn.ExecContext(waiting, `UPDATE hosted_account_asset SET client_id=NULL,revision=revision+1 WHERE organization_id=$1 AND kind=6 AND asset_id=$2`, org, module)
		require.Error(t, updateErr, "opening ownership remains locked until the caller commits")
		return rollback
	})
	require.ErrorIs(t, err, rollback)
	var count int
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT count(*) FROM repair_ticket WHERE org_id=$1`, org).Scan(&count))
	require.Zero(t, count, "store must participate in the caller's rollback")
}

func insertMaintenanceSnapshotAccounts(t *testing.T, conn *sql.DB, org int64) (owner, subowner, creator int64) {
	t.Helper()
	require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities) SELECT gen_random_uuid()::text,$1,id,1,'Opening owner',31 FROM hosted_account WHERE organization_id=$1 AND depth=0 RETURNING id`, org).Scan(&owner))
	require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities) VALUES(gen_random_uuid()::text,$1,$2,2,'Opening subowner',31) RETURNING id`, org, owner).Scan(&subowner))
	require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities) SELECT gen_random_uuid()::text,$1,id,1,'Ticket creator',31 FROM hosted_account WHERE organization_id=$1 AND depth=0 RETURNING id`, org).Scan(&creator))
	return owner, subowner, creator
}
