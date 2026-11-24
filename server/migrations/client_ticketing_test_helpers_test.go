package migrations_test

import (
	"database/sql"
	"testing"

	"github.com/block/proto-fleet/server/migrations"
	"github.com/stretchr/testify/require"
)

func rollbackEmptyClientTicketing(t *testing.T, conn *sql.DB) {
	t.Helper()
	// Repair obligations reference tickets and hosted accounts, so they go first.
	// A test that rolls back twice has already removed them.
	var obligations bool
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT to_regclass('hashboard_repair_obligation') IS NOT NULL`).Scan(&obligations))
	if obligations {
		runClientTicketingDown(t, conn, "001114_internal_hashboard_repair_obligations.down.sql")
	}
	rollbackEmptyHashboardAllocations(t, conn)
	runClientTicketingDown(t, conn, "001048_internal_client_ticketing.down.sql")
}

func runClientTicketingDown(t *testing.T, conn *sql.DB, name string) {
	t.Helper()
	source, err := migrations.Current.ReadFile("current/" + name)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), string(source))
	require.NoError(t, err, name)
}
