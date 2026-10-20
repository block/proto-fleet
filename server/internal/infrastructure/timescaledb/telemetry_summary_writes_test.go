package timescaledb

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestTelemetrySummaryStaleBoardDoesNotRollbackLivePoll(t *testing.T) {
	r := newContainerRollupRig(t)
	r.base = time.Now().UTC().Add(-49 * time.Hour).Truncate(time.Microsecond)
	r.store(t, r.snapshot(0, false, 100e12, 200e12))
	reinstallTelemetryRecovery(t, r.db)
	runRecovery(t, r.db, "hashboard_metrics_hourly")
	for _, offset := range []time.Duration{49 * time.Hour, 49*time.Hour + time.Second} {
		snapshot := r.snapshot(offset, false, 300e12, 400e12)
		board := &snapshot.Tanks[0].Modules[0].Hashboards[0]
		board.ObservedAt, board.StateObservedAt = r.base, r.base
		r.store(t, snapshot)
		var count int
		require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM device_metrics WHERE time=$1`, snapshot.ObservedAt).Scan(&count))
		require.Equal(t, 1, count, "a cached board must not roll back fresh parent readings")
		require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hashboard_metrics WHERE time=$1`, snapshot.ObservedAt).Scan(&count))
		require.Equal(t, 1, count, "the other board's fresh sample must survive")
	}
	var history int
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hashboard_metrics WHERE time=$1`, r.base).Scan(&history))
	require.Equal(t, 2, history, "the original observations must remain intact")
}

func TestTelemetrySummaryFinalizationWaitsForWriterCommit(t *testing.T) {
	db := testutil.GetTestDB(t)
	bucket := time.Now().UTC().Truncate(time.Hour).Add(-49 * time.Hour)
	_, err := db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs) VALUES($1,'initial',100)`, bucket)
	require.NoError(t, err)
	reinstallTelemetryRecovery(t, db)
	source := selectRecoveryWork(t, db, "device_metrics_hourly", "catchup")
	_, err = db.ExecContext(t.Context(), `UPDATE telemetry_summary_work SET batch_buckets=1 WHERE aggregate_name='device_metrics_hourly' AND work_kind='catchup'`)
	require.NoError(t, err)
	writer, err := db.Conn(t.Context())
	require.NoError(t, err)
	defer writer.Close()
	worker, err := db.Conn(t.Context())
	require.NoError(t, err)
	defer worker.Close()
	var writerPID, workerPID int
	require.NoError(t, writer.QueryRowContext(t.Context(), `SELECT pg_backend_pid()`).Scan(&writerPID))
	require.NoError(t, worker.QueryRowContext(t.Context(), `SELECT pg_backend_pid()`).Scan(&workerPID))
	tx, err := writer.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs) VALUES($1,'in-flight',200)`, bucket.Add(15*time.Minute))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := worker.ExecContext(ctx, `CALL telemetry_summary_step($1)`, source); result <- err }()
	require.Eventually(t, func() bool {
		var waiting bool
		err := db.QueryRowContext(t.Context(), `SELECT $1::integer=ANY(pg_blocking_pids($2::integer))`, writerPID, workerPID).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond, "finalization must wait for the writer, including a transaction begun before its horizon")
	var acknowledged sql.NullTime
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT completed_through FROM telemetry_summary_progress WHERE aggregate_name='device_metrics_hourly'`).Scan(&acknowledged))
	require.True(t, acknowledged.Valid)
	require.True(t, bucket.Equal(acknowledged.Time), "the checkpoint must not advance before the writer commits")
	require.NoError(t, tx.Commit())
	require.NoError(t, <-result)
	var value float64
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT avg_hash_rate FROM device_metrics_hourly WHERE device_identifier='in-flight' AND bucket=$1`, bucket).Scan(&value))
	require.Equal(t, 200.0, value, "the newly certified summary must include the committed writer")
	require.WithinDuration(t, bucket.Add(time.Hour), recoveryCompletedThrough(t, db, "device_metrics_hourly"), time.Microsecond)
	resultSQL, err := db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs) VALUES($1,'too-late',300)`, bucket)
	require.NoError(t, err)
	inserted, err := resultSQL.RowsAffected()
	require.NoError(t, err)
	require.Zero(t, inserted)
}

func TestTelemetrySummaryWriteFenceFollowsConfiguredLateness(t *testing.T) {
	db := testutil.GetTestDB(t)
	oldest := time.Now().UTC().Truncate(24 * time.Hour).Add(-5 * 24 * time.Hour)
	_, err := db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs) VALUES($1,'lateness-initial',100)`, oldest)
	require.NoError(t, err)
	reinstallTelemetryRecovery(t, db)
	_, err = db.ExecContext(t.Context(), `UPDATE telemetry_summary_progress SET lateness=INTERVAL '48 hours'
 WHERE source_name='device_metrics' AND bucket_width=INTERVAL '1 hour'`)
	require.NoError(t, err)
	finishRecovery(t, db, []string{"device_metrics_hourly"})
	for _, tc := range []struct {
		age  string
		want int64
	}{{"36 hours", 1}, {"60 hours", 0}} {
		result, err := db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs) VALUES(now()-$1::interval,'lateness-review',200)`, tc.age)
		require.NoError(t, err)
		n, err := result.RowsAffected()
		require.NoError(t, err)
		require.Equal(t, tc.want, n)
	}
}

func TestTelemetrySummaryPreflightFailureBlocksOnlyItsTask(t *testing.T) {
	db := testutil.GetTestDB(t)
	oldest := time.Now().UTC().Truncate(24 * time.Hour).Add(-20 * 24 * time.Hour)
	_, err := db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs) VALUES($1,'preflight',100)`, oldest)
	require.NoError(t, err)
	reinstallTelemetryRecovery(t, db)
	runRecovery(t, db, "device_metrics_hourly")
	source := selectRecoveryWork(t, db, "device_metrics_hourly", "catchup")
	_, err = db.ExecContext(t.Context(), `UPDATE telemetry_summary_source SET dropped_through=raw_floor+INTERVAL '48 hours',next_retention_at=now() WHERE source_name='device_metrics';
 UPDATE telemetry_summary_progress p SET completed_through=s.dropped_through FROM telemetry_summary_source s
 WHERE p.source_name=s.source_name AND p.source_name='device_metrics' AND p.aggregate_name<>'device_metrics_hourly'`)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `CALL telemetry_summary_step($1)`, source)
	require.NoError(t, err)
	var blocked bool
	var failures, deferrals int
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT blocked,failures FROM telemetry_summary_work WHERE aggregate_name='device_metrics_hourly' AND work_kind='catchup'`).Scan(&blocked, &failures))
	require.True(t, blocked)
	require.Equal(t, 1, failures)
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT retention_deferrals FROM telemetry_summary_source WHERE source_name='device_metrics'`).Scan(&deferrals))
	require.Positive(t, deferrals, "preflight failure must not roll back retention bookkeeping")
	runRecovery(t, db, "device_metrics_daily")
	require.True(t, recoveryCompletedThrough(t, db, "device_metrics_daily").After(oldest.Add(48*time.Hour)))
}

func TestTelemetrySummaryPausedJobStaysPaused(t *testing.T) {
	db := testutil.GetTestDB(t)
	id := recoveryJob(t, db, "device_metrics")
	_, err := db.ExecContext(t.Context(), `CALL run_job($1)`, id)
	require.ErrorContains(t, err, "telemetry maintenance is paused")
	var scheduled bool
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT scheduled FROM timescaledb_information.jobs WHERE job_id=$1`, id).Scan(&scheduled))
	require.False(t, scheduled)
}

func TestTelemetrySummaryBatchRecoversAfterFastSuccesses(t *testing.T) {
	db := testutil.GetTestDB(t)
	_, err := db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs) VALUES(now()-INTERVAL '20 days','adaptive',100)`)
	require.NoError(t, err)
	reinstallTelemetryRecovery(t, db)
	_, err = db.ExecContext(t.Context(), `ALTER MATERIALIZED VIEW device_metrics_hourly RENAME TO unavailable_hourly`)
	require.NoError(t, err)
	source := selectRecoveryWork(t, db, "device_metrics_hourly", "catchup")
	_, err = db.ExecContext(t.Context(), `CALL telemetry_summary_step($1)`, source)
	require.Error(t, err)
	_, err = db.ExecContext(t.Context(), `ALTER MATERIALIZED VIEW unavailable_hourly RENAME TO device_metrics_hourly`)
	require.NoError(t, err)
	for _, want := range []int{12, 12, 24} {
		runRecovery(t, db, "device_metrics_hourly")
		var buckets int
		require.NoError(t, db.QueryRowContext(t.Context(), `SELECT batch_buckets FROM telemetry_summary_work WHERE aggregate_name='device_metrics_hourly' AND work_kind='catchup'`).Scan(&buckets))
		require.Equal(t, want, buckets)
	}
}

func TestTelemetrySummaryBoundaryLookupRunsOncePerStatement(t *testing.T) {
	db := testutil.GetTestDB(t)
	conn, err := db.Conn(t.Context())
	require.NoError(t, err)
	defer conn.Close()
	_, err = conn.ExecContext(t.Context(), `SET track_functions='all'`)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs)
 SELECT now(),'statement-boundary-'||i,100 FROM generate_series(1,5000) i`)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `SELECT pg_stat_force_next_flush()`)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		_, err := conn.ExecContext(t.Context(), `SELECT pg_stat_clear_snapshot()`)
		if err != nil {
			return false
		}
		var calls int
		err = conn.QueryRowContext(t.Context(), `SELECT calls FROM pg_stat_user_functions WHERE funcname='lock_telemetry_summary_writes'`).Scan(&calls)
		return err == nil && calls == 1
	}, 3*time.Second, 20*time.Millisecond, "a 5000-row insert must look up and lock its boundary once")
}

func TestTelemetrySummaryImportExcludesConcurrentWriters(t *testing.T) {
	db := testutil.GetTestDB(t)
	importer, err := db.Conn(t.Context())
	require.NoError(t, err)
	defer importer.Close()
	_, err = importer.ExecContext(t.Context(), `SELECT pg_advisory_lock(1030,'device_metrics'::regclass::oid::integer)`)
	require.NoError(t, err)
	locked := true
	defer func() {
		if locked {
			_, _ = importer.ExecContext(context.Background(), `SELECT pg_advisory_unlock(1030,'device_metrics'::regclass::oid::integer)`) //nolint:usetesting // Release even after an assertion cancels the test context.
		}
	}()
	writer, err := db.Conn(t.Context())
	require.NoError(t, err)
	defer writer.Close()
	var importerPID, writerPID int
	require.NoError(t, importer.QueryRowContext(t.Context(), `SELECT pg_backend_pid()`).Scan(&importerPID))
	require.NoError(t, writer.QueryRowContext(t.Context(), `SELECT pg_backend_pid()`).Scan(&writerPID))
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := writer.ExecContext(ctx, `INSERT INTO device_metrics(time,device_identifier,hash_rate_hs) VALUES(now(),'import-waiter',100)`)
		result <- err
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := db.QueryRowContext(t.Context(), `SELECT $1::integer=ANY(pg_blocking_pids($2::integer))`, importerPID, writerPID).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond, "live writes must wait while an import reopens historical coverage")
	_, err = importer.ExecContext(t.Context(), `SELECT pg_advisory_unlock(1030,'device_metrics'::regclass::oid::integer)`)
	require.NoError(t, err)
	locked = false
	require.NoError(t, <-result)
}
