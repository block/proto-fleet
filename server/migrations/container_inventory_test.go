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

func TestRemoveContainerSnapshotMigrationRoundTrip(t *testing.T) {
	db := testutil.GetTestDB(t)
	for _, direction := range []string{"down", "up"} {
		script, err := migrations.Migrations.ReadFile("000154_remove_container_snapshot." + direction + ".sql")
		require.NoError(t, err)
		_, err = db.ExecContext(t.Context(), string(script))
		require.NoError(t, err)
		var exists bool
		require.NoError(t, db.QueryRowContext(t.Context(), "SELECT to_regclass('container_snapshot') IS NOT NULL").Scan(&exists))
		require.Equal(t, direction == "down", exists)
		for _, table := range []string{"container_inventory", "container_module", "container_module_metrics", "container_plant_metrics"} {
			require.NoError(t, db.QueryRowContext(t.Context(), "SELECT to_regclass($1) IS NOT NULL", table).Scan(&exists))
			require.True(t, exists, table)
		}
	}
}

func TestContainerEquipmentMigrationRoundTrip(t *testing.T) {
	db := testutil.GetTestDB(t)
	for _, direction := range []string{"down", "up"} {
		script, err := migrations.Migrations.ReadFile("000158_container_equipment." + direction + ".sql")
		require.NoError(t, err)
		_, err = db.ExecContext(t.Context(), string(script))
		require.NoError(t, err)
		for _, table := range []string{"container_fan_observation", "container_controls_observation", "container_control_observation", "container_setpoint_observation"} {
			var exists bool
			require.NoError(t, db.QueryRowContext(t.Context(), "SELECT to_regclass($1) IS NOT NULL", table).Scan(&exists))
			require.Equal(t, direction == "up", exists, table)
		}
	}
}
