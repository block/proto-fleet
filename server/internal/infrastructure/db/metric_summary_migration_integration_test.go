package db

import (
	"io/fs"
	"testing"

	"github.com/block/proto-fleet/server/migrations"
	"github.com/stretchr/testify/require"
)

func TestMetricSummaryMigrationPreservesCompressedHistory(t *testing.T) {
	requireBaselineDB(t)
	conn, config := newMigrationBridgeTestDB(t)
	files, err := fs.Sub(migrations.Current, "current")
	require.NoError(t, err)
	require.NoError(t, migrateTestDBSourceTo(t.Context(), conn, config.Name, files, 1015))
	seedBaselineIdentity(t, conn)

	// Pin settings to the same connection used by the production migration runner.
	// This remains a regression test even when a developer disabled the limit as
	// a workaround in their local database configuration.
	conn.SetMaxOpenConns(1)
	//nolint:dupword // Fixture source time and collection time intentionally match.
	_, err = conn.ExecContext(t.Context(), `
SET timescaledb.max_tuples_decompressed_per_dml_transaction = 100000;
SELECT alter_job(job_id, scheduled => false) FROM timescaledb_information.jobs;
INSERT INTO container_inventory(device_id, observed_at) VALUES (999999, now());
INSERT INTO container_tank(id, device_id, position, first_seen_at, last_seen_at)
VALUES (999999, 999999, 1, now(), now());
INSERT INTO container_module(id, tank_id, position, first_seen_at, last_seen_at)
VALUES (999999, 999999, 1, now(), now());
INSERT INTO hashboard(id, org_id, first_seen_at, last_seen_at)
VALUES (999999, 999999, now(), now());
INSERT INTO hashboard_installation(id, hashboard_id, module_id, position, installed_at, last_seen_at)
VALUES (999999, 999999, 999999, 1, now() - INTERVAL '3 days', now());
INSERT INTO container_topology_version(id, device_id, fingerprint, topology)
VALUES (999999, 999999, 'summary-migration', '{"site":{"Valid":true,"Int64":123}}');
INSERT INTO container_topology_observation(id, device_id, version_id, collected_at, complete)
VALUES (999998, 999999, 999999, date_trunc('hour', now() - INTERVAL '2 days') + INTERVAL '5 minutes', true),
       (999999, 999999, 999999, now() - INTERVAL '1 minute', true);

-- The legacy history exceeds TimescaleDB's default per-transaction limit and
-- has no topology reference, as on a fleet upgraded from before migration 1015.
INSERT INTO device_metrics(time, device_identifier, hash_rate_hs)
SELECT date_trunc('hour', now() - INTERVAL '2 days') + n * INTERVAL '1 millisecond',
       'fixture-device', n
FROM generate_series(1, 110000) n;
INSERT INTO device_metrics(time, device_identifier, hash_rate_hs, metric_semantics, topology_observation_id)
SELECT collected_at, 'fixture-device', 123, 2, id FROM container_topology_observation;
INSERT INTO container_tank_metrics(tank_id, time, topology_observation_id, hashrate_hs, coverage)
SELECT 999999, collected_at, id, 123, '{}' FROM container_topology_observation;
INSERT INTO container_module_metrics(module_id, time, collected_at, hashrate_hs)
SELECT 999999, collected_at, collected_at, 123 FROM container_topology_observation;
INSERT INTO hashboard_metrics(hashboard_id, installation_id, time, collected_at, state, mining_state, hashrate_hs)
SELECT 999999, 999999, collected_at, collected_at, 'hashing', 'mining', 123 FROM container_topology_observation;
SELECT compress_chunk(c) FROM show_chunks('device_metrics', older_than => INTERVAL '1 day') c;
`)
	require.NoError(t, err)

	var compressedBefore int
	var rowstoreBytesBefore int64
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT count(*),
sum(pg_relation_size(format('%I.%I', chunk_schema, chunk_name)::regclass))
FROM timescaledb_information.chunks WHERE hypertable_name='device_metrics' AND is_compressed`).Scan(&compressedBefore, &rowstoreBytesBefore))
	require.Positive(t, compressedBefore)
	require.Zero(t, rowstoreBytesBefore)

	// Exercise startup's checkpoint transaction, including 1016 and the related
	// hashboard provenance migration, against the already populated database.
	require.NoError(t, runCurrentMigrations(t.Context(), conn, migrations.Current))
	_, latest, err := migrationCheckpoints(migrations.Current)
	require.NoError(t, err)
	var version int
	var dirty bool
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT version, dirty FROM schema_migrations`).Scan(&version, &dirty))
	require.Equal(t, latest, version)
	require.False(t, dirty)

	var compressedAfter int
	var rowstoreBytesAfter int64
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT count(*),
sum(pg_relation_size(format('%I.%I', chunk_schema, chunk_name)::regclass))
FROM timescaledb_information.chunks WHERE hypertable_name='device_metrics' AND is_compressed`).Scan(&compressedAfter, &rowstoreBytesAfter))
	require.Equal(t, compressedBefore, compressedAfter, "historical chunks must remain compressed")
	// is_compressed also stays true after partial decompression. The original
	// chunk heap must stay empty, without historical rows moved into row storage.
	require.Zero(t, rowstoreBytesAfter)
	var legacyCount int
	var legacyHashrateSum float64
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT count(*), sum(hash_rate_hs)
FROM device_metrics WHERE topology_observation_id IS NULL AND topology_version_id IS NULL`).Scan(&legacyCount, &legacyHashrateSum))
	require.Equal(t, 110000, legacyCount)
	require.Equal(t, float64(110000*110001/2), legacyHashrateSum)

	// Only compressed device history skips the provenance backfill. Uncompressed
	// tank, module and hashboard history receives provenance regardless of age.
	rows, err := conn.QueryContext(t.Context(), `
SELECT 'device', m.time > now() - INTERVAL '6 hours', m.topology_version_id, m.hash_rate_hs
FROM device_metrics m WHERE m.topology_observation_id IS NOT NULL
UNION ALL
SELECT 'tank', time > now() - INTERVAL '6 hours', topology_version_id, hashrate_hs FROM container_tank_metrics
UNION ALL
SELECT 'module', time > now() - INTERVAL '6 hours', topology_version_id, hashrate_hs FROM container_module_metrics
UNION ALL
SELECT 'hashboard', time > now() - INTERVAL '6 hours', topology_version_id, hashrate_hs FROM hashboard_metrics`)
	require.NoError(t, err)
	defer rows.Close()
	var count int
	for rows.Next() {
		var table string
		var recent bool
		var version *int64
		var hashrate float64
		require.NoError(t, rows.Scan(&table, &recent, &version, &hashrate))
		require.Equal(t, float64(123), hashrate, table)
		if recent || table != "device" {
			require.NotNil(t, version, table)
			require.Equal(t, int64(999999), *version, table)
		} else {
			require.Nil(t, version, table)
		}
		count++
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	require.Equal(t, 8, count)
	var preserved bool
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT
 (SELECT count(*)=2 FROM container_topology_observation o JOIN container_topology_version v ON v.id=o.version_id WHERE v.fingerprint='summary-migration') AND
 (SELECT count(*)=2 FROM container_module_metrics WHERE topology_version_id=999999 AND site_id=123) AND
 (SELECT count(*)=2 FROM device_metrics m JOIN container_topology_observation o ON o.id=m.topology_observation_id)`).Scan(&preserved))
	require.True(t, preserved, "retain old topology references and restore historical module sites at every age")
}
