package sqlstores_test

import (
	"context"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/maintenance"
	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestMaintenanceTicketVersionGuardsEveryMutation(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLMaintenanceStore(db)
	tx := sqlstores.NewSQLTransactor(db)
	service := maintenance.NewService(store, store, sqlstores.NewSQLInventoryStore(db), tx, nil)
	orgID := insertMaintenanceTestOrg(t, db, "ticket-version")
	original, err := store.CreateRepairTicket(ctx, models.CreateParams{
		OrgID: orgID, Category: models.TicketCategoryInfrastructure, Component: "Transformer",
		IdempotencyKey: "version-ticket", CreateRequestHash: maintenanceCreateRequestHash,
	}, "TK-0001")
	require.NoError(t, err)
	urgent := true
	current, err := service.UpdateRepairTicket(ctx, models.UpdateParams{OrgID: orgID, ID: original.ID, ExpectedUpdatedAt: original.UpdatedAt, Urgent: &urgent})
	require.NoError(t, err)
	require.True(t, current.UpdatedAt.After(original.UpdatedAt))
	status, vendor := models.TicketStatusInProgress, "Vendor"
	completed, resolution := models.TicketStatusCompleted, models.TicketResolutionDeferred
	emptyParts := []models.PartUsage{}
	for _, mutation := range []struct {
		name   string
		params models.UpdateParams
	}{
		{"urgent", models.UpdateParams{Urgent: &urgent}},
		{"assignment", models.UpdateParams{ClearAssignee: true}},
		{"status", models.UpdateParams{Status: &status}},
		{"RMA", models.UpdateParams{RMAVendor: &vendor}},
		{"parts", models.UpdateParams{PartsSelection: &emptyParts}},
		{"completion", models.UpdateParams{Status: &completed, Resolution: &resolution}},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			params := mutation.params
			params.OrgID, params.ID, params.ExpectedUpdatedAt = orgID, original.ID, original.UpdatedAt
			_, err := service.UpdateRepairTicket(ctx, params)
			require.True(t, fleeterror.IsFailedPreconditionError(err), "%v", err)
			unchanged, err := store.GetRepairTicket(ctx, orgID, original.ID)
			require.NoError(t, err)
			require.Equal(t, current, unchanged)
		})
	}
	next, err := service.UpdateRepairTicket(ctx, models.UpdateParams{OrgID: orgID, ID: original.ID, ExpectedUpdatedAt: current.UpdatedAt, Urgent: &urgent})
	require.NoError(t, err)
	require.True(t, next.UpdatedAt.After(current.UpdatedAt), "same-value writes also advance the token")
	current = next
	// Even same-value updates within a transaction advance the token.
	require.NoError(t, tx.RunInTx(ctx, func(txCtx context.Context) error {
		first, err := store.UpdateRepairTicket(txCtx, models.UpdateParams{OrgID: orgID, ID: original.ID, Urgent: &urgent})
		if err != nil {
			return err
		}
		second, err := store.UpdateRepairTicket(txCtx, models.UpdateParams{OrgID: orgID, ID: original.ID, Urgent: &urgent})
		if err != nil {
			return err
		}
		require.True(t, first.UpdatedAt.After(current.UpdatedAt))
		require.True(t, second.UpdatedAt.After(first.UpdatedAt))
		current = second
		return nil
	}))
}

func TestMaintenanceCompletionStaleReplayCannotConsumeStockAgain(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	store := sqlstores.NewSQLMaintenanceStore(db)
	service := maintenance.NewService(store, store, sqlstores.NewSQLInventoryStore(db), sqlstores.NewSQLTransactor(db), nil)
	orgID := insertMaintenanceTestOrg(t, db, "completion-version")
	var ticketID, partID int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO repair_ticket (org_id, ticket_number, category, component)
  VALUES ($1, 'TK-0001', 2, 'Transformer') RETURNING id`, orgID).Scan(&ticketID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO inventory_part (org_id, name, type, on_hand, allocated, reorder_point)
  VALUES ($1, 'Fan', 'cooling', 5, 1, 0) RETURNING id`, orgID).Scan(&partID))
	require.NoError(t, store.InsertTicketPart(ctx, orgID, ticketID, partID, "Fan", 1))
	initial, err := store.GetRepairTicket(ctx, orgID, ticketID)
	require.NoError(t, err)
	completed, resolution := models.TicketStatusCompleted, models.TicketResolutionRepaired
	params := models.UpdateParams{OrgID: orgID, ID: ticketID, ExpectedUpdatedAt: initial.UpdatedAt, Status: &completed, Resolution: &resolution}
	_, err = service.UpdateRepairTicket(ctx, params)
	require.NoError(t, err)
	_, err = service.UpdateRepairTicket(ctx, params)
	require.True(t, fleeterror.IsFailedPreconditionError(err), "%v", err)
	var onHand, allocated int64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT on_hand, allocated FROM inventory_part WHERE id = $1`, partID).Scan(&onHand, &allocated))
	require.Equal(t, int64(4), onHand)
	require.Zero(t, allocated)
}
