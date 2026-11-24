package migrations_test

import (
	"testing"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/stretchr/testify/require"
)

func TestTicketingCategoriesMigrationNormalizesComponentsAndGuardsRollback(t *testing.T) {
	conn := testutil.GetTestDB(t)
	org := insertMaintenanceTestOrg(t, conn, "ticketing-categories")

	applyClientTicketMigration(t, conn, "001097_internal_ticketing_categories.down")
	_, err := conn.ExecContext(t.Context(), `INSERT INTO repair_ticket(org_id,ticket_number,category,component) VALUES($1,'TK-UNSPECIFIED',5,'Unspecified'),($1,'TK-NAMED',5,'Fan')`, org)
	require.NoError(t, err)

	applyClientTicketMigration(t, conn, "001097_internal_ticketing_categories.up")
	var unspecified, named string
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT component FROM repair_ticket WHERE org_id=$1 AND ticket_number='TK-UNSPECIFIED'`, org).Scan(&unspecified))
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT component FROM repair_ticket WHERE org_id=$1 AND ticket_number='TK-NAMED'`, org).Scan(&named))
	require.Equal(t, "Unknown", unspecified, "the Unknown component filter must find older client tickets")
	require.Equal(t, "Fan", named)

	_, err = conn.ExecContext(t.Context(), `INSERT INTO repair_ticket(org_id,ticket_number,category,component) VALUES($1,'TK-OTHER',6,'Other')`, org)
	require.NoError(t, err, "the up migration accepts Other tickets")

	down, err := migrations.Current.ReadFile("current/001097_internal_ticketing_categories.down.sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), string(down))
	require.ErrorContains(t, err, "Other tickets exist; preserve them and fix forward")
	var others int
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT count(*) FROM repair_ticket WHERE org_id=$1 AND category=6`, org).Scan(&others))
	require.Equal(t, 1, others, "a refused rollback must keep Other tickets")
	_, err = conn.ExecContext(t.Context(), `INSERT INTO repair_ticket(org_id,ticket_number,category,component) VALUES($1,'TK-OTHER-2',6,'Other')`, org)
	require.NoError(t, err, "a refused rollback must leave the widened constraint in place")
}
