package sqlstores_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestMaintenanceStoresRetryTransactions(t *testing.T) {
	for _, table := range []string{"inventory_part", "repair_ticket"} {
		for _, code := range []string{"40001", "40P01"} {
			t.Run(table+"/"+code, func(t *testing.T) {
				db := testutil.GetTestDB(t)
				ctx := t.Context()
				orgID := insertMaintenanceTestOrg(t, db, "retry-wrapping")
				var ticketID, partID int64
				require.NoError(t, db.QueryRowContext(ctx, `
					INSERT INTO repair_ticket (org_id, ticket_number, category, component)
					VALUES ($1, 'TK-0001', 2, 'Transformer') RETURNING id`, orgID).Scan(&ticketID))
				require.NoError(t, db.QueryRowContext(ctx, `
					INSERT INTO inventory_part (org_id, name, type, on_hand, allocated, reorder_point)
					VALUES ($1, 'Fan', 'cooling', 5, 2, 0) RETURNING id`, orgID).Scan(&partID))

				// The sequence survives rollback, so only the first UPDATE fails.
				// All objects live in this test's disposable database.
				_, err := db.ExecContext(ctx, fmt.Sprintf(`
					CREATE SEQUENCE maintenance_retry_attempt;
					CREATE FUNCTION fail_first_maintenance_write() RETURNS trigger AS $$
					BEGIN
						IF nextval('maintenance_retry_attempt') = 1 THEN
							RAISE EXCEPTION 'injected retryable write failure' USING ERRCODE = '%s';
						END IF;
						RETURN NEW;
					END;
					$$ LANGUAGE plpgsql;
					CREATE TRIGGER maintenance_retry BEFORE UPDATE ON %s
					FOR EACH ROW EXECUTE FUNCTION fail_first_maintenance_write();`, code, table))
				require.NoError(t, err)

				inventory := sqlstores.NewSQLInventoryStore(db)
				maintenance := sqlstores.NewSQLMaintenanceStore(db)
				attempts := 0
				var firstErr error
				err = sqlstores.NewSQLTransactor(db).RunInTx(ctx, func(txCtx context.Context) error {
					attempts++
					writeErr := inventory.Release(txCtx, orgID, partID, 1)
					if writeErr == nil {
						urgent := true
						_, writeErr = maintenance.UpdateRepairTicket(txCtx, models.UpdateParams{OrgID: orgID, ID: ticketID, Urgent: &urgent})
					}
					if firstErr == nil {
						firstErr = writeErr
					}
					return writeErr
				})
				require.NoError(t, err, "the store must preserve the cause so the whole transaction can retry")
				require.Equal(t, 2, attempts)
				var pgErr *pgconn.PgError
				require.ErrorAs(t, firstErr, &pgErr)
				require.Equal(t, code, pgErr.Code)
				part, err := inventory.Get(ctx, orgID, partID)
				require.NoError(t, err)
				require.EqualValues(t, 1, part.Allocated, "retry must not release stock twice")
				ticket, err := maintenance.GetRepairTicket(ctx, orgID, ticketID)
				require.NoError(t, err)
				require.True(t, ticket.Urgent)
			})
		}
	}
}

func TestMaintenanceReadAndReferenceErrorsPreserveCause(t *testing.T) {
	db := testutil.GetTestDB(t)
	maintenance := sqlstores.NewSQLMaintenanceStore(db)
	inventory := sqlstores.NewSQLInventoryStore(db)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, operation := range []struct {
		name string
		run  func() error
	}{
		{"inventory read", func() error { _, err := inventory.Get(ctx, 1, 1); return err }},
		{"ticket read", func() error { _, err := maintenance.GetRepairTicket(ctx, 1, 1); return err }},
		{"site lock", func() error { return maintenance.LockSiteForTicket(ctx, 1, 1) }},
	} {
		t.Run(operation.name, func(t *testing.T) {
			require.ErrorIs(t, operation.run(), context.Canceled)
		})
	}
}
