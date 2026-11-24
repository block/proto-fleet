package timescaledb

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/stretchr/testify/require"
)

func TestTopologyCompactionPreservesChangesGapsAndBatchBoundaries(t *testing.T) {
	for _, batch := range []int{1, 3, 100} {
		r := newContainerRollupRig(t)
		base := time.Now().UTC().Truncate(time.Second).Add(-20 * 24 * time.Hour)
		versions := make(map[string]int64)
		for _, name := range []string{"A", "B", "incomplete"} {
			var id int64
			require.NoError(t, r.db.QueryRowContext(t.Context(), `INSERT INTO container_topology_version(device_id,fingerprint,topology) VALUES ($1,$2,'{}') RETURNING id`, r.databaseID, name).Scan(&id))
			versions[name] = id
		}
		for _, point := range []struct {
			seconds int
			version string
		}{{0, "A"}, {60, "A"}, {180, "A"}, {301, "A"}, {310, "B"}, {320, "A"}, {330, "incomplete"}, {340, "incomplete"}, {350, "A"}} {
			_, err := r.db.ExecContext(t.Context(), `INSERT INTO container_topology_observation(device_id,version_id,collected_at,complete) VALUES ($1,$2,$3,$4)`, r.databaseID, versions[point.version], base.Add(time.Duration(point.seconds)*time.Second), point.version != "incomplete")
			require.NoError(t, err)
		}
		for range 12 {
			_, err := r.db.ExecContext(t.Context(), `SELECT compact_container_topology($1,now()-INTERVAL '10 days',$2)`, r.databaseID, batch)
			require.NoError(t, err)
		}
		// Every batch size has drained the input and run empty batches. Progress
		// must still point to the final observation and its surviving anchor.
		var scannedThrough, anchorAt time.Time
		require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT s.scanned_through,o.collected_at
		 FROM container_topology_compaction_state s JOIN container_topology_observation o ON o.id=s.anchor_id
		 WHERE s.device_id=$1`, r.databaseID).Scan(&scannedThrough, &anchorAt))
		require.True(t, scannedThrough.Equal(base.Add(350*time.Second)))
		require.True(t, anchorAt.Equal(base.Add(350*time.Second)))
		rows, err := r.db.QueryContext(t.Context(), `SELECT collected_at,COALESCE(last_confirmed_at,collected_at) FROM container_topology_observation WHERE device_id=$1 ORDER BY collected_at`, r.databaseID)
		require.NoError(t, err)
		var spans [][2]int
		for rows.Next() {
			var start, end time.Time
			require.NoError(t, rows.Scan(&start, &end))
			spans = append(spans, [2]int{int(start.Sub(base).Seconds()), int(end.Sub(base).Seconds())})
		}
		require.NoError(t, rows.Err())
		defer rows.Close()
		require.Equal(t, [][2]int{{0, 180}, {301, 301}, {310, 310}, {320, 320}, {330, 340}, {350, 350}}, spans, "batch size %d", batch)
	}
}

func TestTopologyCompactionRetainsAndRechecksLogicalReferences(t *testing.T) {
	r := newContainerRollupRig(t)
	r.base = time.Now().UTC().Truncate(time.Second).Add(-20 * 24 * time.Hour)
	for _, offset := range []time.Duration{0, time.Minute, 2 * time.Minute} {
		r.store(t, r.snapshot(offset, false, 100e12, 200e12))
	}
	// Exercise the logical lookup against columnstore data, not only row indexes.
	_, err := r.db.ExecContext(t.Context(), `SELECT compress_chunk(chunk,if_not_compressed=>true) FROM show_chunks('device_metrics') chunk`)
	require.NoError(t, err)
	// Tank rows have FKs; device_metrics intentionally has no observation FK.
	_, err = r.db.ExecContext(t.Context(), `SELECT drop_chunks('container_tank_metrics',older_than=>now()-INTERVAL '10 days')`)
	require.NoError(t, err)
	_, err = r.db.ExecContext(t.Context(), `SELECT compact_container_topology($1,now()-INTERVAL '10 days',100)`, r.databaseID)
	require.NoError(t, err)
	var count int
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM container_topology_observation WHERE device_id=$1`, r.databaseID).Scan(&count))
	require.Equal(t, 3, count, "logical references must pin interior receipts")
	var end sql.NullTime
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT last_confirmed_at FROM container_topology_observation WHERE device_id=$1 ORDER BY collected_at LIMIT 1`, r.databaseID).Scan(&end))
	require.True(t, end.Valid)
	require.True(t, end.Time.Equal(r.base.Add(2*time.Minute)))
	_, err = r.db.ExecContext(t.Context(), `SELECT drop_chunks('device_metrics',older_than=>now()-INTERVAL '10 days')`)
	require.NoError(t, err)
	_, err = r.db.ExecContext(t.Context(), `UPDATE container_topology_compaction_pending SET next_check_at=now()`)
	require.NoError(t, err)
	// No new candidates remain, but retry work exceeds this batch. Drain it without
	// waiting an hour, and stop immediately rescheduling once only future pins remain.
	for remaining := 1; remaining >= 0; remaining-- {
		_, err = r.db.ExecContext(t.Context(), `SELECT compact_container_topology($1,now()-INTERVAL '10 days',1)`, r.databaseID)
		require.NoError(t, err)
		var due int
		var ready bool
		require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM container_topology_compaction_pending WHERE next_check_at<=now()`).Scan(&due))
		require.Equal(t, remaining, due)
		require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT next_run_at<=now() FROM container_topology_compaction_state WHERE device_id=$1`, r.databaseID).Scan(&ready))
		require.Equal(t, remaining > 0, ready)
	}
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM container_topology_observation WHERE device_id=$1`, r.databaseID).Scan(&count))
	require.Equal(t, 2, count, "only the anchor and latest-value reference should survive")
}

func TestTopologyCompactionRejectsUnboundedRequests(t *testing.T) {
	r := newContainerRollupRig(t)
	for _, query := range []string{
		"SELECT * FROM container_topology_candidates($1,$2,$3)",
		"SELECT * FROM compact_container_topology($1,$2,$3)",
	} {
		cutoff := time.Now().UTC().Add(-10 * 24 * time.Hour)
		for _, input := range []struct{ cutoff, limit any }{{cutoff, nil}, {nil, 1}, {time.Now(), 1}, {cutoff, 0}} {
			_, err := r.db.ExecContext(t.Context(), query, r.databaseID, input.cutoff, input.limit)
			require.ErrorContains(t, err, "cleanup requires")
		}
	}
}

func TestTopologyCompactionDryRunAndRollback(t *testing.T) {
	r := newContainerRollupRig(t)
	r.base = time.Now().UTC().Truncate(time.Second).Add(-20 * 24 * time.Hour)
	r.store(t, r.snapshot(0, false, 100e12, 200e12))
	r.store(t, r.snapshot(time.Minute, false, 100e12, 200e12))
	var candidates, receipts int
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM container_topology_candidates($1,now()-INTERVAL '10 days',100)`, r.databaseID).Scan(&candidates))
	require.Equal(t, 2, candidates)
	var cursor sql.NullTime
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT scanned_through FROM container_topology_compaction_state WHERE device_id=$1`, r.databaseID).Scan(&cursor))
	require.False(t, cursor.Valid)
	tx, err := r.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(t.Context(), `SELECT compact_container_topology($1,now()-INTERVAL '10 days',100)`, r.databaseID)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM container_topology_observation WHERE last_confirmed_at IS NOT NULL`).Scan(&receipts))
	require.Zero(t, receipts)
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT scanned_through FROM container_topology_compaction_state WHERE device_id=$1`, r.databaseID).Scan(&cursor))
	require.False(t, cursor.Valid)
}

func TestTopologyCompactionSchedulerPersistsLeaseOnCancellation(t *testing.T) {
	for _, scenario := range []struct {
		name           string
		microbatchSize int
		cancelAt       int
		wantExamined   int
		wantCursor     bool
		storedEvidence bool
		wantPending    int
	}{
		{"first_microbatch", 3, 1, 0, false, false, 0},
		{"later_microbatch", 3, 2, 3, true, false, 0},
		{"configured_microbatch", 5, 2, 5, true, false, 0},
		{"referenced_microbatch", 3, 2, 3, true, true, 2},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			r := newContainerRollupRig(t)
			if scenario.storedEvidence {
				r.base = time.Now().UTC().Truncate(time.Second).Add(-20 * 24 * time.Hour)
				for n := range 10 {
					r.store(t, r.snapshot(time.Duration(n)*time.Second, false, 100e12, 200e12))
				}
			} else {
				insertTopologyCleanupCandidates(t, r, "cancel-"+scenario.name, 30)
			}
			// Query cancellation deliberately escapes the exception handler, as a
			// worker runtime limit would. The sequence survives the rollback.
			_, err := r.db.ExecContext(t.Context(), `CREATE SEQUENCE cancel_compaction_test_sequence;
 CREATE TABLE cancel_compaction_test_state(cancel_at bigint NOT NULL);
 CREATE FUNCTION cancel_compaction_test() RETURNS trigger LANGUAGE plpgsql AS $$
 DECLARE attempt bigint;
 BEGIN
  attempt:=nextval('cancel_compaction_test_sequence');
  IF attempt=(SELECT cancel_at FROM cancel_compaction_test_state) THEN
   RAISE EXCEPTION 'test cancellation' USING ERRCODE='57014';
  END IF;
  RETURN NEW;
 END $$;
 CREATE TRIGGER cancel_compaction_test BEFORE UPDATE OF scanned_through ON container_topology_compaction_state
 FOR EACH ROW EXECUTE FUNCTION cancel_compaction_test()`)
			require.NoError(t, err)
			_, err = r.db.ExecContext(t.Context(), `INSERT INTO cancel_compaction_test_state(cancel_at) VALUES($1)`, scenario.cancelAt)
			require.NoError(t, err)
			var job int
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT job_id FROM timescaledb_information.jobs WHERE proc_name='compact_container_topology_job'`).Scan(&job))
			_, err = r.db.ExecContext(t.Context(), `SELECT alter_job($1,config=>jsonb_build_object(
 'enabled',true,'batch_size',25,'microbatch_size',$2::int))`, job, scenario.microbatchSize)
			require.NoError(t, err)
			_, err = r.db.ExecContext(t.Context(), `CALL run_job($1)`, job)
			require.ErrorContains(t, err, "test cancellation")
			var examined int
			var leased bool
			var cursor sql.NullTime
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT examined,next_run_at>now(),scanned_through
 FROM container_topology_compaction_state WHERE device_id=$1`, r.databaseID).Scan(&examined, &leased, &cursor))
			require.Equal(t, scenario.wantExamined, examined)
			require.Equal(t, scenario.wantCursor, cursor.Valid)
			require.True(t, leased, "cancelled batch must leave scheduling backoff committed")
			var pending int
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM container_topology_compaction_pending WHERE device_id=$1`, r.databaseID).Scan(&pending))
			require.Equal(t, scenario.wantPending, pending, "committed microbatches must preserve pinned evidence across cancellation")
			// The durable lease must prevent an immediate retry of the same device.
			_, err = r.db.ExecContext(t.Context(), `CALL run_job($1)`, job)
			require.NoError(t, err)
			var examinedAfterRetry int
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT examined FROM container_topology_compaction_state WHERE device_id=$1`, r.databaseID).Scan(&examinedAfterRetry))
			require.Equal(t, examined, examinedAfterRetry)
		})
	}
}

func insertTopologyCleanupCandidates(t *testing.T, r *containerRollupRig, fingerprint string, count int) {
	t.Helper()
	var version int64
	require.NoError(t, r.db.QueryRowContext(t.Context(), `INSERT INTO container_topology_version(device_id,fingerprint,topology)
 VALUES ($1,$2,'{}') RETURNING id`, r.databaseID, fingerprint).Scan(&version))
	_, err := r.db.ExecContext(t.Context(), `INSERT INTO container_topology_observation(device_id,version_id,collected_at,complete)
 SELECT $1,$2,$3::timestamptz+n*INTERVAL '1 minute',true FROM generate_series(0,$4::integer-1) n`,
		r.databaseID, version, time.Now().UTC().Truncate(time.Second).Add(-20*24*time.Hour), count)
	require.NoError(t, err)
}

func TestTopologyCompactionSchedulerPreservesBudgetAcrossMicrobatches(t *testing.T) {
	r := newContainerRollupRig(t)
	insertTopologyCleanupCandidates(t, r, "microbatch-budget", 30)
	_, err := r.db.ExecContext(t.Context(), `CALL compact_container_topology_job(0,'{"enabled":true,"batch_size":25}'::jsonb)`)
	require.NoError(t, err)
	var examined, deleted, failures int
	var due bool
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT examined,deleted,failures,next_run_at<=now()
 FROM container_topology_compaction_state WHERE device_id=$1`, r.databaseID).Scan(&examined, &deleted, &failures, &due))
	require.Equal(t, 25, examined)
	require.Equal(t, 24, deleted)
	require.Zero(t, failures)
	require.True(t, due, "a full scheduled budget must leave the remaining backlog ready")
}

func TestTopologyCompactionSchedulerReleasesIngestionLockBetweenMicrobatches(t *testing.T) {
	r := newContainerRollupRig(t)
	insertTopologyCleanupCandidates(t, r, "lock-handoff", 26)
	_, err := r.db.ExecContext(t.Context(), `CREATE FUNCTION slow_topology_cleanup_delete_test() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN
  PERFORM pg_advisory_xact_lock(234,234);
  PERFORM pg_sleep(0.04);
  RETURN OLD;
 END $$;
 CREATE TRIGGER slow_topology_cleanup_delete_test BEFORE DELETE ON container_topology_observation
 FOR EACH ROW EXECUTE FUNCTION slow_topology_cleanup_delete_test()`)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	waitForCall := func(done <-chan error) {
		t.Helper()
		select {
		case callErr := <-done:
			require.NoError(t, callErr)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	gate, err := r.db.Conn(ctx)
	require.NoError(t, err)
	defer gate.Close()
	_, err = gate.ExecContext(ctx, `SELECT pg_advisory_lock(234,234)`)
	require.NoError(t, err)

	cleanup, err := r.db.Conn(ctx)
	require.NoError(t, err)
	defer cleanup.Close()
	var cleanupPID int
	require.NoError(t, cleanup.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&cleanupPID))
	cleanupDone := make(chan error, 1)
	go func() {
		_, callErr := cleanup.ExecContext(ctx, `CALL compact_container_topology_job(0,'{"enabled":true,"batch_size":25}'::jsonb)`)
		cleanupDone <- callErr
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		queryErr := r.db.QueryRowContext(ctx, `SELECT wait_event='advisory' FROM pg_stat_activity WHERE pid=$1`, cleanupPID).Scan(&waiting)
		return queryErr == nil && waiting
	}, 5*time.Second, 10*time.Millisecond, "cleanup did not reach the controlled delete")

	ingestion, err := r.db.Conn(ctx)
	require.NoError(t, err)
	defer ingestion.Close()
	ingestionTx, err := ingestion.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = ingestionTx.Rollback() }()
	_, err = ingestionTx.ExecContext(ctx, `SET LOCAL lock_timeout='250ms'`)
	require.NoError(t, err)
	var ingestionPID int
	require.NoError(t, ingestionTx.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&ingestionPID))
	ingestionDone := make(chan error, 1)
	go func() {
		_, lockErr := sqlc.New(ingestionTx).LockHashboardOrganization(ctx, r.deviceID)
		ingestionDone <- lockErr
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		queryErr := r.db.QueryRowContext(ctx, `SELECT wait_event_type='Lock' FROM pg_stat_activity WHERE pid=$1`, ingestionPID).Scan(&waiting)
		return queryErr == nil && waiting
	}, 5*time.Second, 10*time.Millisecond, "ingestion did not wait for the cleanup organization lock")

	_, err = gate.ExecContext(ctx, `SELECT pg_advisory_unlock(234,234)`)
	require.NoError(t, err)
	waitForCall(ingestionDone)
	waitForCall(cleanupDone)
	var examined, failures, deferrals int
	var due bool
	var lastDeferredAt sql.NullTime
	require.NoError(t, r.db.QueryRowContext(ctx, `SELECT examined,failures,next_run_at<=now(),deferrals,last_deferred_at
 FROM container_topology_compaction_state WHERE device_id=$1`, r.databaseID).Scan(&examined, &failures, &due, &deferrals, &lastDeferredAt))
	require.Positive(t, examined)
	require.Less(t, examined, 25, "ingestion must interrupt cleanup before the full scheduled batch")
	require.Zero(t, failures)
	require.True(t, due, "a device that made progress must remain due after lock contention")
	require.Equal(t, 1, deferrals)
	require.True(t, lastDeferredAt.Valid)
	require.NoError(t, ingestionTx.Commit())
	_, err = r.db.ExecContext(ctx, `CALL compact_container_topology_job(0,'{"enabled":true,"batch_size":3}'::jsonb)`)
	require.NoError(t, err)
	var examinedAfterRetry int
	require.NoError(t, r.db.QueryRowContext(ctx, `SELECT examined FROM container_topology_compaction_state WHERE device_id=$1`, r.databaseID).Scan(&examinedAfterRetry))
	require.Equal(t, examined+3, examinedAfterRetry, "progress-aware contention must allow an immediate retry")
}

func TestTopologyCompactionSchedulerBoundsForeignKeyLockWait(t *testing.T) {
	r := newContainerRollupRig(t)
	r.base = time.Now().UTC().Truncate(time.Second).Add(-20 * 24 * time.Hour)
	for _, offset := range []time.Duration{0, time.Minute, 2 * time.Minute} {
		r.store(t, r.snapshot(offset, false, 100e12, 200e12))
	}
	_, err := r.db.ExecContext(t.Context(), `SELECT drop_chunks('device_metrics',older_than=>now()-INTERVAL '10 days');
 SELECT drop_chunks('container_tank_metrics',older_than=>now()-INTERVAL '10 days')`)
	require.NoError(t, err)
	var blockedObservation int64
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT id FROM container_topology_observation
 WHERE device_id=$1 AND collected_at=$2`, r.databaseID, r.base.Add(time.Minute)).Scan(&blockedObservation))

	// Reproduce the FK row-lock condition possible during an in-flight latest-value
	// update. The update is invisible to the precheck, but it blocks parent delete.
	blocker, err := r.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = blocker.Rollback() })
	_, err = blocker.ExecContext(t.Context(), `UPDATE container_tank_latest_metrics
 SET topology_observation_id=$1`, blockedObservation)
	require.NoError(t, err)

	started := time.Now()
	_, err = r.db.ExecContext(t.Context(), `CALL compact_container_topology_job(0,
  '{"enabled":true,"batch_size":25,"microbatch_size":3,"lock_timeout_ms":10000}'::jsonb)`)
	require.NoError(t, err)
	require.Less(t, time.Since(started), time.Second, "an FK lock wait must not consume the two-second job deadline")
	var examined, failures, deferrals int
	var deferred bool
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT examined,failures,deferrals,next_run_at>now()
 FROM container_topology_compaction_state WHERE device_id=$1`, r.databaseID).Scan(&examined, &failures, &deferrals, &deferred))
	require.Zero(t, examined, "the timed-out microbatch must roll back atomically")
	require.Zero(t, failures)
	require.Equal(t, 1, deferrals)
	require.True(t, deferred, "a first-microbatch lock wait must keep the one-minute backoff")

	require.NoError(t, blocker.Rollback())
	_, err = r.db.ExecContext(t.Context(), `UPDATE container_topology_compaction_state SET next_run_at=now() WHERE device_id=$1`, r.databaseID)
	require.NoError(t, err)
	_, err = r.db.ExecContext(t.Context(), `CALL compact_container_topology_job(0,
  '{"enabled":true,"batch_size":3,"microbatch_size":3}'::jsonb)`)
	require.NoError(t, err)
	var retained bool
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT EXISTS(
  SELECT 1 FROM container_topology_observation WHERE id=$1)`, blockedObservation).Scan(&retained))
	require.False(t, retained, "cleanup must retry the deferred observation after the writer releases its lock")
}

func TestTopologyCompactionSchedulerYieldsBeforeJobDeadline(t *testing.T) {
	r := newContainerRollupRig(t)
	insertTopologyCleanupCandidates(t, r, "runtime-budget", 30)
	_, err := r.db.ExecContext(t.Context(), `CREATE FUNCTION slow_topology_cleanup_runtime_test() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN
  PERFORM pg_sleep(0.15);
  RETURN OLD;
 END $$;
 CREATE TRIGGER slow_topology_cleanup_runtime_test BEFORE DELETE ON container_topology_observation
 FOR EACH ROW EXECUTE FUNCTION slow_topology_cleanup_runtime_test()`)
	require.NoError(t, err)

	conn, err := r.db.Conn(t.Context())
	require.NoError(t, err)
	defer conn.Close()
	_, err = conn.ExecContext(t.Context(), `SET statement_timeout='2s'`)
	require.NoError(t, err)
	started := time.Now()
	_, err = conn.ExecContext(t.Context(), `CALL compact_container_topology_job(0,
  '{"enabled":true,"batch_size":25,"microbatch_size":1,"run_budget_ms":10000}'::jsonb)`)
	require.NoError(t, err, "the cleanup must yield before the scheduler deadline")
	require.Less(t, time.Since(started), 1800*time.Millisecond)

	var examined, failures, deferrals, runtimeYields int
	var due bool
	var lastRuntimeYield sql.NullTime
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT examined,failures,deferrals,runtime_yields,
 next_run_at<=now(),last_runtime_yield_at FROM container_topology_compaction_state WHERE device_id=$1`, r.databaseID).
		Scan(&examined, &failures, &deferrals, &runtimeYields, &due, &lastRuntimeYield))
	require.Positive(t, examined)
	require.Less(t, examined, 25, "the runtime guard must stop before the configured row budget")
	require.Zero(t, failures)
	require.Zero(t, deferrals)
	require.Equal(t, 1, runtimeYields)
	require.True(t, due, "runtime-yielded work must remain due behind older controllers")
	require.True(t, lastRuntimeYield.Valid)

	_, err = r.db.ExecContext(t.Context(), `DROP TRIGGER slow_topology_cleanup_runtime_test ON container_topology_observation;
 DROP FUNCTION slow_topology_cleanup_runtime_test()`)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `CALL compact_container_topology_job(0,
  '{"enabled":true,"batch_size":25,"microbatch_size":3}'::jsonb)`)
	require.NoError(t, err)
	var examinedAfterRetry int
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT examined FROM container_topology_compaction_state
 WHERE device_id=$1`, r.databaseID).Scan(&examinedAfterRetry))
	require.Greater(t, examinedAfterRetry, examined, "a yielded device must make progress on its next turn")
}

func TestTopologyCompactionSchedulerRechecksRuntimeAfterHandoff(t *testing.T) {
	r := newContainerRollupRig(t)
	insertTopologyCleanupCandidates(t, r, "handoff-runtime-budget", 9)
	_, err := r.db.ExecContext(t.Context(), `CREATE FUNCTION slow_topology_cleanup_handoff_test() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN
  IF OLD.last_run_at IS NOT NULL
   AND NEW.last_run_at IS NOT DISTINCT FROM OLD.last_run_at
   AND NEW.runtime_yields=OLD.runtime_yields
   AND NEW.next_run_at>OLD.next_run_at THEN
   PERFORM pg_sleep(1.1);
  END IF;
  RETURN NEW;
 END $$;
 CREATE TRIGGER slow_topology_cleanup_handoff_test BEFORE UPDATE ON container_topology_compaction_state
 FOR EACH ROW EXECUTE FUNCTION slow_topology_cleanup_handoff_test()`)
	require.NoError(t, err)

	_, err = r.db.ExecContext(t.Context(), `CALL compact_container_topology_job(0,
  '{"enabled":true,"batch_size":6,"microbatch_size":3,"run_budget_ms":1000}'::jsonb)`)
	require.NoError(t, err)

	var examined, failures, runtimeYields int
	var due bool
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT examined,failures,runtime_yields,next_run_at<=now()
 FROM container_topology_compaction_state WHERE device_id=$1`, r.databaseID).
		Scan(&examined, &failures, &runtimeYields, &due))
	require.Equal(t, 3, examined, "cleanup must not start another microbatch after a slow handoff crosses the budget")
	require.Zero(t, failures)
	require.Equal(t, 1, runtimeYields)
	require.True(t, due, "handoff-yielded work must remain due")
}

func TestTopologyCompactionSkipsActiveIngestionAndRecentReceipts(t *testing.T) {
	r := newContainerRollupRig(t)
	r.base = time.Now().UTC().Truncate(time.Second).Add(-20 * 24 * time.Hour)
	r.store(t, r.snapshot(0, false, 100e12, 200e12))
	r.store(t, r.snapshot(time.Minute, false, 100e12, 200e12))
	r.store(t, r.snapshot(20*24*time.Hour, false, 100e12, 200e12))
	tx, err := r.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	_, err = tx.ExecContext(t.Context(), `SELECT o.id FROM organization o JOIN device d ON d.org_id=o.id WHERE d.id=$1 FOR UPDATE OF o`, r.databaseID)
	require.NoError(t, err)
	_, err = r.db.ExecContext(t.Context(), `SELECT compact_container_topology($1,now()-INTERVAL '10 days',100)`, r.databaseID)
	require.ErrorContains(t, err, "could not obtain lock")
	// The scheduler treats this expected contention as a deferred attempt, not a
	// failure, while preserving the same no-progress guarantee.
	_, err = r.db.ExecContext(t.Context(), `CALL compact_container_topology_job(0,'{"enabled":true,"batch_size":100}'::jsonb)`)
	require.NoError(t, err)
	var failures, deferrals int
	var deferred bool
	var lastError sql.NullString
	var lastDeferredAt sql.NullTime
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT failures,last_error,next_run_at>now(),deferrals,last_deferred_at
 FROM container_topology_compaction_state WHERE device_id=$1`, r.databaseID).Scan(&failures, &lastError, &deferred, &deferrals, &lastDeferredAt))
	require.Zero(t, failures)
	require.False(t, lastError.Valid)
	require.True(t, deferred, "a device that made no progress must keep the one-minute backoff")
	require.Equal(t, 1, deferrals)
	require.True(t, lastDeferredAt.Valid)

	require.NoError(t, tx.Rollback())
	_, err = r.db.ExecContext(t.Context(), `SELECT compact_container_topology($1,now()-INTERVAL '10 days',100)`, r.databaseID)
	require.NoError(t, err)
	var recent int
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM container_topology_observation WHERE collected_at>now()-INTERVAL '10 days' AND last_confirmed_at IS NULL`).Scan(&recent))
	require.Equal(t, 1, recent)
	migration, err := migrations.Current.ReadFile("current/001022_internal_topology_compaction.down.sql")
	require.NoError(t, err)
	_, err = r.db.ExecContext(t.Context(), string(migration))
	require.ErrorContains(t, err, "topology has been compacted")
	var spans int
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM container_topology_observation WHERE last_confirmed_at>collected_at`).Scan(&spans))
	require.Equal(t, 1, spans, "rejected rollback must preserve evidence")
}

func TestTopologyCompactionBoundsStableBacklog(t *testing.T) {
	r := newContainerRollupRig(t)
	base := time.Now().UTC().Truncate(time.Second).Add(-20 * 24 * time.Hour)
	var version int64
	require.NoError(t, r.db.QueryRowContext(t.Context(), `INSERT INTO container_topology_version(device_id,fingerprint,topology) VALUES ($1,'stable-backlog','{}') RETURNING id`, r.databaseID).Scan(&version))
	_, err := r.db.ExecContext(t.Context(), `INSERT INTO container_topology_observation(device_id,version_id,collected_at,complete) SELECT $1,$2,$3::timestamptz+n*INTERVAL '15 seconds',true FROM generate_series(0,9999) n`, r.databaseID, version, base)
	require.NoError(t, err)
	for n := range 20 {
		_, err = r.db.ExecContext(t.Context(), `SELECT compact_container_topology($1,now()-INTERVAL '10 days',500)`, r.databaseID)
		require.NoError(t, err)
		var examined, remaining int
		var duration float64
		require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT examined,last_duration_ms FROM container_topology_compaction_state WHERE device_id=$1`, r.databaseID).Scan(&examined, &duration))
		require.Equal(t, (n+1)*500, examined)
		require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM container_topology_observation WHERE device_id=$1`, r.databaseID).Scan(&remaining))
		require.Equal(t, 10000-examined+1, remaining)
		if n == 0 {
			t.Logf("500-observation batch: %.1f ms (development measurement)", duration)
		}
	}
	var end time.Time
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT last_confirmed_at FROM container_topology_observation WHERE device_id=$1`, r.databaseID).Scan(&end))
	require.True(t, end.Equal(base.Add(9999*15*time.Second)))
}
