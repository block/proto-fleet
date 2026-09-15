package sqlstores_test

import (
	"context"
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

func TestMaintenanceBulkServiceValidatesEntireSelection(t *testing.T) {
	db := testutil.GetTestDB(t)
	store := sqlstores.NewSQLMaintenanceStore(db)
	service := maintenance.NewService(store, store, sqlstores.NewSQLInventoryStore(db), sqlstores.NewSQLTransactor(db), nil)
	orgID := insertMaintenanceTestOrg(t, db, "bulk-service")
	otherOrgID := insertMaintenanceTestOrg(t, db, "bulk-service-other")
	siteID := insertMaintenanceTestSite(t, db, orgID, "Bulk allowed")
	deniedSiteID := insertMaintenanceTestSite(t, db, orgID, "Bulk denied")
	assigneeID := insertMaintenanceTestUser(t, db, orgID, "bulk-assignee")
	ctx := authz.WithAuthorizedSiteScope(t.Context(), authz.AuthorizedSiteScope{OrgWide: true, SiteIDs: []int64{deniedSiteID}})
	versions := make(map[int64]time.Time)
	create := func(org, site int64) *models.RepairTicket {
		t.Helper()
		n, err := store.NextTicketNumber(ctx, org)
		require.NoError(t, err)
		ticket, err := store.CreateRepairTicket(ctx, models.CreateParams{
			OrgID: org, Category: models.TicketCategoryInfrastructure, Component: "Transformer", SiteID: &site,
			IdempotencyKey: fmt.Sprintf("bulk-service-%d", n), CreateRequestHash: maintenanceCreateRequestHash,
		}, fmt.Sprintf("TK-%04d", n))
		require.NoError(t, err)
		versions[ticket.ID] = ticket.UpdatedAt
		return ticket
	}
	otherSiteID := insertMaintenanceTestSite(t, db, otherOrgID, "Other org")
	foreign := create(otherOrgID, otherSiteID)
	denied := create(orgID, deniedSiteID)
	expectedVersions := func(ids []int64) map[int64]time.Time {
		result := make(map[int64]time.Time, len(ids))
		for _, id := range ids {
			result[id] = versions[id]
			if result[id].IsZero() {
				result[id] = time.Unix(1700000000, 0)
			}
		}
		return result
	}
	for _, operation := range []struct {
		name  string
		run   func(context.Context, []int64) (int64, error)
		check func(*testing.T, *models.RepairTicket)
	}{
		{"status", func(ctx context.Context, ids []int64) (int64, error) {
			return service.BulkUpdateStatus(ctx, orgID, ids, models.TicketStatusInProgress, expectedVersions(ids))
		}, func(t *testing.T, ticket *models.RepairTicket) {
			require.Equal(t, models.TicketStatusInProgress, ticket.Status)
		}},
		{"assign", func(ctx context.Context, ids []int64) (int64, error) {
			return service.BulkAssign(ctx, orgID, ids, &assigneeID, expectedVersions(ids))
		}, func(t *testing.T, ticket *models.RepairTicket) { require.Equal(t, &assigneeID, ticket.AssigneeUserID) }},
		{"urgent", func(ctx context.Context, ids []int64) (int64, error) {
			return service.BulkMarkUrgent(ctx, orgID, ids, expectedVersions(ids))
		}, func(t *testing.T, ticket *models.RepairTicket) { require.True(t, ticket.Urgent) }},
		{"close", func(ctx context.Context, ids []int64) (int64, error) {
			return service.BulkClose(ctx, models.BulkCloseParams{OrgID: orgID, TicketIDs: ids,
				ExpectedVersions: expectedVersions(ids), Resolution: models.TicketResolutionRepaired})
		}, func(t *testing.T, ticket *models.RepairTicket) {
			require.Equal(t, models.TicketStatusCompleted, ticket.Status)
		}},
	} {
		t.Run(operation.name, func(t *testing.T) {
			one, two := create(orgID, siteID), create(orgID, siteID)
			stale := create(orgID, siteID)
			urgent := true
			_, err := store.UpdateRepairTicket(ctx, models.UpdateParams{OrgID: orgID, ID: stale.ID, Urgent: &urgent})
			require.NoError(t, err)
			for _, rejected := range []struct {
				name    string
				ids     []int64
				isError func(error) bool
			}{
				{"foreign organization", []int64{one.ID, foreign.ID}, fleeterror.IsNotFoundError},
				{"denied site", []int64{one.ID, denied.ID}, fleeterror.IsForbiddenError},
				{"missing ticket", []int64{one.ID, 9223372036854775807}, fleeterror.IsNotFoundError},
				{"invalid ID", []int64{one.ID, 0}, fleeterror.IsInvalidArgumentError},
				{"empty selection", nil, fleeterror.IsInvalidArgumentError},
				{"mixed fresh and stale versions", []int64{one.ID, stale.ID}, fleeterror.IsFailedPreconditionError},
			} {
				t.Run(rejected.name, func(t *testing.T) {
					count, err := operation.run(ctx, rejected.ids)
					require.True(t, rejected.isError(err), "unexpected error: %v", err)
					require.Zero(t, count)
					unchanged, err := store.GetRepairTicket(ctx, orgID, one.ID)
					require.NoError(t, err)
					require.Equal(t, one, unchanged, "invalid selections must not partially mutate authorized tickets")
				})
			}
			count, err := operation.run(ctx, []int64{two.ID, one.ID, one.ID})
			require.NoError(t, err)
			require.Equal(t, int64(2), count, "the service normalizes duplicate IDs")
			for _, id := range []int64{one.ID, two.ID} {
				updated, err := store.GetRepairTicket(ctx, orgID, id)
				require.NoError(t, err)
				operation.check(t, updated)
				require.True(t, updated.UpdatedAt.After(versions[id]))
			}
			count, err = operation.run(ctx, []int64{one.ID, two.ID})
			require.True(t, fleeterror.IsFailedPreconditionError(err), "stale bulk replay must fail: %v", err)
			require.Zero(t, count)
		})
	}
}

func TestMaintenanceBulkCloseRollsBackInventoryOnWriteFailure(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLMaintenanceStore(db)
	service := maintenance.NewService(store, store, sqlstores.NewSQLInventoryStore(db), sqlstores.NewSQLTransactor(db), nil)
	orgID := insertMaintenanceTestOrg(t, db, "bulk-rollback")
	var ticketID, partID int64
	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO repair_ticket (org_id, ticket_number, category, component)
		VALUES ($1, 'TK-0001', 2, 'Transformer') RETURNING id`, orgID).Scan(&ticketID))
	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO inventory_part (org_id, name, type, on_hand, allocated, reorder_point)
		VALUES ($1, 'Fan', 'cooling', 5, 1, 0) RETURNING id`, orgID).Scan(&partID))
	require.NoError(t, store.InsertTicketPart(ctx, orgID, ticketID, partID, "Fan", 1))
	// PostgreSQL rejects NUL text in the final ticket UPDATE, after inventory
	// consumption. Exercise rollback through the real service and transaction.
	invalidNotes := "\x00"
	initial, err := store.GetRepairTicket(ctx, orgID, ticketID)
	require.NoError(t, err)
	count, err := service.BulkClose(ctx, models.BulkCloseParams{
		OrgID: orgID, TicketIDs: []int64{ticketID}, ExpectedVersions: map[int64]time.Time{ticketID: initial.UpdatedAt},
		Resolution: models.TicketResolutionRepaired, Notes: &invalidNotes,
	})
	require.Error(t, err)
	require.Zero(t, count)
	ticket, err := store.GetRepairTicket(ctx, orgID, ticketID)
	require.NoError(t, err)
	require.Equal(t, models.TicketStatusOpen, ticket.Status)
	parts, err := store.ListTicketParts(ctx, orgID, ticketID)
	require.NoError(t, err)
	require.Len(t, parts, 1)
	require.Nil(t, parts[0].ConsumedAt)
	var onHand, allocated int64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT on_hand, allocated FROM inventory_part WHERE id = $1`, partID).Scan(&onHand, &allocated))
	require.Equal(t, int64(5), onHand)
	require.Equal(t, int64(1), allocated)
}
