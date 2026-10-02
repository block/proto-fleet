package migrations_test

import (
	"os"
	"testing"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

// This guards against accidentally importing a dump's extension-owned catalogs
// or migration bookkeeping when the fresh-install baseline is regenerated.
func TestSharedBaselineUsesExtensionAPIs(t *testing.T) {
	script, err := os.ReadFile("current/001000_shared_baseline.up.sql")
	require.NoError(t, err)
	for _, forbidden := range []string{"_timescaledb_catalog", "_timescaledb_internal", "schema_migrations", "CREATE ROLE", "ALTER ROLE", "CREATE DATABASE"} {
		require.NotContains(t, string(script), forbidden)
	}
}

func TestBaselinePreservesContinuousAggregateStorageIntervals(t *testing.T) {
	db := testutil.GetTestDB(t)
	rows, err := db.QueryContext(t.Context(), `
		SELECT ca.view_name, d.time_interval::text
		FROM timescaledb_information.continuous_aggregates ca
		JOIN timescaledb_information.dimensions d
		  ON d.hypertable_schema = ca.materialization_hypertable_schema
		 AND d.hypertable_name = ca.materialization_hypertable_name
		WHERE ca.view_schema = 'public'
		ORDER BY ca.view_name`)
	require.NoError(t, err)
	defer rows.Close()
	actual := map[string]string{}
	for rows.Next() {
		var view, interval string
		require.NoError(t, rows.Scan(&view, &interval))
		actual[view] = interval
	}
	require.NoError(t, rows.Err())
	// Tightening the source hypertable's interval never changed these existing
	// materializations. Fresh creation otherwise derives a different interval.
	require.Equal(t, map[string]string{
		"device_metrics_daily":               "10 days",
		"device_metrics_hourly":              "10 days",
		"device_status_daily":                "10 days",
		"device_status_hourly":               "10 days",
		"fleet_telemetry_poll_heartbeat":     "10 days",
		"miner_state_snapshot_device_1m":     "10 days",
		"miner_state_snapshot_device_daily":  "10 days",
		"miner_state_snapshot_device_hourly": "10 days",
	}, actual)
}

func TestSharedBaselineDownRefusesWithoutRemovingData(t *testing.T) {
	db := testutil.GetTestDB(t)
	var before int
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT count(*) FROM permission").Scan(&before))
	script, err := os.ReadFile("current/001000_shared_baseline.down.sql")
	require.NoError(t, err)
	tx, err := db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.ExecContext(t.Context(), string(script))
	require.ErrorContains(t, err, "baseline migration cannot be reverted")
	require.NoError(t, tx.Rollback())
	var after int
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT count(*) FROM permission").Scan(&after))
	require.Equal(t, before, after)
}
