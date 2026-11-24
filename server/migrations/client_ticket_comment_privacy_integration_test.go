package migrations_test

import (
	"regexp"
	"testing"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/stretchr/testify/require"
)

func TestClientTicketMigrationKeepsLegacyCommentsInternal(t *testing.T) {
	conn := testutil.GetTestDB(t)
	tx, err := conn.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer tx.Rollback()
	// Shadow only this table in the transaction to exercise the shipped ALTER
	// statements on pre-ticketing rows without changing the shared schema.
	_, err = tx.ExecContext(t.Context(), `CREATE TEMP TABLE repair_ticket_comment (id BIGINT PRIMARY KEY, text TEXT NOT NULL) ON COMMIT DROP; INSERT INTO repair_ticket_comment VALUES (1,'Legacy private host comment')`)
	require.NoError(t, err)
	applyColumnStatement := func(file, pattern string) {
		t.Helper()
		source, err := migrations.Current.ReadFile("current/" + file)
		require.NoError(t, err)
		matches := regexp.MustCompile(pattern).FindAllString(string(source), -1)
		require.Len(t, matches, 1)
		_, err = tx.ExecContext(t.Context(), matches[0])
		require.NoError(t, err)
	}
	applyColumnStatement("001048_internal_client_ticketing.up.sql", `(?m)^ALTER TABLE repair_ticket_comment ADD COLUMN internal [^;]+;`)
	var internal bool
	require.NoError(t, tx.QueryRowContext(t.Context(), `SELECT internal FROM repair_ticket_comment WHERE id=1`).Scan(&internal))
	require.True(t, internal, "existing host comments must retain private visibility")
	_, err = tx.ExecContext(t.Context(), `INSERT INTO repair_ticket_comment (id,text,internal) VALUES (2,'Explicit client-visible reply',false)`)
	require.NoError(t, err)
	require.NoError(t, tx.QueryRowContext(t.Context(), `INSERT INTO repair_ticket_comment (id,text) VALUES (3,'New default-private host comment') RETURNING internal`).Scan(&internal))
	require.True(t, internal, "new host comments must default to private")
	require.NoError(t, tx.QueryRowContext(t.Context(), `SELECT internal FROM repair_ticket_comment WHERE id=2`).Scan(&internal))
	require.False(t, internal, "the default must preserve explicit client sharing")
}
