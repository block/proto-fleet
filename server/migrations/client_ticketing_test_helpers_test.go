package migrations_test

import (
	"database/sql"
	"testing"

	"github.com/block/proto-fleet/server/migrations"
	"github.com/stretchr/testify/require"
)

func rollbackEmptyClientTicketing(t *testing.T, conn *sql.DB) {
	t.Helper()
	rollbackEmptyHashboardAllocations(t, conn)
	for _, name := range []string{"001048_internal_client_ticketing.down.sql"} {
		source, err := migrations.Current.ReadFile("current/" + name)
		require.NoError(t, err)
		_, err = conn.ExecContext(t.Context(), string(source))
		require.NoError(t, err, name)
	}
}
