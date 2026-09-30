package migrations_test

import (
	"testing"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/stretchr/testify/require"
)

func TestContainerInventoryMigrationRoundTrip(t *testing.T) {
	db := testutil.GetTestDB(t)
	for _, direction := range []string{"down", "up"} {
		script, err := migrations.Migrations.ReadFile("000153_container_inventory." + direction + ".sql")
		require.NoError(t, err)
		_, err = db.ExecContext(t.Context(), string(script))
		require.NoError(t, err)
	}
	var count int
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT count(*) FROM fleet_mining_asset").Scan(&count))
	require.Zero(t, count)
}
