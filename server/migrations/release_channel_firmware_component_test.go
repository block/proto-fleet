package migrations_test

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
)

// Migration 1111 keys assignments, rollouts and provenance by firmware
// component. Existing rows become the main component; rolling back keeps them
// and drops what only the newer schema can hold.
func TestReleaseChannelFirmwareComponentMigrationDownAndUp(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	read := func(name string) string {
		t.Helper()
		contents, err := migrations.Current.ReadFile("current/001111_internal_release_channel_firmware_component." + name + ".sql")
		require.NoError(t, err)
		return string(contents)
	}
	f := newReleaseChannelFixture(t, db)
	channel := f.channel("components")
	device := f.device("components", 0)
	for _, component := range []string{"", "psu"} {
		f.exec(`INSERT INTO release_channel_firmware (channel_id, manufacturer, model, component, firmware_checksum, firmware_version, assigned_by)
			VALUES ($1, 'Bitmain', 'S19', $2, 'sum-' || $2, 'v1', 1)`, channel, component)
		f.exec(`INSERT INTO firmware_rollout (org_id, channel_id, manufacturer, model, component, firmware_checksum, firmware_version, assignment_generation)
			VALUES ($1, $2, 'Bitmain', 'S19', $3, 'sum-' || $3, 'v1', 1)`, f.org, channel, component)
		f.exec(`INSERT INTO device_firmware_deployment (device_id, component, firmware_checksum, firmware_version) VALUES ($1, $2, 'sum-' || $2, 'v1')`, device.id, component)
	}
	count := func(table string) int {
		t.Helper()
		var n int
		require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&n)) //nolint:gosec // fixed table names
		return n
	}
	reported := func(query string, args ...any) sql.NullString {
		t.Helper()
		var version sql.NullString
		require.NoError(t, db.QueryRowContext(ctx, query, args...).Scan(&version))
		return version
	}
	require.Equal(t, sql.NullString{String: "1.2.3", Valid: true}, reported(`SELECT release_channel_firmware_version($1, 1, '1.2.3', '')`, device.id))
	require.False(t, reported(`SELECT release_channel_firmware_version($1, 1, '1.2.3', 'psu')`, device.id).Valid, "only a container reports PSU firmware")

	_, err := db.ExecContext(ctx, read("down"))
	require.NoError(t, err)
	for _, table := range []string{"release_channel_firmware", "firmware_rollout", "device_firmware_deployment"} {
		require.Equal(t, 1, count(table), "%s keeps its main-component row", table)
	}
	var checksum string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT firmware_checksum FROM release_channel_firmware WHERE channel_id = $1`, channel).Scan(&checksum))
	require.Equal(t, "sum-", checksum)
	_, err = db.ExecContext(ctx, `INSERT INTO device_firmware_deployment (device_id, firmware_checksum, firmware_version) VALUES ($1, 'other', 'v2')`, device.id)
	require.Error(t, err, "provenance is one row per device again")
	require.Equal(t, sql.NullString{String: "1.2.3", Valid: true}, reported(`SELECT release_channel_firmware_version($1, 1, '1.2.3')`, device.id))
	require.Zero(t, count("firmware_rollout_suppressed_device"))

	_, err = db.ExecContext(ctx, read("up"))
	require.NoError(t, err)
	for _, table := range []string{"release_channel_firmware", "firmware_rollout", "device_firmware_deployment"} {
		var component string
		require.NoError(t, db.QueryRowContext(ctx, `SELECT component FROM `+table).Scan(&component)) //nolint:gosec // fixed table names
		require.Empty(t, component, "%s rows from before the migration are the main component", table)
	}
	f.exec(`INSERT INTO device_firmware_deployment (device_id, component, firmware_checksum, firmware_version) VALUES ($1, 'psu', 'capsule', 'v1')`, device.id)
	require.Equal(t, sql.NullString{String: "1.2.3", Valid: true}, reported(`SELECT release_channel_firmware_version($1, 1, '1.2.3', '')`, device.id))
	var suppressedColumns int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.columns
		WHERE table_name = 'firmware_rollout_suppressed_device' AND column_name = 'component'`).Scan(&suppressedColumns))
	require.Equal(t, 1, suppressedColumns)
}
