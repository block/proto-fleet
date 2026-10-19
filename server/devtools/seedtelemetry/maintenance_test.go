package main

import (
	"database/sql"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/alecthomas/kong"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

func seedTestConfig(t *testing.T, database *sql.DB) config {
	t.Helper()
	cli := struct {
		DB db.Config `envprefix:"DB_" embed:""`
	}{}
	parser, err := kong.New(&cli)
	require.NoError(t, err)
	_, err = parser.Parse(nil)
	require.NoError(t, err)
	host, port, err := net.SplitHostPort(cli.DB.Address)
	require.NoError(t, err)
	parsedPort, err := strconv.Atoi(port)
	require.NoError(t, err)
	var name string
	require.NoError(t, database.QueryRowContext(t.Context(), `SELECT current_database()`).Scan(&name))
	return config{DBHost: host, DBPort: parsedPort, DBName: name, DBUser: cli.DB.Username, DBPassword: cli.DB.Password, BatchSize: 2}
}

func seedFixture() ([]deviceMetric, time.Time, time.Time) {
	end := time.Now().UTC().Truncate(time.Hour)
	start := end.Add(-10 * 24 * time.Hour)
	metrics := generateMetrics(buildProfiles(1), start, end, 24*time.Hour, false)
	return metrics, start, end
}

func TestSeedTelemetryAndCleanupUseCheckpointedRecovery(t *testing.T) {
	database := testutil.GetTestDB(t)
	cfg := seedTestConfig(t, database)
	_, err := database.ExecContext(t.Context(), `CALL telemetry_summary_step('device_metrics')`)
	require.NoError(t, err, "empty coverage has already initialized, reproducing the old seeder failure")
	metrics, start, end := seedFixture()
	require.NoError(t, seedAndRefresh(cfg, metrics, start, end, time.Now()))
	for _, aggregate := range []string{"device_metrics_hourly", "device_metrics_daily", "device_status_hourly", "device_status_daily"} {
		var count int
		require.NoError(t, database.QueryRowContext(t.Context(), `SELECT count(*) FROM `+aggregate+` WHERE device_identifier LIKE 'seed-device-%'`).Scan(&count))
		require.Positive(t, count, aggregate)
	}
	var completed, scheduled int
	require.NoError(t, database.QueryRowContext(t.Context(), `SELECT count(*) FROM telemetry_summary_progress
 WHERE source_name='device_metrics' AND completed_through>=time_bucket(bucket_width,now()-lateness)`).Scan(&completed))
	require.Equal(t, 6, completed, "all dependent tiers, including container summaries, must be covered")
	for i := range metrics {
		metrics[i].HashRateHs = 123
	}
	require.NoError(t, seedAndRefresh(cfg, metrics, start, end, time.Now()))
	var value float64
	require.NoError(t, database.QueryRowContext(t.Context(), `SELECT avg_hash_rate FROM device_metrics_hourly WHERE device_identifier=$1 AND bucket=time_bucket(INTERVAL '1 hour',$2::timestamptz)`, metrics[0].DeviceIdentifier, start).Scan(&value))
	require.Equal(t, 123.0, value, "seeding can safely revise certified history whose raw remains intact")
	require.NoError(t, runCleanup(cfg))
	for _, table := range []string{"device_metrics", "device_metrics_hourly", "device_metrics_daily", "device_status_hourly", "device_status_daily"} {
		var count int
		require.NoError(t, database.QueryRowContext(t.Context(), `SELECT count(*) FROM `+table+` WHERE device_identifier LIKE 'seed-device-%'`).Scan(&count))
		require.Zero(t, count, table+" must not retain deleted synthetic readings")
	}
	require.NoError(t, database.QueryRowContext(t.Context(), `SELECT count(*) FROM timescaledb_information.jobs WHERE proc_name='maintain_telemetry_source' AND scheduled`).Scan(&scheduled))
	require.Zero(t, scheduled, "seeding must preserve deliberately paused schedules")
}

func TestSeedTelemetryRebuildsRecentMaterializedBucketsWhilePaused(t *testing.T) {
	for _, operation := range []string{"replace", "shorten", "cleanup"} {
		t.Run(operation, func(t *testing.T) {
			database := testutil.GetTestDB(t)
			cfg := seedTestConfig(t, database)
			metrics, start, end := seedFixture()
			latest := metrics[len(metrics)-1]
			// An inclusive raw maximum on a bucket boundary must still rebuild
			// that entire completed bucket after deletion or a shorter import.
			latest.Time = end.Add(-time.Hour)
			metrics = append(metrics, latest)
			for i := range metrics {
				metrics[i].HashRateHs = 100
				metrics[i].Health = healthActive
			}
			require.NoError(t, seedAndRefresh(cfg, metrics, start, end, time.Now()))
			checks := []struct {
				aggregate string
				bucket    time.Time
				column    string
				want      float64
			}{
				{"device_metrics_hourly", latest.Time, "avg_hash_rate", 123},
				{"device_metrics_daily", end.Truncate(24 * time.Hour).Add(-24 * time.Hour), "avg_hash_rate", 123},
				{"device_status_hourly", latest.Time, "hashing_count", 0},
				{"device_status_daily", end.Truncate(24 * time.Hour).Add(-24 * time.Hour), "hashing_count", 0},
			}
			for _, check := range checks {
				_, err := database.ExecContext(t.Context(), `ALTER MATERIALIZED VIEW `+check.aggregate+` SET (timescaledb.materialized_only=true)`)
				require.NoError(t, err)
				_, err = database.ExecContext(t.Context(), `CALL refresh_continuous_aggregate($1::regclass,$2::timestamptz,$3::timestamptz,force=>true)`, check.aggregate, start, end)
				require.NoError(t, err)
				var count int
				require.NoError(t, database.QueryRowContext(t.Context(), `SELECT count(*) FROM `+check.aggregate+` WHERE device_identifier=$1 AND bucket=$2`, latest.DeviceIdentifier, check.bucket).Scan(&count))
				require.Equal(t, 1, count, "fixture must already contain the materialized bucket: %s", check.aggregate)
			}
			if operation == "cleanup" {
				require.NoError(t, runCleanup(cfg))
			} else {
				if operation == "shorten" {
					end = end.Add(-48 * time.Hour)
				}
				replacement := make([]deviceMetric, 0, len(metrics))
				for _, metric := range metrics {
					if metric.Time.Before(end) {
						metric.HashRateHs = 123
						metric.Health = healthCritical
						replacement = append(replacement, metric)
					}
				}
				require.NoError(t, seedAndRefresh(cfg, replacement, start, end, time.Now()))
			}
			for _, check := range checks {
				var count int
				require.NoError(t, database.QueryRowContext(t.Context(), `SELECT count(*) FROM `+check.aggregate+` WHERE device_identifier=$1 AND bucket=$2`, latest.DeviceIdentifier, check.bucket).Scan(&count))
				if operation != "replace" {
					require.Zero(t, count, "deleted seed values must disappear immediately: %s", check.aggregate)
					continue
				}
				require.Equal(t, 1, count, check.aggregate)
				var value float64
				require.NoError(t, database.QueryRowContext(t.Context(), `SELECT `+check.column+` FROM `+check.aggregate+` WHERE device_identifier=$1 AND bucket=$2`, latest.DeviceIdentifier, check.bucket).Scan(&value))
				require.Equal(t, check.want, value, "replacements must be materialized immediately: %s", check.aggregate)
			}
			var normalBoundary bool
			require.NoError(t, database.QueryRowContext(t.Context(), `SELECT
 bool_and(p.completed_through<=time_bucket(p.bucket_width,now()-p.lateness))
 AND g.frozen_through<=max(time_bucket(p.bucket_width,now()-p.lateness))
 FROM telemetry_summary_progress p JOIN telemetry_summary_write_gate g USING (source_name)
 WHERE p.source_name='device_metrics' GROUP BY g.frozen_through`).Scan(&normalBoundary))
			require.True(t, normalBoundary, "recent seed repairs must preserve the normal lateness boundary")
			var scheduled int
			require.NoError(t, database.QueryRowContext(t.Context(), `SELECT count(*) FROM timescaledb_information.jobs WHERE proc_name='maintain_telemetry_source' AND scheduled`).Scan(&scheduled))
			require.Zero(t, scheduled, "maintenance must remain paused")
		})
	}
}

func TestSeedTelemetryFailedReplacementPreservesExistingRows(t *testing.T) {
	database := testutil.GetTestDB(t)
	cfg := seedTestConfig(t, database)
	metrics, start, end := seedFixture()
	require.NoError(t, seedAndRefresh(cfg, metrics, start, end, time.Now()))
	// Force a later batch to fail, after earlier replacements were inserted.
	metrics[len(metrics)-1].DeviceIdentifier = "seed-device-\x00invalid"
	err := seedAndRefresh(cfg, metrics, start, end, time.Now())
	require.ErrorContains(t, err, "inserting data")
	var count int
	require.NoError(t, database.QueryRowContext(t.Context(), `SELECT count(*) FROM device_metrics WHERE device_identifier LIKE 'seed-device-%'`).Scan(&count))
	require.Len(t, metrics, count, "the replacement transaction must restore all original seed rows")
}

func TestSeedTelemetryRefreshFailurePinsRawAndReleasesMaintenance(t *testing.T) {
	database := testutil.GetTestDB(t)
	cfg := seedTestConfig(t, database)
	_, err := database.ExecContext(t.Context(), `CREATE FUNCTION fail_seed_ack() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN RAISE EXCEPTION 'injected seed acknowledgement failure'; END $$;
 CREATE TRIGGER fail_seed_ack BEFORE UPDATE OF completed_through ON telemetry_summary_progress
 FOR EACH ROW WHEN (NEW.completed_through>OLD.completed_through) EXECUTE FUNCTION fail_seed_ack()`)
	require.NoError(t, err)
	metrics, start, end := seedFixture()
	err = seedAndRefresh(cfg, metrics, start, end, time.Now())
	require.ErrorContains(t, err, "injected seed acknowledgement failure")
	var pending, available bool
	require.NoError(t, database.QueryRowContext(t.Context(), `SELECT bool_or(pending) FROM telemetry_summary_work`).Scan(&pending))
	require.True(t, pending, "a failed refresh must remain recoverable without acknowledging coverage")
	require.NoError(t, database.QueryRowContext(t.Context(), `SELECT pg_try_advisory_xact_lock(1030,'device_metrics'::regclass::oid::integer)`).Scan(&available))
	require.True(t, available, "the failed command must release its session maintenance lock")
	var count int
	require.NoError(t, database.QueryRowContext(t.Context(), `SELECT count(*) FROM device_metrics WHERE device_identifier LIKE 'seed-device-%'`).Scan(&count))
	require.Len(t, metrics, count, "new raw evidence must survive a refresh failure")
}

func TestSeedTelemetryRefusesExpiredHistoryBeforeDeleting(t *testing.T) {
	database := testutil.GetTestDB(t)
	cfg := seedTestConfig(t, database)
	_, err := database.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs) VALUES(now(),'seed-device-existing',100);
 UPDATE telemetry_summary_source SET last_retention_at=now(),dropped_through=now()-INTERVAL '1 day' WHERE source_name='device_metrics'`)
	require.NoError(t, err)
	end := time.Now().UTC().Add(-10 * 24 * time.Hour)
	start := end.Add(-24 * time.Hour)
	metrics := generateMetrics(buildProfiles(1), start, end, time.Hour, false)
	err = seedAndRefresh(cfg, metrics, start, end, time.Now())
	require.ErrorContains(t, err, "existing rows were preserved")
	var count int
	require.NoError(t, database.QueryRowContext(t.Context(), `SELECT count(*) FROM device_metrics WHERE device_identifier='seed-device-existing'`).Scan(&count))
	require.Equal(t, 1, count)
}
