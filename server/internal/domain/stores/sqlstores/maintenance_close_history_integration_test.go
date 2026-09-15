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

func TestNoActionClosureReleasesStockAndPreservesHistory(t *testing.T) {
	for _, bulk := range []bool{false, true} {
		t.Run(fmt.Sprintf("bulk=%t", bulk), func(t *testing.T) {
			db := testutil.GetTestDB(t)
			ctx := t.Context()
			store := sqlstores.NewSQLMaintenanceStore(db)
			inventory := sqlstores.NewSQLInventoryStore(db)
			service := maintenance.NewService(store, store, inventory, sqlstores.NewSQLTransactor(db), nil)
			orgID := insertMaintenanceTestOrg(t, db, "close-history")
			userID := insertMaintenanceTestUser(t, db, orgID, "close-author")
			var partID int64
			require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO inventory_part (org_id, name, type, on_hand, allocated, reorder_point)
    VALUES ($1, 'Fan', 'cooling', 20, 6, 0) RETURNING id`, orgID).Scan(&partID))
			var ids []int64
			versions := map[int64]time.Time{}
			for i, quantity := range []int32{2, 3} {
				ticket, err := store.CreateRepairTicket(ctx, models.CreateParams{OrgID: orgID,
					Category: models.TicketCategoryInfrastructure, Component: "Transformer",
					IdempotencyKey: fmt.Sprintf("close-%d", i), CreateRequestHash: maintenanceCreateRequestHash}, fmt.Sprintf("TK-%04d", i+1))
				require.NoError(t, err)
				require.NoError(t, store.InsertTicketPart(ctx, orgID, ticket.ID, partID, "Fan", quantity))
				ids = append(ids, ticket.ID)
				versions[ticket.ID] = ticket.UpdatedAt
				_, err = service.CreateComment(ctx, orgID, ticket.ID, userID, "close-author", "Retain this discussion", fmt.Sprintf("comment-%d", i))
				require.NoError(t, err)
			}
			closeTickets := func(notes string) error {
				if bulk {
					_, err := service.BulkClose(ctx, models.BulkCloseParams{OrgID: orgID, TicketIDs: ids, ExpectedVersions: versions,
						Resolution: models.TicketResolutionNoActionNeeded, Notes: &notes})
					return err
				}
				status, resolution := models.TicketStatusCompleted, models.TicketResolutionNoActionNeeded
				for _, id := range ids {
					_, err := service.UpdateRepairTicket(ctx, models.UpdateParams{OrgID: orgID, ID: id,
						ExpectedUpdatedAt: versions[id], Status: &status, Resolution: &resolution, Notes: &notes})
					if err != nil {
						return err
					}
				}
				return nil
			}
			// A final ticket-write failure must roll reservation releases back.
			require.Error(t, closeTickets("\x00"))
			part, err := inventory.Get(ctx, orgID, partID)
			require.NoError(t, err)
			require.EqualValues(t, 20, part.OnHand)
			require.EqualValues(t, 6, part.Allocated)
			require.NoError(t, closeTickets("Duplicate ticket; no work required"))
			part, err = inventory.Get(ctx, orgID, partID)
			require.NoError(t, err)
			require.EqualValues(t, 20, part.OnHand, "closing without work must not consume stock")
			require.EqualValues(t, 1, part.Allocated, "only these tickets' reservations are released")
			for _, id := range ids {
				detail, err := service.GetRepairTicket(ctx, orgID, id)
				require.NoError(t, err)
				require.Equal(t, models.TicketStatusCompleted, detail.Ticket.Status)
				require.Equal(t, models.TicketResolutionNoActionNeeded, detail.Ticket.Resolution)
				require.Nil(t, detail.Ticket.DeletedAt)
				require.Len(t, detail.Comments, 1)
				require.Equal(t, "Retain this discussion", detail.Comments[0].Text)
				require.Empty(t, detail.PartsUsed)
			}
			active, total, _, err := service.ListRepairTickets(ctx, models.ListFilter{OrgID: orgID, ExcludeCompleted: true})
			require.NoError(t, err)
			require.Empty(t, active)
			require.Zero(t, total)
			history, total, _, _, err := service.ListCompletedTickets(ctx, models.CompletedFilter{OrgID: orgID})
			require.NoError(t, err)
			require.Len(t, history, 2)
			require.EqualValues(t, 2, total)
			require.True(t, fleeterror.IsFailedPreconditionError(closeTickets("Duplicate ticket; no work required")))
		})
	}
}
