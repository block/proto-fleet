package sqlstores_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/maintenance"
	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestMaintenanceClientClosurePreservesHostWork(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	org := insertMaintenanceTestOrg(t, db, "client-close")
	site := insertMaintenanceTestSite(t, db, org, "Repair site")
	var creator, other int64
	for i, name := range []string{"Creator", "Other account"} {
		var id int64
		require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities) SELECT gen_random_uuid()::text,$1,id,1,$2,31 FROM hosted_account WHERE organization_id=$1 AND depth=0 RETURNING id`, org, name).Scan(&id))
		if i == 0 {
			creator = id
		} else {
			other = id
		}
	}
	store := sqlstores.NewSQLMaintenanceStore(db)
	inventory := sqlstores.NewSQLInventoryStore(db)
	service := maintenance.NewService(store, store, inventory, sqlstores.NewSQLTransactor(db), nil)
	for i, tc := range []struct {
		name                                                           string
		status                                                         models.TicketStatus
		vendor, tracking                                               *string
		eta                                                            *time.Time
		activePart, consumedPart, foreignCreator, hostCreated, allowed bool
	}{
		{name: "untouched", status: models.TicketStatusOpen, allowed: true},
		{name: "in progress", status: models.TicketStatusInProgress},
		{name: "on hold", status: models.TicketStatusOnHold},
		{name: "sent to vendor", status: models.TicketStatusSentToVendor},
		{name: "reserved part", status: models.TicketStatusOpen, activePart: true},
		{name: "RMA vendor", status: models.TicketStatusOpen, vendor: maintenanceCloseString("Vendor")},
		{name: "RMA tracking", status: models.TicketStatusOpen, tracking: maintenanceCloseString("Tracking")},
		{name: "RMA ETA", status: models.TicketStatusOpen, eta: maintenanceCloseTime(time.Now())},
		{name: "other creator", status: models.TicketStatusOpen, foreignCreator: true},
		{name: "host creator", status: models.TicketStatusOpen, hostCreated: true},
		{name: "historical consumed part", status: models.TicketStatusOpen, consumedPart: true, allowed: true},
		{name: "blank RMA text", status: models.TicketStatusOpen, vendor: maintenanceCloseString("  "), tracking: maintenanceCloseString(""), allowed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			owner := &creator
			if tc.foreignCreator {
				owner = &other
			}
			if tc.hostCreated {
				owner = nil
			}
			operational := "Host operational notes"
			ticket, err := store.CreateRepairTicket(ctx, models.CreateParams{OrgID: org, SiteID: &site, Category: models.TicketCategoryInfrastructure, CreatedByAccountID: owner, Component: "Power", Notes: &operational, IdempotencyKey: fmt.Sprintf("client-close-%d", i), CreateRequestHash: maintenanceCreateRequestHash}, fmt.Sprintf("TK-%04d", i+1))
			require.NoError(t, err)
			_, err = db.ExecContext(ctx, "UPDATE repair_ticket SET status=$1,rma_vendor=$2,rma_tracking=$3,rma_eta=$4 WHERE id=$5", tc.status, tc.vendor, tc.tracking, tc.eta, ticket.ID)
			require.NoError(t, err)
			var partID int64
			if tc.activePart || tc.consumedPart {
				allocated := 0
				if tc.activePart {
					allocated = 1
				}
				require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO inventory_part(org_id,site_id,name,type,on_hand,allocated,reorder_point) VALUES($1,$2,$4,'cooling',10,$3,0) RETURNING id`, org, site, allocated, fmt.Sprintf("Fan %d", i)).Scan(&partID))
				require.NoError(t, store.InsertTicketPart(ctx, org, ticket.ID, partID, "Fan", 1))
				if tc.consumedPart {
					_, err = db.ExecContext(ctx, "UPDATE repair_ticket_part SET consumed_at=clock_timestamp() WHERE ticket_id=$1", ticket.ID)
					require.NoError(t, err)
				}
			}
			before, err := store.GetRepairTicket(ctx, org, ticket.ID)
			require.NoError(t, err)
			partsBefore, err := store.ListTicketParts(ctx, org, ticket.ID)
			require.NoError(t, err)
			allowed, err := store.CanAccountCloseTicket(ctx, org, ticket.ID, creator)
			require.NoError(t, err)
			require.Equal(t, tc.allowed, allowed)
			status, resolution, note := models.TicketStatusCompleted, models.TicketResolutionNoActionNeeded, "Client no longer needs repair"
			_, err = service.UpdateRepairTicket(ctx, models.UpdateParams{OrgID: org, ID: ticket.ID, ExpectedUpdatedAt: before.UpdatedAt, ClosingAccountID: &creator, Status: &status, Resolution: &resolution, ResolutionNote: &note})
			if tc.allowed {
				require.NoError(t, err)
			} else {
				require.True(t, fleeterror.IsFailedPreconditionError(err), "%v", err)
			}
			after, err := store.GetRepairTicket(ctx, org, ticket.ID)
			require.NoError(t, err)
			if tc.allowed {
				require.Equal(t, models.TicketStatusCompleted, after.Status)
			} else {
				require.Equal(t, before, after)
			}
			require.Equal(t, before.Notes, after.Notes)
			require.Equal(t, before.RMAVendor, after.RMAVendor)
			require.Equal(t, before.RMATracking, after.RMATracking)
			require.Equal(t, before.RMAEta, after.RMAEta)
			partsAfter, err := store.ListTicketParts(ctx, org, ticket.ID)
			require.NoError(t, err)
			require.Equal(t, partsBefore, partsAfter)
			if partID != 0 {
				part, err := inventory.Get(ctx, org, partID)
				require.NoError(t, err)
				require.EqualValues(t, 10, part.OnHand)
				if tc.activePart {
					require.EqualValues(t, 1, part.Allocated)
				} else {
					require.Zero(t, part.Allocated)
				}
			}
		})
	}
}

func maintenanceCloseString(value string) *string     { return &value }
func maintenanceCloseTime(value time.Time) *time.Time { return &value }

func TestMaintenanceClientClosureRechecksReservationAfterTicketLockWait(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	org := insertMaintenanceTestOrg(t, db, "client-close-race")
	site := insertMaintenanceTestSite(t, db, org, "Repair site")
	var creator, partID int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities) SELECT gen_random_uuid()::text,$1,id,1,'Creator',31 FROM hosted_account WHERE organization_id=$1 AND depth=0 RETURNING id`, org).Scan(&creator))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO inventory_part(org_id,site_id,name,type,on_hand,allocated,reorder_point) VALUES($1,$2,'Fan','cooling',10,1,0) RETURNING id`, org, site).Scan(&partID))
	store := sqlstores.NewSQLMaintenanceStore(db)
	service := maintenance.NewService(store, store, sqlstores.NewSQLInventoryStore(db), sqlstores.NewSQLTransactor(db), nil)
	ticket, err := store.CreateRepairTicket(ctx, models.CreateParams{OrgID: org, SiteID: &site, Category: models.TicketCategoryInfrastructure, CreatedByAccountID: &creator, Component: "Power", IdempotencyKey: "close-reservation-race", CreateRequestHash: maintenanceCreateRequestHash}, "TK-0001")
	require.NoError(t, err)
	allowed, err := store.CanAccountCloseTicket(ctx, org, ticket.ID, creator)
	require.NoError(t, err)
	require.True(t, allowed, "client may observe closure availability before the host reserves a part")
	reserving, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = reserving.Rollback() }()
	var lockedID int64
	require.NoError(t, reserving.QueryRowContext(ctx, "SELECT id FROM repair_ticket WHERE org_id=$1 AND id=$2 FOR UPDATE", org, ticket.ID).Scan(&lockedID))
	var reservingPID int
	require.NoError(t, reserving.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&reservingPID))
	_, err = reserving.ExecContext(ctx, "INSERT INTO repair_ticket_part(org_id,ticket_id,inventory_part_id,part_name,quantity) VALUES($1,$2,$3,'Fan',1)", org, ticket.ID, partID)
	require.NoError(t, err)
	result := make(chan error, 1)
	go func() {
		status, resolution, note := models.TicketStatusCompleted, models.TicketResolutionNoActionNeeded, "Client no longer needs repair"
		_, closeErr := service.UpdateRepairTicket(ctx, models.UpdateParams{OrgID: org, ID: ticket.ID, ExpectedUpdatedAt: ticket.UpdatedAt, ClosingAccountID: &creator, Status: &status, Resolution: &resolution, ResolutionNote: &note})
		result <- closeErr
	}()
	waitForMaintenancePlacementLock(t, db, reservingPID)
	require.NoError(t, reserving.Commit())
	select {
	case closeErr := <-result:
		require.True(t, fleeterror.IsFailedPreconditionError(closeErr), "%v", closeErr)
	case <-time.After(5 * time.Second):
		t.Fatal("client closure did not finish after host reservation committed")
	}
	current, err := store.GetRepairTicket(ctx, org, ticket.ID)
	require.NoError(t, err)
	require.Equal(t, models.TicketStatusOpen, current.Status)
	parts, err := store.ListTicketParts(ctx, org, ticket.ID)
	require.NoError(t, err)
	require.Len(t, parts, 1)
	require.Nil(t, parts[0].ConsumedAt)
	var onHand, allocated int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT on_hand,allocated FROM inventory_part WHERE id=$1", partID).Scan(&onHand, &allocated))
	require.Equal(t, 10, onHand)
	require.Equal(t, 1, allocated)
}
