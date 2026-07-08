package timescaledb_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

// writeMQTTSample lands one fleet_mqtt_* gauge sample for a source at the given age.
func writeMQTTSample(t *testing.T, db *sql.DB, metric, org, source string, value float64, age time.Duration) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), `
		INSERT INTO notification_metric_sample (time, metric, organization_id, kind, value)
		VALUES ($1, $2, $3, $4, $5)`,
		time.Now().Add(-age), metric, org, source, value)
	require.NoError(t, err)
}

// runMQTTRule executes the rule SQL and returns the set of (org, source) instances it reports as firing.
func runMQTTRule(t *testing.T, db *sql.DB, rawSQL string) map[[2]string]bool {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), rawSQL)
	require.NoError(t, err)
	defer rows.Close()

	out := map[[2]string]bool{}
	for rows.Next() {
		var org, source string
		var value float64
		require.NoError(t, rows.Scan(&org, &source, &value))
		out[[2]string{org, source}] = true
	}
	require.NoError(t, rows.Err())
	return out
}

// TestCurtailmentActiveRule fires per source while the latest gauge sample is 1 and resolves once it flips to 0 or the series ages out of the window.
func TestCurtailmentActiveRule(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	db := testutil.GetTestDB(t)
	rawSQL := loadRuleSQL(t, "Curtailment Active", "fleet_mqtt_curtailment_active")

	const org = "mqtt-curtail-org"
	const metric = "fleet_mqtt_curtailment_active"

	// Latest sample says curtailed: must fire even though an older sample said restored.
	writeMQTTSample(t, db, metric, org, "curtailing", 0, 2*time.Minute)
	writeMQTTSample(t, db, metric, org, "curtailing", 1, 10*time.Second)
	// Latest sample says restored: must not fire even though an older sample said curtailed.
	writeMQTTSample(t, db, metric, org, "restored", 1, 2*time.Minute)
	writeMQTTSample(t, db, metric, org, "restored", 0, 10*time.Second)
	// Only samples older than the 10-minute window: the instance must vanish.
	writeMQTTSample(t, db, metric, org, "aged-out", 1, 15*time.Minute)

	got := runMQTTRule(t, db, rawSQL)

	require.Contains(t, got, [2]string{org, "curtailing"}, "a source whose latest sample is curtailed must fire")
	require.NotContains(t, got, [2]string{org, "restored"}, "a source whose latest sample is restored must not fire")
	require.NotContains(t, got, [2]string{org, "aged-out"}, "samples outside the window must not fire")
}

// TestCurtailmentSourceUnreachableRule fires per source while the latest connectivity sample is 0.
func TestCurtailmentSourceUnreachableRule(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	db := testutil.GetTestDB(t)
	rawSQL := loadRuleSQL(t, "Curtailment Source Unreachable", "fleet_mqtt_source_connected")

	const org = "mqtt-conn-org"
	const metric = "fleet_mqtt_source_connected"

	// Latest sample is disconnected: must fire even though it was connected earlier.
	writeMQTTSample(t, db, metric, org, "down", 1, 2*time.Minute)
	writeMQTTSample(t, db, metric, org, "down", 0, 10*time.Second)
	// Reconnected: latest sample is connected, must not fire.
	writeMQTTSample(t, db, metric, org, "up", 0, 2*time.Minute)
	writeMQTTSample(t, db, metric, org, "up", 1, 10*time.Second)
	// Disabled source: the loop stops emitting, so the instance must vanish instead of firing forever.
	writeMQTTSample(t, db, metric, org, "disabled", 0, 15*time.Minute)

	got := runMQTTRule(t, db, rawSQL)

	require.Contains(t, got, [2]string{org, "down"}, "a source whose latest sample is disconnected must fire")
	require.NotContains(t, got, [2]string{org, "up"}, "a reconnected source must not fire")
	require.NotContains(t, got, [2]string{org, "disabled"}, "a source that stopped emitting must age out, not fire")
}
