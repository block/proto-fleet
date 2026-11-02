package sqlstores_test

import (
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestMaintenanceBoardTriageRetainsOpeningOwnerAndRemovedInstallation(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	org := insertMaintenanceTestOrg(t, db, "board-triage")
	identifier := insertMaintenanceTestDevice(t, db, org, "board-controller")
	var controller, module, board, installation, accountA, accountB int64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT id FROM device WHERE org_id=$1 AND device_identifier=$2`, org, identifier).Scan(&controller))
	_, err := db.ExecContext(ctx, `INSERT INTO container_inventory(device_id,org_id,observed_at) VALUES($1,$2,NOW())`, controller, org)
	require.NoError(t, err)
	require.NoError(t, db.QueryRowContext(ctx, `WITH tank AS (INSERT INTO container_tank(device_id,position,first_seen_at,last_seen_at) VALUES($1,1,NOW(),NOW()) RETURNING id) INSERT INTO container_module(tank_id,position,first_seen_at,last_seen_at,model) SELECT id,1,NOW(),NOW(),'BM1368' FROM tank RETURNING id`, controller).Scan(&module))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO hashboard(org_id,first_seen_at,last_seen_at) VALUES($1,NOW(),NOW()) RETURNING id`, org).Scan(&board))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at) VALUES($1,$2,1,NOW()-interval '1 day',NOW()) RETURNING id`, board, module).Scan(&installation))
	observeMaintenanceModule(t, db, controller, module, board)
	for i, name := range []string{"Opening client", "New client"} {
		var id int64
		require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities) SELECT gen_random_uuid()::text,$1,id,1,$2,31 FROM hosted_account WHERE organization_id=$1 AND depth=0 RETURNING id`, org, name).Scan(&id))
		if i == 0 {
			accountA = id
		} else {
			accountB = id
		}
	}
	_, err = db.ExecContext(ctx, `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,device_id,hashboard_id,client_id) VALUES($1,7,$2,$3,$2,$4)`, org, board, controller, accountA)
	require.NoError(t, err)
	store := sqlstores.NewSQLMaintenanceStore(db)
	ticket, err := store.CreateRepairTicket(ctx, models.CreateParams{OrgID: org, Category: models.TicketCategoryModule, Component: "Module", IdempotencyKey: "triage", CreateRequestHash: maintenanceCreateRequestHash, Targets: []models.TicketAssetSelection{{Kind: models.TicketCategoryModule, ID: module}}}, "TK-0001")
	require.NoError(t, err)
	// Transfer ownership after the incident opens.
	_, err = db.ExecContext(ctx, `UPDATE hosted_account_asset SET client_id=$1,revision=revision+1 WHERE organization_id=$2 AND kind=7 AND asset_id=$3`, accountB, org, board)
	require.NoError(t, err)
	// The physical board has already been removed by the time triage identifies it.
	_, err = db.ExecContext(ctx, `UPDATE hashboard_installation SET removed_at=clock_timestamp() WHERE id=$1`, installation)
	require.NoError(t, err)
	options, err := store.ListTicketBoardOptions(ctx, org, ticket.ID, "")
	require.NoError(t, err)
	require.Len(t, options, 1, "triage picker must find the original detached board")
	require.Equal(t, board, options[0].ID)
	require.Equal(t, &installation, options[0].InstallationID)
	require.NoError(t, store.AddRepairTicketBoard(ctx, org, ticket.ID, board, module))
	got, err := store.GetRepairTicket(ctx, org, ticket.ID)
	require.NoError(t, err)
	require.Len(t, got.Targets, 2)
	require.Equal(t, &installation, got.Targets[1].InstallationID)
	require.Len(t, got.Audiences, 1)
	var inherited int64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT client_id FROM repair_ticket_target WHERE org_id=$1 AND ticket_id=$2 AND kind=4`, org, ticket.ID).Scan(&inherited))
	require.Equal(t, accountA, inherited)
	require.NotEqual(t, accountB, inherited)
}
