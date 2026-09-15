package migrations_test

import (
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/stretchr/testify/require"
)

func TestMaintenanceMigrationsRoundTrip(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	orgID := insertMaintenanceTestOrg(t, db, "maintenance-migration")
	siteID := insertMaintenanceTestSite(t, db, orgID, "Preserved site")
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	apply := func(name string) {
		t.Helper()
		script, err := migrations.Migrations.ReadFile(name)
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx, string(script))
		require.NoError(t, err)
	}

	apply("000147_maintenance_setup.down.sql")
	apply("000146_maintenance_schema.down.sql")
	for _, table := range []string{"repair_ticket_counter", "repair_ticket", "repair_ticket_comment", "inventory_part", "repair_ticket_part"} {
		var missing bool
		require.NoError(t, tx.QueryRowContext(ctx, `SELECT to_regclass($1) IS NULL`, table).Scan(&missing))
		require.True(t, missing, "%s must be removed by down", table)
	}
	var siteName string
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT name FROM site WHERE id = $1`, siteID).Scan(&siteName))
	require.Equal(t, "Preserved site", siteName, "maintenance rollback must not remove upstream data")

	apply("000146_maintenance_schema.up.sql")
	apply("000147_maintenance_setup.up.sql")
	var ticketID int64
	require.NoError(t, tx.QueryRowContext(ctx, `INSERT INTO repair_ticket (org_id, ticket_number, category, component, idempotency_key)
		VALUES ($1, 'TK-0001', 2, 'Transformer', 'round-trip-ticket') RETURNING id`, orgID).Scan(&ticketID))
	var first, second time.Time
	const update = `UPDATE repair_ticket SET urgent = TRUE WHERE id = $1 RETURNING updated_at`
	require.NoError(t, tx.QueryRowContext(ctx, update, ticketID).Scan(&first))
	require.NoError(t, tx.QueryRowContext(ctx, update, ticketID).Scan(&second))
	require.True(t, second.After(first), "schema recreation must restore strictly advancing versions")
}
