package migrations_test

import (
	"os"
	"testing"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestPrivateBaselineDownRefusesWithoutRemovingInventory(t *testing.T) {
	db := testutil.GetTestDB(t)
	script, err := os.ReadFile("current/001001_internal_baseline.down.sql")
	require.NoError(t, err)
	tx, err := db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.ExecContext(t.Context(), string(script))
	require.ErrorContains(t, err, "baseline migration cannot be reverted")
	require.NoError(t, tx.Rollback())
	for _, relation := range []string{"container_inventory", "container_tank", "container_module", "container_module_metrics", "container_module_latest_metrics", "container_plant_metrics", "fleet_mining_asset", "container_fan_observation", "container_controls_observation", "container_control_observation", "container_setpoint_observation"} {
		var exists bool
		require.NoError(t, db.QueryRowContext(t.Context(), "SELECT to_regclass($1) IS NOT NULL", relation).Scan(&exists))
		require.True(t, exists, relation)
	}
}
