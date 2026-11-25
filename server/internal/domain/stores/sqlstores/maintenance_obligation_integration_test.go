package sqlstores_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestHashboardRepairObligationOpensWhenModuleTicketNamesHashboard(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	org := insertMaintenanceTestOrg(t, db, "oblig-history")
	site := insertMaintenanceTestSite(t, db, org, "Obligation site")
	controller, module, board := insertMaintenancePhysicalTargets(t, db, org, site, "obligation-controller")
	owner, subowner, creator := insertMaintenanceSnapshotAccounts(t, db, org)
	_, err := db.ExecContext(ctx, `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,device_id,hashboard_id,client_id,subclient_id) VALUES($1,7,$2,$3,$2,$4,$5)`, org, board, controller, owner, subowner)
	require.NoError(t, err)
	store := sqlstores.NewSQLMaintenanceStore(db)

	moduleTicket, err := store.CreateRepairTicket(ctx, models.CreateParams{OrgID: org, Category: models.TicketCategoryModule, Component: "Module", IdempotencyKey: "obligation-module", CreateRequestHash: maintenanceCreateRequestHash, Targets: []models.TicketAssetSelection{{Kind: models.TicketCategoryModule, ID: module}}}, "TK-0001")
	require.NoError(t, err)
	first := moduleTicket.ID
	require.Zero(t, countRows(t, db, `SELECT count(*) FROM hashboard_repair_obligation WHERE org_id=$1`, org), "a module ticket names no hashboard until an operator selects one")

	// The operator identifies the board after the ticket opened.
	require.NoError(t, store.AddRepairTicketBoard(ctx, org, first, board, module))
	var obligation, client int64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT id,client_id FROM hashboard_repair_obligation WHERE org_id=$1 AND hashboard_id=$2`, org, board).Scan(&obligation, &client))
	require.Equal(t, owner, client)
	// The selected hashboard target holds the subclient and placement of the opening.
	require.Equal(t, 1, countRows(t, db, `SELECT count(*) FROM repair_ticket_target WHERE org_id=$1 AND ticket_id=$2 AND kind=4 AND hashboard_id=$3 AND subclient_id=$4 AND module_id=$5 AND container_id=$6 AND site_id=$7`, org, first, board, subowner, module, controller, site))

	// A client ticket for the same hashboard joins the open obligation.
	second, err := store.CreateRepairTicket(ctx, models.CreateParams{OrgID: org, Category: models.TicketCategoryHashboard, Component: "Board", CreatedByAccountID: &creator, IdempotencyKey: "obligation-board", CreateRequestHash: maintenanceCreateRequestHash, Targets: []models.TicketAssetSelection{{Kind: models.TicketCategoryHashboard, ID: board, ParentModuleID: &module}}}, "TK-0002")
	require.NoError(t, err)
	require.Equal(t, 1, countRows(t, db, `SELECT count(*) FROM hashboard_repair_obligation WHERE org_id=$1`, org))
	require.Equal(t, 2, countRows(t, db, `SELECT count(*) FROM hashboard_repair_obligation_ticket WHERE org_id=$1 AND obligation_id=$2 AND ticket_id IN ($3,$4)`, org, obligation, first, second.ID))

	note := "Repaired"
	_, err = store.BulkCloseTickets(ctx, org, []int64{first, second.ID}, int16(models.TicketResolutionRepaired), int16(models.RepairLocationOnRack), &note)
	require.NoError(t, err)
	require.Equal(t, 1, countRows(t, db, `SELECT count(*) FROM hashboard_repair_obligation WHERE org_id=$1 AND restored_at IS NULL`, org), "closing every ticket must not end the obligation")
}

func TestHashboardRepairObligationIsPerClient(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	org := insertMaintenanceTestOrg(t, db, "oblig-clients")
	site := insertMaintenanceTestSite(t, db, org, "Obligation site")
	controller, module, board := insertMaintenancePhysicalTargets(t, db, org, site, "obligation-transfer")
	owner, _, nextOwner := insertMaintenanceSnapshotAccounts(t, db, org)
	store := sqlstores.NewSQLMaintenanceStore(db)

	unowned := createHashboardObligationTicket(t, store, org, module, board, 1)
	require.Zero(t, countRows(t, db, `SELECT count(*) FROM hashboard_repair_obligation WHERE org_id=$1`, org), "a hashboard with no client has no repair obligation")
	closeObligationTicket(t, store, org, unowned)

	_, err := db.ExecContext(ctx, `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,device_id,hashboard_id,client_id) VALUES($1,7,$2,$3,$2,$4)`, org, board, controller, owner)
	require.NoError(t, err)
	closeObligationTicket(t, store, org, createHashboardObligationTicket(t, store, org, module, board, 2))
	_, err = db.ExecContext(ctx, `UPDATE hosted_account_asset SET client_id=$1,revision=revision+1 WHERE organization_id=$2 AND kind=7 AND asset_id=$3`, nextOwner, org, board)
	require.NoError(t, err)
	createHashboardObligationTicket(t, store, org, module, board, 3)

	rows, err := db.QueryContext(ctx, `SELECT client_id FROM hashboard_repair_obligation WHERE org_id=$1 AND hashboard_id=$2 AND restored_at IS NULL ORDER BY id`, org, board)
	require.NoError(t, err)
	defer rows.Close()
	var clients []int64
	for rows.Next() {
		var id int64
		require.NoError(t, rows.Scan(&id))
		clients = append(clients, id)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []int64{owner, nextOwner}, clients, "a transfer must not move the first client's obligation")
}

func TestHashboardRepairObligationUsesSelectedBoardClientOnSharedModule(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	org := insertMaintenanceTestOrg(t, db, "oblig-shared")
	site := insertMaintenanceTestSite(t, db, org, "Obligation site")
	controller, module, board := insertMaintenancePhysicalTargets(t, db, org, site, "obligation-shared")
	owner, subowner, _ := insertMaintenanceSnapshotAccounts(t, db, org)
	// The host keeps the second board, so the module has no single client.
	var hostBoard int64
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO hashboard(org_id,first_seen_at,last_seen_at) VALUES($1,NOW(),NOW()) RETURNING id", org).Scan(&hostBoard))
	_, err := db.ExecContext(ctx, "INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at,site_id) VALUES($1,$2,2,NOW(),NOW(),$3)", hostBoard, module, site)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "INSERT INTO hashboard_observation(hashboard_id,device_id,site_id,last_seen_at) VALUES($1,$2,$3,NOW())", hostBoard, controller, site)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "UPDATE hashboard_slot SET occupancy='occupied',hashboard_id=$1 WHERE module_id=$2 AND position=2", hostBoard, module)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,device_id,hashboard_id,client_id,subclient_id) VALUES($1,7,$2,$3,$2,$4,$5)`, org, board, controller, owner, subowner)
	require.NoError(t, err)
	store := sqlstores.NewSQLMaintenanceStore(db)

	ticket, err := store.CreateRepairTicket(ctx, models.CreateParams{OrgID: org, Category: models.TicketCategoryModule, Component: "Module", IdempotencyKey: "obligation-shared", CreateRequestHash: maintenanceCreateRequestHash, Targets: []models.TicketAssetSelection{{Kind: models.TicketCategoryModule, ID: module}}}, "TK-0001")
	require.NoError(t, err)
	require.Equal(t, 1, countRows(t, db, `SELECT count(*) FROM repair_ticket_target WHERE org_id=$1 AND ticket_id=$2 AND kind=3 AND client_id IS NULL`, org, ticket.ID), "the fixture must be a module with no single client")

	require.NoError(t, store.AddRepairTicketBoard(ctx, org, ticket.ID, board, module))
	require.NoError(t, store.AddRepairTicketBoard(ctx, org, ticket.ID, hostBoard, module))
	var hashboard, client int64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT hashboard_id,client_id FROM hashboard_repair_obligation WHERE org_id=$1`, org).Scan(&hashboard, &client), "only the client's board has an obligation")
	require.Equal(t, []int64{board, owner}, []int64{hashboard, client})
	require.Equal(t, 1, countRows(t, db, `SELECT count(*) FROM repair_ticket_target WHERE org_id=$1 AND ticket_id=$2 AND kind=4 AND hashboard_id=$3 AND subclient_id=$4`, org, ticket.ID, board, subowner))
}

// A module with an incomplete observation has no hosted module membership. The
// board owners come from the installations, so the selected board still has
// its client.
func TestHashboardRepairObligationOpensForModuleWithoutMembership(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	org := insertMaintenanceTestOrg(t, db, "oblig-nomember")
	site := insertMaintenanceTestSite(t, db, org, "Obligation site")
	controller, module, board := insertMaintenancePhysicalTargets(t, db, org, site, "obligation-nomember")
	owner, _, _ := insertMaintenanceSnapshotAccounts(t, db, org)
	_, err := db.ExecContext(ctx, `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,device_id,hashboard_id,client_id) VALUES($1,7,$2,$3,$2,$4)`, org, board, controller, owner)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE hashboard_slot SET unavailable_reason='sensor fault' WHERE module_id=$1 AND position=3`, module)
	require.NoError(t, err)
	require.Zero(t, countRows(t, db, `SELECT count(*) FROM hosted_module_membership WHERE module_id=$1`, module), "the fixture must be a module with no membership")
	store := sqlstores.NewSQLMaintenanceStore(db)

	ticket, err := store.CreateRepairTicket(ctx, models.CreateParams{OrgID: org, Category: models.TicketCategoryModule, Component: "Module", IdempotencyKey: "obligation-nomember", CreateRequestHash: maintenanceCreateRequestHash, Targets: []models.TicketAssetSelection{{Kind: models.TicketCategoryModule, ID: module}}}, "TK-0001")
	require.NoError(t, err)
	require.NoError(t, store.AddRepairTicketBoard(ctx, org, ticket.ID, board, module))
	require.Equal(t, 1, countRows(t, db, `SELECT count(*) FROM hashboard_repair_obligation WHERE org_id=$1 AND hashboard_id=$2 AND client_id=$3`, org, board, owner))
}

// The obligation stores no start or placement. A reader takes them from the
// hashboard target of the linked ticket that opened first.
func TestHashboardRepairObligationStartIsEarliestLinkedTicket(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	org := insertMaintenanceTestOrg(t, db, "oblig-earliest")
	site := insertMaintenanceTestSite(t, db, org, "Obligation site")
	controller, module, board := insertMaintenancePhysicalTargets(t, db, org, site, "obligation-earliest")
	owner, _, _ := insertMaintenanceSnapshotAccounts(t, db, org)
	_, err := db.ExecContext(ctx, `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,device_id,hashboard_id,client_id) VALUES($1,7,$2,$3,$2,$4)`, org, board, controller, owner)
	require.NoError(t, err)
	store := sqlstores.NewSQLMaintenanceStore(db)

	moduleTicket, err := store.CreateRepairTicket(ctx, models.CreateParams{OrgID: org, Category: models.TicketCategoryModule, Component: "Module", IdempotencyKey: "obligation-earliest", CreateRequestHash: maintenanceCreateRequestHash, Targets: []models.TicketAssetSelection{{Kind: models.TicketCategoryModule, ID: module}}}, "TK-0001")
	require.NoError(t, err)
	// The container moves to a different site between the two ticket openings.
	laterSite := insertMaintenanceTestSite(t, db, org, "Later site")
	_, err = db.ExecContext(ctx, `UPDATE device SET site_id=$1 WHERE id=$2`, laterSite, controller)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE hashboard_installation SET site_id=$1 WHERE module_id=$2`, laterSite, module)
	require.NoError(t, err)
	boardTicket := createHashboardObligationTicket(t, store, org, module, board, 2)
	opening := func() (ticket, openingSite int64) {
		require.NoError(t, db.QueryRowContext(ctx, `SELECT r.id,t.site_id FROM hashboard_repair_obligation e
 JOIN hashboard_repair_obligation_ticket l ON l.org_id=e.org_id AND l.obligation_id=e.id
 JOIN repair_ticket r ON r.id=l.ticket_id AND r.org_id=l.org_id
 JOIN repair_ticket_target t ON t.org_id=l.org_id AND t.ticket_id=l.ticket_id AND t.kind=4 AND t.hashboard_id=e.hashboard_id
 WHERE e.org_id=$1 ORDER BY r.created_at,r.id LIMIT 1`, org).Scan(&ticket, &openingSite))
		return ticket, openingSite
	}
	ticket, openingSite := opening()
	require.Equal(t, []int64{boardTicket, laterSite}, []int64{ticket, openingSite})

	// The module ticket opened first, and now names the same hashboard.
	require.NoError(t, store.AddRepairTicketBoard(ctx, org, moduleTicket.ID, board, module))
	require.Equal(t, 1, countRows(t, db, `SELECT count(*) FROM hashboard_repair_obligation WHERE org_id=$1`, org))
	ticket, openingSite = opening()
	require.Equal(t, []int64{moduleTicket.ID, site}, []int64{ticket, openingSite}, "the start and placement are those of the earliest linked ticket")
	require.Equal(t, 1, countRows(t, db, `SELECT count(*) FROM hashboard_repair_obligation_ticket l JOIN repair_ticket r ON r.id=l.ticket_id WHERE l.org_id=$1 AND l.ticket_id=$2 AND l.linked_at>r.created_at`, org, moduleTicket.ID), "linked_at shows the later selection")
}

// The ticket transaction starts before a transfer and waits for it. The board
// selected later must have the client that the opening locked, not the client
// at the transaction start.
func TestHashboardRepairObligationUsesClientLockedAtModuleTicketOpening(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	org := insertMaintenanceTestOrg(t, db, "oblig-lockwait")
	site := insertMaintenanceTestSite(t, db, org, "Obligation site")
	controller, module, board := insertMaintenancePhysicalTargets(t, db, org, site, "obligation-lockwait")
	owner, _, nextOwner := insertMaintenanceSnapshotAccounts(t, db, org)
	_, err := db.ExecContext(ctx, `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,device_id,hashboard_id,client_id) VALUES($1,7,$2,$3,$2,$4)`, org, board, controller, owner)
	require.NoError(t, err)
	store := sqlstores.NewSQLMaintenanceStore(db)

	started, transferred, result := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var ticket int64
	go func() {
		result <- sqlstores.NewSQLTransactor(db).RunInTx(ctx, func(txCtx context.Context) error {
			close(started)
			<-transferred
			created, createErr := store.CreateRepairTicket(txCtx, models.CreateParams{OrgID: org, Category: models.TicketCategoryModule, Component: "Module", IdempotencyKey: "obligation-lockwait", CreateRequestHash: maintenanceCreateRequestHash, Targets: []models.TicketAssetSelection{{Kind: models.TicketCategoryModule, ID: module}}}, "TK-0001")
			if createErr == nil {
				ticket = created.ID
			}
			return createErr
		})
	}()
	<-started
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	var pid int
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid))
	_, err = tx.ExecContext(ctx, `UPDATE hosted_account_asset SET client_id=$1,revision=revision+1 WHERE organization_id=$2 AND kind=7 AND asset_id=$3`, nextOwner, org, board)
	require.NoError(t, err)
	close(transferred)
	waitForMaintenancePlacementLock(t, db, pid)
	require.NoError(t, tx.Commit())
	require.NoError(t, <-result)
	require.Equal(t, 1, countRows(t, db, `SELECT count(*) FROM repair_ticket r JOIN hosted_account_asset_interval i ON i.organization_id=r.org_id AND i.kind=7 AND i.asset_id=$2 AND i.client_id=$3 WHERE r.id=$1 AND i.started_at<=r.created_at AND i.ended_at>r.created_at`, ticket, board, owner), "the fixture must open the ticket in the previous client's period")

	require.NoError(t, store.AddRepairTicketBoard(ctx, org, ticket, board, module))
	require.Equal(t, 1, countRows(t, db, `SELECT count(*) FROM repair_ticket_target WHERE org_id=$1 AND ticket_id=$2 AND kind=4 AND client_id=$3`, org, ticket, nextOwner))
	require.Equal(t, 1, countRows(t, db, `SELECT count(*) FROM hashboard_repair_obligation WHERE org_id=$1 AND hashboard_id=$2 AND client_id=$3`, org, board, nextOwner))
}

func TestHashboardRepairObligationRollsBackWithTicket(t *testing.T) {
	db := testutil.GetTestDB(t)
	org := insertMaintenanceTestOrg(t, db, "oblig-rollback")
	site := insertMaintenanceTestSite(t, db, org, "Obligation site")
	controller, module, board := insertMaintenancePhysicalTargets(t, db, org, site, "obligation-rollback")
	owner, _, _ := insertMaintenanceSnapshotAccounts(t, db, org)
	_, err := db.ExecContext(t.Context(), `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,device_id,hashboard_id,client_id) VALUES($1,7,$2,$3,$2,$4)`, org, board, controller, owner)
	require.NoError(t, err)
	store := sqlstores.NewSQLMaintenanceStore(db)
	rollback := errors.New("abort ticket transaction")
	err = sqlstores.NewSQLTransactor(db).RunInTx(t.Context(), func(ctx context.Context) error {
		_, createErr := store.CreateRepairTicket(ctx, models.CreateParams{OrgID: org, Category: models.TicketCategoryHashboard, Component: "Board", IdempotencyKey: "obligation-rollback", CreateRequestHash: maintenanceCreateRequestHash, Targets: []models.TicketAssetSelection{{Kind: models.TicketCategoryHashboard, ID: board, ParentModuleID: &module}}}, "TK-0001")
		require.NoError(t, createErr)
		return rollback
	})
	require.ErrorIs(t, err, rollback)
	require.Zero(t, countRows(t, db, `SELECT (SELECT count(*) FROM hashboard_repair_obligation WHERE org_id=$1)+(SELECT count(*) FROM hashboard_repair_obligation_ticket WHERE org_id=$1)`, org))
}

func createHashboardObligationTicket(t *testing.T, store *sqlstores.SQLMaintenanceStore, org, module, board int64, n int) int64 {
	t.Helper()
	ticket, err := store.CreateRepairTicket(t.Context(), models.CreateParams{OrgID: org, Category: models.TicketCategoryHashboard, Component: "Board", IdempotencyKey: fmt.Sprintf("obligation-%d", n), CreateRequestHash: maintenanceCreateRequestHash, Targets: []models.TicketAssetSelection{{Kind: models.TicketCategoryHashboard, ID: board, ParentModuleID: &module}}}, fmt.Sprintf("TK-%04d", n))
	require.NoError(t, err)
	return ticket.ID
}

func closeObligationTicket(t *testing.T, store *sqlstores.SQLMaintenanceStore, org, ticket int64) {
	t.Helper()
	note := "Repaired"
	_, err := store.BulkCloseTickets(t.Context(), org, []int64{ticket}, int16(models.TicketResolutionRepaired), int16(models.RepairLocationOnRack), &note)
	require.NoError(t, err)
}

func countRows(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRowContext(t.Context(), query, args...).Scan(&n))
	return n
}
