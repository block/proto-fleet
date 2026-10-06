package sqlstores_test

import (
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/buildings"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/maintenance"
	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
	"github.com/block/proto-fleet/server/internal/domain/sites"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestMaintenanceLocationDeletionPreservesCompletedHistory(t *testing.T) {
	for _, location := range []string{"building", "site", "site with moved building"} {
		t.Run(location, func(t *testing.T) {
			db := testutil.GetTestDB(t)
			ctx := t.Context()
			orgID := insertMaintenanceTestOrg(t, db, "location-history")
			otherOrgID := insertMaintenanceTestOrg(t, db, "other-history")
			siteID := insertMaintenanceTestSite(t, db, orgID, "Original site")
			otherSiteID := insertMaintenanceTestSite(t, db, orgID, "Other site")
			buildingID := insertMaintenanceTestBuilding(t, db, orgID, siteID, "Original building")
			userID := insertMaintenanceTestUser(t, db, orgID, "history-author")
			store := sqlstores.NewSQLMaintenanceStore(db)
			siteStore := sqlstores.NewSQLSiteStore(db)
			buildingStore := sqlstores.NewSQLBuildingStore(db)
			tx := sqlstores.NewSQLTransactor(db)
			service := maintenance.NewService(store, store, sqlstores.NewSQLInventoryStore(db), tx, nil)
			siteService := sites.NewService(siteStore, buildingStore, nil, nil, nil, tx, nil)
			buildingService := buildings.NewService(buildingStore, siteStore, nil, nil, nil, tx, nil)
			ticket, err := store.CreateRepairTicket(ctx, models.CreateParams{OrgID: orgID,
				Category: models.TicketCategoryInfrastructure, Component: "Electrical",
				SiteID: &siteID, BuildingID: &buildingID,
				IdempotencyKey: "location-history", CreateRequestHash: maintenanceCreateRequestHash}, "TK-0001")
			require.NoError(t, err)
			_, err = service.CreateComment(ctx, orgID, ticket.ID, userID, "history-author", "Keep the location and discussion", "history-comment")
			require.NoError(t, err)
			deleteSiteID := siteID
			if location == "site with moved building" {
				// A building's current parent can differ from a ticket's original site.
				_, err = db.ExecContext(ctx, "UPDATE building SET site_id = $1 WHERE id = $2", otherSiteID, buildingID)
				require.NoError(t, err)
				deleteSiteID = otherSiteID
			}
			deleteLocation := func() error {
				if location == "building" {
					_, err := buildingService.DeleteBuilding(ctx, orgID, buildingID)
					return err
				}
				_, err := siteService.DeleteSite(ctx, orgID, deleteSiteID)
				return err
			}
			for _, status := range []models.TicketStatus{models.TicketStatusOpen, models.TicketStatusInProgress, models.TicketStatusOnHold, models.TicketStatusSentToVendor} {
				ticket, err = store.UpdateRepairTicket(ctx, models.UpdateParams{OrgID: orgID, ID: ticket.ID, Status: &status})
				require.NoError(t, err)
				require.True(t, fleeterror.IsFailedPreconditionError(deleteLocation()), "unfinished status %d must block deletion", status)
				var live bool
				require.NoError(t, db.QueryRowContext(ctx, "SELECT deleted_at IS NULL FROM building WHERE id = $1", buildingID).Scan(&live))
				require.True(t, live, "failed deletion must roll back its location changes")
			}
			status, resolution := models.TicketStatusCompleted, models.TicketResolutionNoActionNeeded
			_, err = service.UpdateRepairTicket(ctx, models.UpdateParams{OrgID: orgID, ID: ticket.ID,
				ExpectedUpdatedAt: ticket.UpdatedAt, Status: &status, Resolution: &resolution})
			require.NoError(t, err)
			if location != "building" {
				var partID int64
				require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO inventory_part
					(org_id, site_id, name, type, on_hand, allocated, reorder_point)
					VALUES ($1, $2, 'Remaining inventory', 'electrical', 0, 0, 0) RETURNING id`, orgID, deleteSiteID).Scan(&partID))
				require.True(t, fleeterror.IsFailedPreconditionError(deleteLocation()), "inventory must still block site deletion")
				_, err = sqlstores.NewSQLInventoryStore(db).SoftDelete(ctx, orgID, partID)
				require.NoError(t, err)
			}
			require.NoError(t, deleteLocation(), "completed tickets must not prevent retiring a location")
			var deleted bool
			require.NoError(t, db.QueryRowContext(ctx, "SELECT deleted_at IS NOT NULL FROM building WHERE id = $1", buildingID).Scan(&deleted))
			require.True(t, deleted)

			allowed := authz.WithAuthorizedSiteScope(ctx, authz.AuthorizedSiteScope{SiteIDs: []int64{siteID}})
			detail, err := service.GetRepairTicket(allowed, orgID, ticket.ID)
			require.NoError(t, err)
			require.Equal(t, &siteID, detail.Ticket.SiteID)
			require.Equal(t, &buildingID, detail.Ticket.BuildingID)
			require.Equal(t, "Original site", detail.Ticket.SiteName)
			require.Equal(t, "Original building", detail.Ticket.BuildingName)
			require.Len(t, detail.Comments, 1)
			require.Equal(t, "Keep the location and discussion", detail.Comments[0].Text)
			history, total, _, _, err := service.ListCompletedTickets(allowed, models.CompletedFilter{OrgID: orgID, SiteIDs: []int64{siteID}})
			require.NoError(t, err)
			require.EqualValues(t, 1, total)
			require.Len(t, history, 1)
			require.Equal(t, "Original site", history[0].SiteName)
			require.Equal(t, "Original building", history[0].BuildingName)
			listed, total, _, err := service.ListRepairTickets(allowed, models.ListFilter{OrgID: orgID, SiteIDs: []int64{siteID}, SearchQuery: "Original building"})
			require.NoError(t, err)
			require.EqualValues(t, 1, total, "search counts must include historical location names")
			require.Len(t, listed, 1)
			require.Equal(t, "Original building", listed[0].BuildingName)

			for _, scope := range []authz.AuthorizedSiteScope{{SiteIDs: []int64{otherSiteID}}, {OrgWide: true, SiteIDs: []int64{siteID}}} {
				denied := authz.WithAuthorizedSiteScope(ctx, scope)
				_, err = service.GetRepairTicket(denied, orgID, ticket.ID)
				require.True(t, fleeterror.IsForbiddenError(err), "original site authority must survive deletion: %v", err)
			}
			_, err = service.GetRepairTicket(ctx, otherOrgID, ticket.ID)
			require.True(t, fleeterror.IsNotFoundError(err))
			hidden, total, _, _, err := service.ListCompletedTickets(ctx, models.CompletedFilter{OrgID: orgID, SiteIDs: []int64{otherSiteID}})
			require.NoError(t, err)
			require.Empty(t, hidden)
			require.Zero(t, total)
		})
	}
}
