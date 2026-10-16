package timescaledb

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/stretchr/testify/require"
)

var deviceMetricRecoveryAggregates = []string{
	"device_metrics_hourly", "device_metrics_daily", "device_status_hourly", "device_status_daily",
	"container_metrics_hourly", "container_metrics_daily",
}

var recoveryAggregates = append(deviceMetricRecoveryAggregates,
	"container_tank_metrics_hourly", "container_tank_metrics_daily",
	"container_module_metrics_hourly", "container_module_metrics_daily", "hashboard_metrics_hourly", "hashboard_metrics_daily",
)

var recoverySources = []string{"device_metrics", "container_tank_metrics", "container_module_metrics", "hashboard_metrics"}

// removeTelemetryRecovery reconstructs the pre-1027 schema so tests can apply
// the real migration after loading backdated upgrade fixtures.
func removeTelemetryRecovery(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), `SELECT delete_job(job_id) FROM timescaledb_information.jobs
	 WHERE proc_name='maintain_telemetry_source';
	 DROP VIEW telemetry_summary_health,telemetry_retention_health;
	 DROP PROCEDURE maintain_telemetry_source(integer,jsonb),telemetry_summary_step(text);
     DROP FUNCTION retain_telemetry_raw(text),prepare_telemetry_seed(timestamptz),telemetry_bucket_ceiling(interval,timestamptz);
     DROP TABLE telemetry_summary_work,telemetry_summary_progress,telemetry_summary_write_gate,telemetry_summary_source;
     DROP FUNCTION guard_telemetry_summary_time() CASCADE;
     DROP FUNCTION lock_telemetry_summary_writes() CASCADE;`)
	require.NoError(t, err)
	testutil.RestoreLegacySummaryBootstraps(t, db,
		"001016_internal_ct1_metric_summaries",
		"001021_internal_hashboard_metric_summaries")
}

func telemetryRecoveryMigration(t *testing.T) string {
	t.Helper()
	up, err := migrations.Current.ReadFile("current/001027_internal_telemetry_summary_recovery.up.sql")
	require.NoError(t, err)
	return string(up) + `
	 SELECT alter_job(job_id,scheduled=>false) FROM timescaledb_information.jobs WHERE scheduled;`
}

func reinstallTelemetryRecovery(t *testing.T, db *sql.DB) {
	t.Helper()
	removeTelemetryRecovery(t, db)
	// These fixtures model maintenance installed before a later outage. Install
	// at their first raw day, then restore the real clock before driving recovery.
	// Upgrade regressions apply telemetryRecoveryMigration directly at today's
	// clock, exercising its conservative boundary for uncertain older history.
	var installedAt time.Time
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT COALESCE(min(range_start),now())
 FROM timescaledb_information.chunks WHERE hypertable_schema='public'
 AND hypertable_name IN ('device_metrics','container_tank_metrics','container_module_metrics','hashboard_metrics')`).Scan(&installedAt))
	conn, err := db.Conn(t.Context())
	require.NoError(t, err)
	defer conn.Close()
	_, err = conn.ExecContext(t.Context(), fmt.Sprintf(`CREATE OR REPLACE FUNCTION public.now() RETURNS timestamptz LANGUAGE sql STABLE AS
 $$ SELECT '%s'::timestamptz $$; SET search_path=public,pg_catalog`, installedAt.Format(time.RFC3339Nano)))
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), telemetryRecoveryMigration(t))
	require.NoError(t, err)
	// Health views resolve now() during creation; retain their dependency while
	// delegating it back to the actual transaction clock.
	_, err = conn.ExecContext(t.Context(), `CREATE OR REPLACE FUNCTION public.now() RETURNS timestamptz LANGUAGE sql STABLE AS
 $$ SELECT pg_catalog.now() $$; RESET search_path`)
	require.NoError(t, err)
}

func recoveryJob(t *testing.T, db *sql.DB, source string) int {
	t.Helper()
	var id int
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT job_id FROM telemetry_summary_source WHERE source_name=$1`, source).Scan(&id))
	return id
}

// Most regressions drive one internal worker quantum synchronously. Public
// foreground run_job only dispatches; the scheduled-worker test covers that path.
func selectRecoveryWork(t *testing.T, db *sql.DB, aggregate, kind string) string {
	t.Helper()
	var source string
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT source_name FROM telemetry_summary_progress WHERE aggregate_name=$1`, aggregate).Scan(&source))
	_, err := db.ExecContext(t.Context(), `UPDATE telemetry_summary_work SET next_run_at=now()+INTERVAL '1 day';
 UPDATE telemetry_summary_source SET next_retention_at=now()+INTERVAL '1 day'`)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `UPDATE telemetry_summary_work SET next_run_at=now()-INTERVAL '1 second' WHERE aggregate_name=$1 AND work_kind=$2`, aggregate, kind)
	require.NoError(t, err)
	return source
}

func runRecovery(t *testing.T, db *sql.DB, aggregate string) {
	t.Helper()
	source := selectRecoveryWork(t, db, aggregate, "catchup")
	_, err := db.ExecContext(t.Context(), `CALL telemetry_summary_step($1)`, source)
	require.NoError(t, err, aggregate)
}

func runRecentRecovery(t *testing.T, db *sql.DB, aggregate string) {
	t.Helper()
	source := selectRecoveryWork(t, db, aggregate, "recent")
	_, err := db.ExecContext(t.Context(), `CALL telemetry_summary_step($1)`, source)
	require.NoError(t, err, aggregate)
}

func recoveryCompletedThrough(t *testing.T, db *sql.DB, aggregate string) time.Time {
	t.Helper()
	var completed time.Time
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT completed_through
	 FROM telemetry_summary_progress WHERE aggregate_name=$1`, aggregate).Scan(&completed))
	return completed
}

func runRawRetention(t *testing.T, db *sql.DB, source string) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), `SELECT retain_telemetry_raw($1)`, source)
	require.NoError(t, err, source)
}

func finishRecovery(t *testing.T, db *sql.DB, aggregates []string) {
	t.Helper()
	for _, aggregate := range aggregates {
		var finished bool
		for range 40 {
			runRecovery(t, db, aggregate)
			require.NoError(t, db.QueryRowContext(t.Context(), `SELECT completed_through>=time_bucket(bucket_width,now()-lateness)
			 FROM telemetry_summary_progress WHERE aggregate_name=$1`, aggregate).Scan(&finished))
			if finished {
				break
			}
		}
		require.True(t, finished, "catch-up did not finish: %s", aggregate)
	}
}

func lockSummaryMaterialization(t *testing.T, db *sql.DB, aggregate string) *sql.Tx {
	t.Helper()
	var materialization string
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT format('%I.%I',materialization_hypertable_schema,materialization_hypertable_name)
	 FROM timescaledb_information.continuous_aggregates WHERE view_name=$1`, aggregate).Scan(&materialization))
	tx, err := db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	_, err = tx.ExecContext(t.Context(), `LOCK TABLE `+materialization+` IN ACCESS EXCLUSIVE MODE`)
	require.NoError(t, err)
	return tx
}

func TestTelemetrySummaryRecoveryAfterLongOutage(t *testing.T) {
	r := newContainerRollupRig(t)
	r.base = time.Now().UTC().Truncate(24 * time.Hour).Add(-24 * 24 * time.Hour)
	// Establish an intact UTC day before the outage, followed by measurements
	// outside both former moving refresh windows and beyond raw retention.
	r.store(t, r.snapshot(0, false, 10e12, 20e12))
	reinstallTelemetryRecovery(t, r.db)
	at := r.base.Add(3*24*time.Hour + time.Hour)
	r.store(t, r.snapshot(at.Sub(r.base), false, 100e12, 200e12))
	_, err := r.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs,temp_c,power_w)
	 VALUES ($1,'recovery-miner',500e12,60,3000)`, at)
	require.NoError(t, err)
	for _, aggregate := range recoveryAggregates {
		runRecovery(t, r.db, aggregate)
	}
	for _, aggregate := range recoveryAggregates {
		width, lookback, delay := "1 hour", "1 day", "1 hour"
		if strings.HasSuffix(aggregate, "_daily") {
			width, lookback, delay = "1 day", "7 days", "1 day"
		}
		// Reproduce the old behavior: a successful recent-window refresh is
		// not evidence that the retained twenty-one-day-old data was summarized.
		_, err := r.db.ExecContext(t.Context(), `ALTER MATERIALIZED VIEW `+aggregate+` SET (timescaledb.materialized_only=true)`)
		require.NoError(t, err)
		var start, end time.Time
		require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT now()-$1::interval,now()-$2::interval`, lookback, delay).Scan(&start, &end))
		testutil.RefreshContinuousAggregate(t, r.db, aggregate, &start, &end)
		var count int
		require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM `+aggregate+` WHERE bucket=time_bucket($1::interval,$2::timestamptz)`, width, at).Scan(&count))
		require.Zero(t, count, aggregate)
	}
	for _, source := range recoverySources {
		runRawRetention(t, r.db, source)
		var count int
		require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM `+source+` WHERE time=$1`, at).Scan(&count))
		require.Positive(t, count, "unfinished %s must retain raw", source)
	}
	// Finish every tier except one dependent on the shared device_metrics source.
	finishRecovery(t, r.db, recoveryAggregates[1:])
	runRawRetention(t, r.db, "device_metrics")
	var count int
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM device_metrics WHERE time=$1`, at).Scan(&count))
	require.Positive(t, count, "container completion cannot bypass unfinished miner summaries")
	finishRecovery(t, r.db, recoveryAggregates[:1])
	for _, source := range recoverySources {
		// Retention also works in bounded batches (device chunks are hourly).
		for range 4 {
			_, err := r.db.ExecContext(t.Context(), `UPDATE telemetry_summary_source SET next_retention_at=now() WHERE source_name=$1`, source)
			require.NoError(t, err)
			_, err = r.db.ExecContext(t.Context(), `CALL telemetry_summary_step($1)`, source)
			require.NoError(t, err)
		}
		require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM `+source+` WHERE time=$1`, at).Scan(&count))
		require.Zero(t, count, source)
	}
	for _, aggregate := range recoveryAggregates {
		width := "1 hour"
		if strings.HasSuffix(aggregate, "_daily") {
			width = "1 day"
		}
		require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM `+aggregate+` WHERE bucket=time_bucket($1::interval,$2::timestamptz)`, width, at).Scan(&count))
		require.Positive(t, count, "recovered %s must survive raw expiration", aggregate)
	}
}

func TestTelemetrySummaryEmptySourcePreservesFirstDay(t *testing.T) {
	db := testutil.GetTestDB(t)
	removeTelemetryRecovery(t, db)
	conn, err := db.Conn(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	exec := func(query string, args ...any) {
		t.Helper()
		_, err := conn.ExecContext(t.Context(), query, args...)
		require.NoError(t, err)
	}
	// Migrate an empty source at noon, then start ingestion before any recovery
	// job runs. The first 13:00 chunk must not push daily coverage to tomorrow.
	exec(`CREATE FUNCTION public.now() RETURNS timestamptz LANGUAGE sql STABLE AS
	 $$ SELECT '2026-09-01 12:00:00+00'::timestamptz $$;
	 SET search_path=public,pg_catalog`)
	exec(telemetryRecoveryMigration(t))
	for _, source := range recoverySources {
		var floor time.Time
		require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT raw_floor FROM telemetry_summary_source WHERE source_name=$1`, source).Scan(&floor))
		require.Equal(t, time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC), floor.UTC(), source)
	}
	exec(`INSERT INTO device_metrics(time,device_identifier,hash_rate_hs) VALUES
	 ('2026-09-01 13:00:00+00','first-day-miner',100),
	 ('2026-09-02 13:00:00+00','first-day-miner',200)`)
	exec(`CREATE OR REPLACE FUNCTION public.now() RETURNS timestamptz LANGUAGE sql STABLE AS
	 $$ SELECT '2026-09-21 12:00:00+00'::timestamptz $$`)
	for _, aggregate := range deviceMetricRecoveryAggregates {
		for range 25 {
			exec(`UPDATE telemetry_summary_work SET next_run_at=now()+INTERVAL '1 day'`)
			exec(`UPDATE telemetry_summary_work SET next_run_at=now()-INTERVAL '1 second' WHERE aggregate_name=$1 AND work_kind='catchup'`, aggregate)
			exec(`CALL telemetry_summary_step('device_metrics')`)
		}
	}
	exec(`SELECT retain_telemetry_raw('device_metrics')`)
	for _, aggregate := range []string{"device_metrics_daily", "device_status_daily"} {
		var firstDay, secondDay int
		require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT
		 count(*) FILTER (WHERE bucket='2026-09-01 00:00:00+00'::timestamptz),
		 count(*) FILTER (WHERE bucket='2026-09-02 00:00:00+00'::timestamptz)
		 FROM `+aggregate+` WHERE device_identifier='first-day-miner'`).Scan(&firstDay, &secondDay))
		require.Equal(t, 1, firstDay, aggregate)
		require.Equal(t, 1, secondDay, aggregate)
	}
	var rawCount int
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT count(*) FROM device_metrics`).Scan(&rawCount))
	require.Zero(t, rawCount, "fully summarized first-day raw chunks must expire")
}

func TestTelemetrySummaryRecoveryCheckpointFailureAndRetry(t *testing.T) {
	r := newContainerRollupRig(t)
	r.base = time.Now().UTC().Truncate(24 * time.Hour).Add(-12 * 24 * time.Hour)
	r.store(t, r.snapshot(0, false, 100e12, 200e12))
	r.store(t, r.snapshot(36*time.Hour, false, 300e12, 400e12))
	aggregate := "hashboard_metrics_hourly"
	reinstallTelemetryRecovery(t, r.db)
	runRecovery(t, r.db, aggregate)
	before := recoveryCompletedThrough(t, r.db, aggregate)
	require.WithinDuration(t, r.base.Add(24*time.Hour), before, time.Microsecond, "only one day per batch")
	_, err := r.db.ExecContext(t.Context(), `ALTER MATERIALIZED VIEW hashboard_metrics_hourly RENAME TO hashboard_metrics_hourly_unavailable`)
	require.NoError(t, err)
	source := selectRecoveryWork(t, r.db, aggregate, "catchup")
	_, err = r.db.ExecContext(t.Context(), `CALL telemetry_summary_step($1)`, source)
	require.Error(t, err)
	after := recoveryCompletedThrough(t, r.db, aggregate)
	require.True(t, before.Equal(after), "failure cannot acknowledge coverage")
	runRawRetention(t, r.db, "hashboard_metrics")
	var deferrals int
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT retention_deferrals FROM telemetry_summary_source WHERE source_name='hashboard_metrics'`).Scan(&deferrals))
	require.Positive(t, deferrals)
	_, err = r.db.ExecContext(t.Context(), `ALTER MATERIALIZED VIEW hashboard_metrics_hourly_unavailable RENAME TO hashboard_metrics_hourly`)
	require.NoError(t, err)
	runRecovery(t, r.db, aggregate)
	after = recoveryCompletedThrough(t, r.db, aggregate)
	require.WithinDuration(t, before.Add(12*time.Hour), after, time.Microsecond)
	// Also fail after Timescale has committed its materialization but before
	// the coverage acknowledgement. Retrying cannot skip or double the bucket.
	_, err = r.db.ExecContext(t.Context(), `CREATE FUNCTION fail_summary_ack() RETURNS trigger LANGUAGE plpgsql AS $$
	 BEGIN RAISE EXCEPTION 'acknowledgement interrupted'; END $$;
	 CREATE TRIGGER fail_summary_ack BEFORE UPDATE OF completed_through ON telemetry_summary_progress
	 FOR EACH ROW EXECUTE FUNCTION fail_summary_ack();`)
	require.NoError(t, err)
	selectRecoveryWork(t, r.db, aggregate, "catchup")
	_, err = r.db.ExecContext(t.Context(), `CALL telemetry_summary_step($1)`, source)
	require.ErrorContains(t, err, "acknowledgement interrupted")
	unacknowledged := recoveryCompletedThrough(t, r.db, aggregate)
	require.True(t, after.Equal(unacknowledged))
	var observations int
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT sum(observations) FROM hashboard_metrics_hourly WHERE bucket=$1`, r.base.Add(36*time.Hour)).Scan(&observations))
	require.Equal(t, 2, observations, "the materialization committed before its acknowledgement failed")
	_, err = r.db.ExecContext(t.Context(), `DROP TRIGGER fail_summary_ack ON telemetry_summary_progress; DROP FUNCTION fail_summary_ack();`)
	require.NoError(t, err)
	runRecovery(t, r.db, aggregate)
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT sum(observations) FROM hashboard_metrics_hourly WHERE bucket=$1`, r.base.Add(36*time.Hour)).Scan(&observations))
	require.Equal(t, 2, observations)
	// A new CALL/session resumes persisted progress, rather than inferring it
	// from the newest bucket or starting over after an interrupted worker.
	finishRecovery(t, r.db, []string{aggregate, "hashboard_metrics_daily"})
	runRawRetention(t, r.db, "hashboard_metrics")
}

func TestTelemetrySummaryHourlyGapAndRecentRevision(t *testing.T) {
	r := newContainerRollupRig(t)
	r.base = time.Now().UTC().Truncate(24 * time.Hour).Add(-3 * 24 * time.Hour)
	r.store(t, r.snapshot(0, false, 100e12, 200e12))
	recent := time.Now().UTC().Truncate(time.Hour).Add(-3 * time.Hour)
	r.store(t, r.snapshot(recent.Sub(r.base), false, 300e12, 400e12))
	reinstallTelemetryRecovery(t, r.db)
	_, err := r.db.ExecContext(t.Context(), `ALTER MATERIALIZED VIEW hashboard_metrics_hourly SET (timescaledb.materialized_only=true)`)
	require.NoError(t, err)
	start, end := r.base, time.Now().UTC().Truncate(24*time.Hour)
	testutil.RefreshContinuousAggregate(t, r.db, "hashboard_metrics_daily", &start, &end)
	var count int
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hashboard_metrics_daily WHERE bucket=$1`, r.base).Scan(&count))
	require.Equal(t, 2, count)
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hashboard_metrics_hourly WHERE bucket=$1`, r.base).Scan(&count))
	require.Zero(t, count)
	finishRecovery(t, r.db, []string{"hashboard_metrics_hourly"})
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hashboard_metrics_hourly WHERE bucket=$1`, r.base).Scan(&count))
	require.Equal(t, 2, count, "daily coverage must not hide an hourly-only gap")
	runRecentRecovery(t, r.db, "hashboard_metrics_hourly")
	var original float64
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT sum(hashrate_sum) FROM hashboard_metrics_hourly WHERE bucket=$1`, recent).Scan(&original))
	require.Equal(t, 700e12, original)
	_, err = r.db.ExecContext(t.Context(), `UPDATE hashboard_metrics SET hashrate_hs=500e12 WHERE time=$1`, recent)
	require.NoError(t, err)
	runRecentRecovery(t, r.db, "hashboard_metrics_hourly")
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT sum(hashrate_sum) FROM hashboard_metrics_hourly WHERE bucket=$1`, recent).Scan(&original))
	require.Equal(t, 1000e12, original, "recent invalidations must replace revised bucket values")
}

func TestTelemetryRawRetentionBoundsWholeChunkDeletion(t *testing.T) {
	db := testutil.GetTestDB(t)
	_, err := db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs)
	 SELECT t,'bounded-retention',100e12 FROM generate_series(date_trunc('day',now())-INTERVAL '20 days',
	 date_trunc('day',now())-INTERVAL '20 days'+INTERVAL '40 hours',INTERVAL '1 hour') t`)
	require.NoError(t, err)
	reinstallTelemetryRecovery(t, db)
	finishRecovery(t, db, deviceMetricRecoveryAggregates)
	var before, after int
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT count(*) FROM timescaledb_information.chunks WHERE hypertable_name='device_metrics'`).Scan(&before))
	require.Equal(t, 41, before)
	runRawRetention(t, db, "device_metrics")
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT count(*) FROM timescaledb_information.chunks WHERE hypertable_name='device_metrics'`).Scan(&after))
	require.Equal(t, 32, before-after)
	runRawRetention(t, db, "device_metrics")
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT count(*) FROM timescaledb_information.chunks WHERE hypertable_name='device_metrics'`).Scan(&after))
	require.Zero(t, after, "next run resumes with remaining whole chunks")
}

func TestTelemetrySummaryRecoveryKeepsUTCBucketsAcrossDST(t *testing.T) {
	r := newContainerRollupRig(t)
	r.base = time.Date(2026, time.March, 8, 0, 0, 0, 0, time.UTC)
	r.store(t, r.snapshot(0, false, 100e12, 200e12))
	r.store(t, r.snapshot(23*time.Hour, false, 300e12, 400e12))
	reinstallTelemetryRecovery(t, r.db)
	conn, err := r.db.Conn(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	_, err = conn.ExecContext(t.Context(), `SET TIME ZONE 'America/Toronto'`)
	require.NoError(t, err)
	selectRecoveryWork(t, r.db, "hashboard_metrics_daily", "catchup")
	_, err = conn.ExecContext(t.Context(), `CALL telemetry_summary_step('hashboard_metrics')`)
	require.NoError(t, err)
	completed := recoveryCompletedThrough(t, r.db, "hashboard_metrics_daily")
	require.WithinDuration(t, r.base.Add(24*time.Hour), completed, time.Microsecond, "a calendar day in the session timezone can be only 23 hours")
	var count int
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT sum(observations) FROM hashboard_metrics_daily WHERE bucket=$1`, r.base).Scan(&count))
	require.Equal(t, 4, count, "acknowledged coverage must include the entire UTC day")
}

func TestTelemetrySummaryUpgradePreservesExpiredAndPartialHistory(t *testing.T) {
	r := newContainerRollupRig(t)
	r.base = time.Now().UTC().Truncate(24 * time.Hour).Add(-20 * 24 * time.Hour)
	r.store(t, r.snapshot(time.Hour, false, 100e12, 200e12))
	start, end := r.base, r.base.Add(24*time.Hour)
	for _, aggregate := range []string{"device_metrics_daily", "container_metrics_daily"} {
		testutil.RefreshContinuousAggregate(t, r.db, aggregate, &start, &end)
	}
	_, err := r.db.ExecContext(t.Context(), `SELECT drop_chunks('device_metrics',older_than=>$1::timestamptz)`, end)
	require.NoError(t, err)
	// Only the final 23 hours of this day remain. Refreshing the whole daily
	// bucket would erase previously retained evidence from its first hour.
	r.store(t, r.snapshot(24*time.Hour, false, 100e12, 200e12))
	partial := r.base.Add(24*time.Hour + time.Hour)
	r.store(t, r.snapshot(partial.Sub(r.base), false, 200e12, 300e12))
	partialStart, partialEnd := r.base.Add(24*time.Hour), r.base.Add(48*time.Hour)
	for _, aggregate := range []string{"device_metrics_daily", "container_metrics_daily"} {
		testutil.RefreshContinuousAggregate(t, r.db, aggregate, &partialStart, &partialEnd)
	}
	_, err = r.db.ExecContext(t.Context(), `SELECT drop_chunks('device_metrics',older_than=>$1::timestamptz)`, partial)
	require.NoError(t, err)
	r.store(t, r.snapshot(48*time.Hour, false, 300e12, 400e12))
	// Reconstruct the pre-1027 scheduling state, then apply the real up file.
	removeTelemetryRecovery(t, r.db)
	_, err = r.db.ExecContext(t.Context(), `SELECT add_retention_policy(source::regclass,INTERVAL '10 days')
	 FROM unnest(ARRAY['device_metrics','container_tank_metrics','container_module_metrics','hashboard_metrics']) source;
	 SELECT alter_job(job_id,scheduled=>false) FROM timescaledb_information.jobs WHERE scheduled;`)
	require.NoError(t, err)
	_, err = r.db.ExecContext(t.Context(), telemetryRecoveryMigration(t))
	require.NoError(t, err)
	finishRecovery(t, r.db, deviceMetricRecoveryAggregates)
	runRawRetention(t, r.db, "device_metrics")
	for _, aggregate := range []string{"device_metrics_daily", "container_metrics_daily"} {
		var count int
		require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM `+aggregate+` WHERE bucket IN ($1,$2)`, start, partialStart).Scan(&count))
		require.Equal(t, 2, count, "expired/partial history must not be replaced: %s", aggregate)
	}
	var preserved float64
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT avg_hash_rate FROM device_metrics_daily WHERE bucket=$1 AND device_identifier=$2`, partialStart, r.deviceID).Scan(&preserved))
	require.Equal(t, 400e12, preserved, "refreshing only surviving raw would incorrectly replace this with 500 TH/s")
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT sum(hashrate_sum)/sum(hashrate_count) FROM container_metrics_daily WHERE bucket=$1`, partialStart).Scan(&preserved))
	require.Equal(t, 400e12, preserved)
	var pinned int
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT unverified_raw_chunks FROM telemetry_retention_health WHERE source_name='device_metrics'`).Scan(&pinned))
	require.Positive(t, pinned, "partial upgrade buckets retain their raw evidence for operator review")
	source := selectRecoveryWork(t, r.db, "device_metrics_daily", "recent")
	_, err = r.db.ExecContext(t.Context(), `CALL telemetry_summary_step($1)`, source)
	require.NoError(t, err, "routine replay must stay above expired raw")
}

func TestTelemetrySummaryUpgradePreservesHistoryAfterRawChunkRecreation(t *testing.T) {
	for _, tc := range []struct {
		name         string
		retention    string
		removePolicy bool
	}{{"ten_day_policy", "10 days", false}, {"shorter_policy", "2 days", false}, {"missing_policy", "10 days", true}} {
		t.Run(tc.name, func(t *testing.T) {
			r := newContainerRollupRig(t)
			r.base = time.Now().UTC().Truncate(24 * time.Hour).Add(-20 * 24 * time.Hour)
			r.store(t, r.snapshot(time.Hour, false, 100e12, 200e12))
			r.store(t, r.snapshot(49*time.Hour, false, 300e12, 400e12))
			start, end := r.base, r.base.Add(72*time.Hour)
			before := make(map[string]int)
			for _, aggregate := range recoveryAggregates {
				var beforeCount int
				testutil.RefreshContinuousAggregate(t, r.db, aggregate, &start, &end)
				require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM `+aggregate+` WHERE bucket>=$1 AND bucket<$2`, start, end).Scan(&beforeCount))
				before[aggregate] = beforeCount
				require.Positive(t, beforeCount, aggregate)
			}
			for _, source := range recoverySources {
				//nolint:unqueryvet // Copy the entire fixture row, including all keys and topology, for reinsertion after expiration.
				_, err := r.db.ExecContext(t.Context(), `CREATE TABLE upgrade_sample_`+source+` AS SELECT * FROM `+source+` WHERE time=$1 LIMIT 1`, r.base.Add(time.Hour))
				require.NoError(t, err)
			}
			removeTelemetryRecovery(t, r.db)
			for _, source := range recoverySources {
				var id int
				require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT add_retention_policy($1::regclass,$2::interval)`, source, tc.retention).Scan(&id))
				_, err := r.db.ExecContext(t.Context(), `SELECT alter_job($1,scheduled=>false)`, id)
				require.NoError(t, err)
				_, err = r.db.ExecContext(t.Context(), `CALL run_job($1)`, id)
				require.NoError(t, err)
				if tc.removePolicy {
					_, err = r.db.ExecContext(t.Context(), `SELECT remove_retention_policy($1::regclass)`, source)
					require.NoError(t, err)
				}
			}
			// A cached observation recreates the oldest expired chunks. The other
			// board and the entire second day's raw evidence remain absent.
			for _, source := range recoverySources {
				//nolint:gosec,unqueryvet // Reinsert complete fixtures; table names come only from the fixed recoverySources allowlist above.
				_, err := r.db.ExecContext(t.Context(), `INSERT INTO `+source+` SELECT * FROM upgrade_sample_`+source+`; DROP TABLE upgrade_sample_`+source)
				require.NoError(t, err)
				var recreated int
				require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM `+source+` WHERE time=$1`, r.base.Add(time.Hour)).Scan(&recreated))
				require.Equal(t, 1, recreated, source+" must actually recreate its expired chunk")
			}
			r.store(t, r.snapshot(time.Now().UTC().Add(-8*24*time.Hour).Sub(r.base), false, 500e12, 600e12))
			var earliestSafe time.Time
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT now()-$1::interval`, tc.retention).Scan(&earliestSafe))
			_, err := r.db.ExecContext(t.Context(), telemetryRecoveryMigration(t))
			require.NoError(t, err)
			var safe bool
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT bool_and(raw_floor>=$1)
 FROM telemetry_summary_source`, earliestSafe).Scan(&safe))
			finishRecovery(t, r.db, recoveryAggregates)
			for _, source := range recoverySources {
				runRawRetention(t, r.db, source)
			}
			for _, aggregate := range recoveryAggregates {
				var after int
				require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM `+aggregate+` WHERE bucket>=$1 AND bucket<$2`, start, end).Scan(&after))
				require.Equal(t, before[aggregate], after, "old summaries must survive incomplete/empty raw: %s", aggregate)
			}
			require.True(t, safe, "recreated old chunks cannot prove complete raw coverage")
			var original float64
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT sum(hashrate_sum) FROM hashboard_metrics_hourly WHERE bucket=$1`, r.base.Add(time.Hour)).Scan(&original))
			require.Equal(t, 300e12, original, "late subset must not replace original board history")
			var pinned int
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT sum(unverified_raw_chunks) FROM telemetry_retention_health`).Scan(&pinned))
			require.Positive(t, pinned, "uncertain recreated chunks remain for operator review")
		})
	}
}

func TestTelemetrySummaryFairRotationAndRecentDuringCatchup(t *testing.T) {
	r := newContainerRollupRig(t)
	r.base = time.Now().UTC().Truncate(24 * time.Hour).Add(-20 * 24 * time.Hour)
	r.store(t, r.snapshot(0, false, 100e12, 200e12))
	recent := time.Now().UTC().Truncate(time.Hour).Add(-3 * time.Hour)
	yesterday := time.Now().UTC().Truncate(24 * time.Hour).Add(-48 * time.Hour)
	r.store(t, r.snapshot(yesterday.Sub(r.base), false, 500e12, 600e12))
	r.store(t, r.snapshot(recent.Sub(r.base), false, 300e12, 400e12))
	reinstallTelemetryRecovery(t, r.db)
	for range 12 {
		_, err := r.db.ExecContext(t.Context(), `CALL telemetry_summary_step('device_metrics')`)
		require.NoError(t, err)
	}
	var successful int
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM telemetry_summary_work w JOIN telemetry_summary_progress p USING (aggregate_name)
 WHERE p.source_name='device_metrics' AND w.last_success_at IS NOT NULL`).Scan(&successful))
	require.Equal(t, 12, successful, "every tier and both kinds of work get a turn")
	for _, aggregate := range deviceMetricRecoveryAggregates {
		var count int
		at := recent
		if strings.HasSuffix(aggregate, "_daily") {
			at = yesterday
		}
		require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM `+aggregate+` WHERE bucket=$1`, at).Scan(&count))
		require.Positive(t, count, aggregate+" must show recent materialized data during catch-up")
		require.True(t, recoveryCompletedThrough(t, r.db, aggregate).Before(yesterday), "cold coverage is still behind")
	}
	// Retention shares this worker and gets a turn despite continuously due work.
	_, err := r.db.ExecContext(t.Context(), `UPDATE telemetry_summary_source SET next_retention_at=now() WHERE source_name='device_metrics'`)
	require.NoError(t, err)
	_, err = r.db.ExecContext(t.Context(), `CALL telemetry_summary_step('device_metrics')`)
	require.NoError(t, err)
	var deferrals int
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT retention_deferrals FROM telemetry_summary_source WHERE source_name='device_metrics'`).Scan(&deferrals))
	require.Positive(t, deferrals)
}

func TestTelemetrySummaryAdaptiveRetriesAndBlockedMinimum(t *testing.T) {
	r := newContainerRollupRig(t)
	r.base = time.Now().UTC().Truncate(24 * time.Hour).Add(-12 * 24 * time.Hour)
	r.store(t, r.snapshot(0, false, 100e12, 200e12))
	reinstallTelemetryRecovery(t, r.db)
	runRecovery(t, r.db, "hashboard_metrics_hourly")
	before := recoveryCompletedThrough(t, r.db, "hashboard_metrics_hourly")
	_, err := r.db.ExecContext(t.Context(), `ALTER MATERIALIZED VIEW hashboard_metrics_hourly RENAME TO unavailable_summary`)
	require.NoError(t, err)
	for _, expected := range []int{24, 12, 6, 3, 1, 1, 1} {
		source := selectRecoveryWork(t, r.db, "hashboard_metrics_hourly", "catchup")
		_, err = r.db.ExecContext(t.Context(), `CALL telemetry_summary_step($1)`, source)
		require.Error(t, err)
		var buckets int
		require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT batch_buckets FROM telemetry_summary_work WHERE aggregate_name='hashboard_metrics_hourly' AND work_kind='catchup'`).Scan(&buckets))
		require.Equal(t, expected, buckets)
		require.True(t, before.Equal(recoveryCompletedThrough(t, r.db, "hashboard_metrics_hourly")))
	}
	// A sibling records the last interruption and still makes progress.
	runRecovery(t, r.db, "hashboard_metrics_daily")
	var blocked bool
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT blocked FROM telemetry_summary_work WHERE aggregate_name='hashboard_metrics_hourly' AND work_kind='catchup'`).Scan(&blocked))
	require.True(t, blocked, "a minimum bucket must not retry forever")
	require.True(t, recoveryCompletedThrough(t, r.db, "hashboard_metrics_daily").After(r.base))
}

func TestTelemetrySummaryScheduledWorkerSerializesManualRequests(t *testing.T) {
	r := newContainerRollupRig(t)
	var capacity int
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT current_setting('timescaledb.max_background_workers')::integer-count(*)::integer
 FROM pg_stat_activity WHERE backend_type LIKE 'TimescaleDB Background Worker%' AND backend_type<>'TimescaleDB Background Worker Launcher'
 AND datname IS DISTINCT FROM current_database()`).Scan(&capacity))
	if capacity < 2 {
		t.Skip("scheduled integration needs a scheduler and a worker slot")
	}
	r.base = time.Now().UTC().Truncate(24 * time.Hour).Add(-12 * 24 * time.Hour)
	r.store(t, r.snapshot(0, false, 100e12, 200e12))
	reinstallTelemetryRecovery(t, r.db)
	selectRecoveryWork(t, r.db, "hashboard_metrics_hourly", "catchup")
	tx := lockSummaryMaterialization(t, r.db, "hashboard_metrics_hourly")
	id := recoveryJob(t, r.db, "hashboard_metrics")
	_, err := r.db.ExecContext(t.Context(), `SELECT alter_job($1,max_runtime=>INTERVAL '1 second',scheduled=>true,next_start=>now()+INTERVAL '1 day')`, id)
	require.NoError(t, err)
	_, err = r.db.ExecContext(t.Context(), `SELECT _timescaledb_functions.start_background_workers()`)
	require.NoError(t, err)
	_, err = r.db.ExecContext(t.Context(), `CALL run_job($1)`, id)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := r.db.ExecContext(context.Background(), `SELECT alter_job($1,scheduled=>false)`, id) //nolint:usetesting // Cleanup runs after the test context is canceled.
		require.NoError(t, err)
	})
	require.Eventually(t, func() bool {
		var pending bool
		err := r.db.QueryRowContext(t.Context(), `SELECT pending FROM telemetry_summary_work WHERE aggregate_name='hashboard_metrics_hourly' AND work_kind='catchup'`).Scan(&pending)
		return err == nil && pending
	}, 15*time.Second, 20*time.Millisecond, "scheduled worker must start the refresh")
	// Repeated foreground calls dispatch without doing refresh or retention.
	// Holding this connection idle must not pin the source after the worker exits.
	conn, err := r.db.Conn(t.Context())
	require.NoError(t, err)
	defer func() {
		_, err := conn.ExecContext(context.Background(), `RESET ROLE`) //nolint:usetesting // Reset pooled session state even after a failed assertion.
		require.NoError(t, err)
		require.NoError(t, conn.Close())
	}()
	// Membership allows job administration without inheriting the superuser's
	// visibility into other sessions, reproducing the hidden-role review case.
	role := fmt.Sprintf("telemetry_operator_%d", time.Now().UnixNano())
	var owner string
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT quote_ident(current_user)`).Scan(&owner))
	_, err = r.db.ExecContext(t.Context(), `CREATE ROLE `+role+`; GRANT `+owner+` TO `+role)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := r.db.ExecContext(context.Background(), `DROP ROLE `+role) //nolint:usetesting // Cleanup runs after the test context is canceled.
		require.NoError(t, err)
	})
	_, err = conn.ExecContext(t.Context(), `SET ROLE `+role)
	require.NoError(t, err)
	for range 3 {
		_, err = conn.ExecContext(t.Context(), `CALL run_job($1)`, id)
		require.NoError(t, err)
	}
	var attempts int
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT attempts FROM telemetry_summary_progress WHERE aggregate_name='hashboard_metrics_hourly'`).Scan(&attempts))
	require.Equal(t, 1, attempts)
	// Let the runtime limit terminate a real refresh, then release its lock. The next
	// worker must shrink the interrupted batch and allow a sibling to run.
	require.Eventually(t, func() bool {
		var failures int
		err := r.db.QueryRowContext(t.Context(), `SELECT total_failures FROM timescaledb_information.job_stats WHERE job_id=$1`, id).Scan(&failures)
		return err == nil && failures > 0
	}, 10*time.Second, 20*time.Millisecond)
	require.NoError(t, tx.Rollback())
	_, err = r.db.ExecContext(t.Context(), `SELECT alter_job($1,max_runtime=>INTERVAL '30 minutes')`, id)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		var failures int
		err := r.db.QueryRowContext(t.Context(), `SELECT failures FROM telemetry_summary_work WHERE aggregate_name='hashboard_metrics_hourly' AND work_kind='catchup'`).Scan(&failures)
		return err == nil && failures > 0
	}, 20*time.Second, 20*time.Millisecond)
	// Arrange a due sibling without bypassing the canonical scheduled job.
	selectRecoveryWork(t, r.db, "hashboard_metrics_daily", "catchup")
	_, err = conn.ExecContext(t.Context(), `CALL run_job($1)`, id)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		var success bool
		err := r.db.QueryRowContext(t.Context(), `SELECT last_success_at IS NOT NULL FROM telemetry_summary_work WHERE aggregate_name='hashboard_metrics_daily' AND work_kind='catchup'`).Scan(&success)
		return err == nil && success
	}, 15*time.Second, 20*time.Millisecond)
	// Once there is no due work, a dispatch should put the worker to sleep,
	// rather than continuing to poll at the catch-up frequency.
	_, err = r.db.ExecContext(t.Context(), `UPDATE telemetry_summary_work SET next_run_at=now()+INTERVAL '1 day';
 UPDATE telemetry_summary_source SET next_retention_at=now()+INTERVAL '1 day'`)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `CALL run_job($1)`, id)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		var next time.Time
		err := r.db.QueryRowContext(t.Context(), `SELECT next_start FROM timescaledb_information.job_stats WHERE job_id=$1`, id).Scan(&next)
		return err == nil && next.After(time.Now().Add(20*time.Minute))
	}, 20*time.Second, 20*time.Millisecond, "idle worker must sleep until the next useful task")
	_, err = conn.ExecContext(t.Context(), `RESET ROLE`)
	require.NoError(t, err)
}

func TestTelemetrySummaryPreservesStatusRetentionAndSkipsLateWrites(t *testing.T) {
	db := testutil.GetTestDB(t)
	var count int
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT count(*) FROM timescaledb_information.jobs WHERE proc_name='policy_retention'
 AND hypertable_name IN ('device_status_hourly','device_status_daily')`).Scan(&count))
	require.Zero(t, count, "migration must not add deletion of existing status history")
	runRecovery(t, db, "device_metrics_hourly")
	// A partial first UTC day can initialize daily coverage at tomorrow's
	// boundary. That empty interval is not a certified watermark for live writes.
	_, err := db.ExecContext(t.Context(), `UPDATE telemetry_summary_progress
 SET coverage_start=time_bucket(INTERVAL '24 hours',now())+INTERVAL '24 hours',
 completed_through=time_bucket(INTERVAL '24 hours',now())+INTERVAL '24 hours'
 WHERE source_name='device_metrics' AND bucket_width=INTERVAL '24 hours'`)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs) VALUES(now(),'live-miner',100)`)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `UPDATE telemetry_summary_write_gate SET frozen_through=now()-INTERVAL '24 hours' WHERE source_name='device_metrics'`)
	require.NoError(t, err)
	result, err := db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs) VALUES(now()-INTERVAL '40 days','late-miner',100) ON CONFLICT DO NOTHING`)
	require.NoError(t, err)
	inserted, err := result.RowsAffected()
	require.NoError(t, err)
	require.Zero(t, inserted, "stale rows must not abort their enclosing transaction")
	var jobs int
	var phases int
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT count(*),count(DISTINCT initial_start) FROM timescaledb_information.jobs WHERE proc_name='maintain_telemetry_source'`).Scan(&jobs, &phases))
	require.Equal(t, 4, jobs)
	require.Equal(t, 4, phases, "source jobs must start staggered")
}

func TestTelemetrySummaryRecoveryMigrationRollbackPreservesSafeguards(t *testing.T) {
	db := testutil.GetTestDB(t)
	down, err := migrations.Current.ReadFile("current/001027_internal_telemetry_summary_recovery.down.sql")
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), string(down))
	require.ErrorContains(t, err, "protects retained evidence")
	var count int
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT count(*) FROM telemetry_summary_progress`).Scan(&count))
	require.Len(t, recoveryAggregates, count)
	for _, source := range recoverySources {
		require.NoError(t, db.QueryRowContext(t.Context(), `SELECT count(*) FROM timescaledb_information.jobs WHERE proc_name='policy_retention' AND hypertable_name=$1`, source).Scan(&count))
		require.Zero(t, count, "unsafe raw policy restored for %s", source)
	}
}
