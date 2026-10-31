package sqlstores_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/buildings"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/maintenance"
	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
	"github.com/block/proto-fleet/server/internal/domain/sites"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	infradb "github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestMaintenanceReopenRetainsConsumedPartsAndResetsCompletion(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLMaintenanceStore(db)
	inventory := sqlstores.NewSQLInventoryStore(db)
	service := maintenance.NewService(store, store, inventory, sqlstores.NewSQLTransactor(db), nil)
	org := insertMaintenanceTestOrg(t, db, "reopen")
	site := insertMaintenanceTestSite(t, db, org, "Repair site")
	var partID int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO inventory_part(org_id,site_id,name,type,on_hand,allocated,reorder_point) VALUES($1,$2,'Fan','cooling',10,2,0) RETURNING id`, org, site).Scan(&partID))
	ticket, err := store.CreateRepairTicket(ctx, models.CreateParams{OrgID: org, SiteID: &site, Category: models.TicketCategoryInfrastructure, Component: "Power", Description: "Opening symptom", IdempotencyKey: "reopen", CreateRequestHash: maintenanceCreateRequestHash}, "TK-0001")
	require.NoError(t, err)
	require.NoError(t, store.InsertTicketPart(ctx, org, ticket.ID, partID, "Fan", 2))
	status, resolution := models.TicketStatusCompleted, models.TicketResolutionRepaired
	note := "Replaced fan and verified stable operation"
	closed, err := service.UpdateRepairTicket(ctx, models.UpdateParams{OrgID: org, ID: ticket.ID, ExpectedUpdatedAt: ticket.UpdatedAt, Status: &status, Resolution: &resolution, ResolutionNote: &note})
	require.NoError(t, err)
	require.NotNil(t, closed.CompletedAt)
	status = models.TicketStatusOpen
	reopened, err := service.UpdateRepairTicket(ctx, models.UpdateParams{OrgID: org, ID: ticket.ID, ExpectedUpdatedAt: closed.UpdatedAt, Status: &status})
	require.NoError(t, err)
	require.Nil(t, reopened.CompletedAt)
	require.Equal(t, models.TicketResolutionUnspecified, reopened.Resolution)
	require.Equal(t, "Opening symptom", reopened.Description)
	parts, err := store.ListTicketParts(ctx, org, ticket.ID)
	require.NoError(t, err)
	require.Len(t, parts, 1)
	require.NotNil(t, parts[0].ConsumedAt)
	originalConsumption := *parts[0].ConsumedAt
	stocked, err := inventory.Get(ctx, org, partID)
	require.NoError(t, err)
	require.EqualValues(t, 8, stocked.OnHand)
	require.Zero(t, stocked.Allocated)
	selection := []models.PartUsage{{InventoryPartID: partID, Quantity: 1}}
	reserved, err := service.UpdateRepairTicket(ctx, models.UpdateParams{OrgID: org, ID: ticket.ID, ExpectedUpdatedAt: reopened.UpdatedAt, PartsSelection: &selection})
	require.NoError(t, err, "reopened repairs may reserve the same SKU while retaining prior consumption")
	parts, err = store.ListTicketParts(ctx, org, ticket.ID)
	require.NoError(t, err)
	require.Len(t, parts, 2)
	require.NotNil(t, parts[0].ConsumedAt)
	require.Nil(t, parts[1].ConsumedAt)
	status = models.TicketStatusCompleted
	complete := models.UpdateParams{OrgID: org, ID: ticket.ID, ExpectedUpdatedAt: reserved.UpdatedAt, Status: &status, Resolution: &resolution, ResolutionNote: &note}
	_, err = service.UpdateRepairTicket(ctx, complete)
	require.NoError(t, err)
	parts, err = store.ListTicketParts(ctx, org, ticket.ID)
	require.NoError(t, err)
	require.Len(t, parts, 2)
	require.Equal(t, originalConsumption, *parts[0].ConsumedAt)
	require.EqualValues(t, 2, parts[0].Quantity)
	require.NotNil(t, parts[1].ConsumedAt)
	require.EqualValues(t, 1, parts[1].Quantity)
	stocked, err = inventory.Get(ctx, org, partID)
	require.NoError(t, err)
	require.EqualValues(t, 7, stocked.OnHand)
	require.Zero(t, stocked.Allocated)
	_, err = service.UpdateRepairTicket(ctx, complete)
	require.Error(t, err, "retrying a completed transition cannot consume parts again")
	stocked, err = inventory.Get(ctx, org, partID)
	require.NoError(t, err)
	require.EqualValues(t, 7, stocked.OnHand)
	require.Zero(t, stocked.Allocated)
}

func TestMaintenanceReopenSerializesWithLocationDeletion(t *testing.T) {
	for _, location := range []string{"site", "building"} {
		for _, winner := range []string{"delete", "reopen"} {
			t.Run(fmt.Sprintf("%s-%s-first", location, winner), func(t *testing.T) {
				db := testutil.GetTestDB(t)
				ctx := t.Context()
				org := insertMaintenanceTestOrg(t, db, "reopen-delete")
				site := insertMaintenanceTestSite(t, db, org, "Repair site")
				building := insertMaintenanceTestBuilding(t, db, org, site, "Repair building")
				store := sqlstores.NewSQLMaintenanceStore(db)
				tx := sqlstores.NewSQLTransactor(db)
				service := maintenance.NewService(store, store, sqlstores.NewSQLInventoryStore(db), tx, nil)
				siteStore := sqlstores.NewSQLSiteStore(db)
				buildingStore := sqlstores.NewSQLBuildingStore(db)
				siteService := sites.NewService(siteStore, buildingStore, nil, nil, nil, tx, nil)
				buildingService := buildings.NewService(buildingStore, siteStore, nil, nil, nil, tx, nil)
				ticket, err := store.CreateRepairTicket(ctx, models.CreateParams{OrgID: org, SiteID: &site, BuildingID: &building, Category: models.TicketCategoryInfrastructure, Component: "Power", IdempotencyKey: "reopen-delete", CreateRequestHash: maintenanceCreateRequestHash}, "TK-0001")
				require.NoError(t, err)
				status, resolution, note := models.TicketStatusCompleted, models.TicketResolutionNoActionNeeded, "No further work needed"
				closed, err := service.UpdateRepairTicket(ctx, models.UpdateParams{OrgID: org, ID: ticket.ID, ExpectedUpdatedAt: ticket.UpdatedAt, Status: &status, Resolution: &resolution, ResolutionNote: &note})
				require.NoError(t, err)
				deleteLocation := func(ctx context.Context) error {
					if location == "site" {
						_, err := siteService.DeleteSite(ctx, org, site)
						return err
					}
					_, err := buildingService.DeleteBuilding(ctx, org, building)
					return err
				}
				reopen := func(ctx context.Context) error {
					open := models.TicketStatusOpen
					_, err := service.UpdateRepairTicket(ctx, models.UpdateParams{OrgID: org, ID: ticket.ID, ExpectedUpdatedAt: closed.UpdatedAt, Status: &open})
					return err
				}
				first, second := deleteLocation, reopen
				if winner == "reopen" {
					first, second = reopen, deleteLocation
				}
				// Hold the first domain operation's transaction open while the other
				// reaches its conflicting row lock, then let it observe the commit.
				pending, err := db.BeginTx(ctx, nil)
				require.NoError(t, err)
				defer func() { _ = pending.Rollback() }()
				var blockingPID int
				require.NoError(t, pending.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&blockingPID))
				pendingCtx := infradb.WithTxQueries(ctx, sqlc.New(pending))
				require.NoError(t, first(pendingCtx))
				result := make(chan error, 1)
				go func() { result <- second(ctx) }()
				waitForMaintenancePlacementLock(t, db, blockingPID)
				require.NoError(t, pending.Commit())
				select {
				case err := <-result:
					if winner == "delete" {
						require.True(t, fleeterror.IsNotFoundError(err), "reopening must reject the retired location after waiting: %v", err)
					} else {
						require.True(t, fleeterror.IsFailedPreconditionError(err), "deletion must see the reopened ticket after waiting: %v", err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("location deletion and reopening did not serialize")
				}
				current, err := store.GetRepairTicket(ctx, org, ticket.ID)
				require.NoError(t, err)
				if winner == "delete" {
					require.Equal(t, models.TicketStatusCompleted, current.Status)
				} else {
					require.Equal(t, models.TicketStatusOpen, current.Status)
				}
			})
		}
	}
}

func TestMaintenanceReopenAllowsUnassignedMiner(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	org := insertMaintenanceTestOrg(t, db, "unassigned-reopen")
	miner := insertMaintenanceTestDevice(t, db, org, "unassigned-miner")
	store := sqlstores.NewSQLMaintenanceStore(db)
	service := maintenance.NewService(store, store, sqlstores.NewSQLInventoryStore(db), sqlstores.NewSQLTransactor(db), nil)
	ticket, created, err := service.CreateRepairTicket(ctx, models.CreateParams{OrgID: org, Category: models.TicketCategoryMiner, Component: "Fan", MinerIdentifier: &miner, IdempotencyKey: "unassigned-reopen"})
	require.NoError(t, err)
	require.True(t, created)
	require.Nil(t, ticket.SiteID)
	require.Nil(t, ticket.BuildingID)
	status, resolution, note := models.TicketStatusCompleted, models.TicketResolutionNoActionNeeded, "No further work needed"
	closed, err := service.UpdateRepairTicket(ctx, models.UpdateParams{OrgID: org, ID: ticket.ID, ExpectedUpdatedAt: ticket.UpdatedAt, Status: &status, Resolution: &resolution, ResolutionNote: &note})
	require.NoError(t, err)
	status = models.TicketStatusOpen
	reopened, err := service.UpdateRepairTicket(ctx, models.UpdateParams{OrgID: org, ID: ticket.ID, ExpectedUpdatedAt: closed.UpdatedAt, Status: &status})
	require.NoError(t, err)
	require.Equal(t, models.TicketStatusOpen, reopened.Status)
	require.Nil(t, reopened.SiteID)
	require.Nil(t, reopened.BuildingID)
}
