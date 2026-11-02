package sqlstores_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/maintenance"
	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestHostClosurePreservesOperationalNotes(t *testing.T) {
	for _, bulk := range []bool{false, true} {
		t.Run(fmt.Sprintf("bulk=%t", bulk), func(t *testing.T) {
			conn := testutil.GetTestDB(t)
			store := sqlstores.NewSQLMaintenanceStore(conn)
			service := maintenance.NewService(store, store, sqlstores.NewSQLInventoryStore(conn), sqlstores.NewSQLTransactor(conn), nil)
			org := insertMaintenanceTestOrg(t, conn, "resolution-notes")
			site := insertMaintenanceTestSite(t, conn, org, "Repair site")
			operational := "Host-only operational notes"
			ticket, _, err := service.CreateRepairTicket(t.Context(), models.CreateParams{OrgID: org, Category: models.TicketCategoryInfrastructure, SiteID: &site, Component: "Power", Notes: &operational, IdempotencyKey: "preserve-notes"})
			require.NoError(t, err)
			note := "  Power restored; client can resume operation  "
			if bulk {
				affected, err := service.BulkClose(t.Context(), models.BulkCloseParams{OrgID: org, TicketIDs: []int64{ticket.ID}, ExpectedVersions: map[int64]time.Time{ticket.ID: ticket.UpdatedAt}, Resolution: models.TicketResolutionNoActionNeeded, ResolutionNote: &note})
				require.NoError(t, err)
				require.EqualValues(t, 1, affected)
			} else {
				status, resolution := models.TicketStatusCompleted, models.TicketResolutionNoActionNeeded
				_, err = service.UpdateRepairTicket(t.Context(), models.UpdateParams{OrgID: org, ID: ticket.ID, ExpectedUpdatedAt: ticket.UpdatedAt, Status: &status, Resolution: &resolution, ResolutionNote: &note})
				require.NoError(t, err)
			}
			closed, err := service.GetRepairTicket(t.Context(), org, ticket.ID)
			require.NoError(t, err)
			require.Equal(t, &operational, closed.Ticket.Notes)
			require.Equal(t, "Power restored; client can resume operation", closed.Ticket.ResolutionNote)
			history, total, _, _, err := service.ListCompletedTickets(t.Context(), models.CompletedFilter{OrgID: org})
			require.NoError(t, err)
			require.EqualValues(t, 1, total)
			require.Len(t, history, 1)
			require.Equal(t, &operational, history[0].Notes)
			require.Equal(t, closed.Ticket.ResolutionNote, history[0].ResolutionNote)
			status := models.TicketStatusOpen
			reopened, err := service.UpdateRepairTicket(t.Context(), models.UpdateParams{OrgID: org, ID: ticket.ID, ExpectedUpdatedAt: closed.Ticket.UpdatedAt, Status: &status})
			require.NoError(t, err)
			require.Equal(t, &operational, reopened.Notes)
			require.Empty(t, reopened.ResolutionNote)
		})
	}
}
