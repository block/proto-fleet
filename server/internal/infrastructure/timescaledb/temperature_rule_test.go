package timescaledb_test

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

// loadTemperatureHighRuleSQL extracts the live rawSql for the "Device Temperature High" rule.
func loadTemperatureHighRuleSQL(t *testing.T) string {
	return loadRuleSQL(t, "Device Temperature High", "fleet_device_temperature_max_celsius")
}

// writeTempSample lands one fleet_device_temperature_max_celsius sample for a device/sensor kind at the given age.
func writeTempSample(t *testing.T, db *sql.DB, org, device, kind string, value float64, age time.Duration) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), `
		INSERT INTO notification_metric_sample (time, metric, organization_id, device_id, sensor_kind, value)
		VALUES ($1, 'fleet_device_temperature_max_celsius', $2, $3, $4, $5)`,
		time.Now().Add(-age), org, device, kind, value)
	require.NoError(t, err)
}

// runTempRule executes the rule SQL and returns device_id -> latest_temp for the devices it reports as firing.
func runTempRule(t *testing.T, db *sql.DB, rawSQL string) map[string]float64 {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), rawSQL)
	require.NoError(t, err)
	defer rows.Close()

	out := map[string]float64{}
	for rows.Next() {
		var org, device string
		var latestTemp float64
		require.NoError(t, rows.Scan(&org, &device, &latestTemp))
		out[device] = latestTemp
	}
	require.NoError(t, rows.Err())
	return out
}

// TestDeviceTemperatureHighRule_Freshness covers the freshness gate: only a device still reporting a hot sample fires; one whose hot reading went stale drops out.
func TestDeviceTemperatureHighRule_Freshness(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	db := testutil.GetTestDB(t)
	rawSQL := loadTemperatureHighRuleSQL(t)

	const org = "temp-test-org"
	hotFresh := fmt.Sprintf("%s-hot-fresh", org)
	hotStale := fmt.Sprintf("%s-hot-stale", org)
	coolFresh := fmt.Sprintf("%s-cool-fresh", org)

	// Reporting now and over the threshold: must fire.
	writeTempSample(t, db, org, hotFresh, "chip", 95, 10*time.Second)
	// Hot but last reported 5 minutes ago: inside the 10-minute scan window, but the freshness gate must drop it.
	writeTempSample(t, db, org, hotStale, "chip", 95, 5*time.Minute)
	// Reporting now but below the threshold: must not fire.
	writeTempSample(t, db, org, coolFresh, "chip", 68, 10*time.Second)

	got := runTempRule(t, db, rawSQL)

	require.Contains(t, got, hotFresh, "a device reporting a fresh hot reading must fire")
	require.InDelta(t, 95.0, got[hotFresh], 1e-9)
	require.NotContains(t, got, hotStale, "a stale hot reading must not keep the rule firing")
	require.NotContains(t, got, coolFresh, "a fresh sub-threshold reading must not fire")
}

// TestDeviceTemperatureHighRule_FreshAcrossKinds covers per-kind freshness: a device whose hot kind went stale must not fire even when a cooler kind is still fresh.
func TestDeviceTemperatureHighRule_FreshAcrossKinds(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	db := testutil.GetTestDB(t)
	rawSQL := loadTemperatureHighRuleSQL(t)

	const org = "temp-kinds-org"
	device := fmt.Sprintf("%s-dev", org)

	// Hot chip reading is stale, cool board reading is fresh: the stale hot kind must be filtered out so the device does not fire.
	writeTempSample(t, db, org, device, "chip", 95, 5*time.Minute)
	writeTempSample(t, db, org, device, "board", 60, 10*time.Second)

	got := runTempRule(t, db, rawSQL)
	require.NotContains(t, got, device, "a stale hot kind must not fire while only a cool kind is fresh")
}
