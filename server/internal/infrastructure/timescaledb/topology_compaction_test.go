package timescaledb

import (
	"database/sql"
	"os"
	"testing"
	"time"

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
	r := newContainerRollupRig(t)
	r.base = time.Now().UTC().Truncate(time.Second).Add(-20 * 24 * time.Hour)
	r.store(t, r.snapshot(0, false, 100e12, 200e12))
	// Force cancellation after the scheduling commit. Query cancellation deliberately
	// escapes the exception handler, as a worker runtime limit would.
	_, err := r.db.ExecContext(t.Context(), `CREATE FUNCTION cancel_compaction_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test cancellation' USING ERRCODE='57014'; END $$;
 CREATE TRIGGER cancel_compaction_test BEFORE UPDATE OF scanned_through ON container_topology_compaction_state FOR EACH ROW EXECUTE FUNCTION cancel_compaction_test()`)
	require.NoError(t, err)
	var job int
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT job_id FROM timescaledb_information.jobs WHERE proc_name='compact_container_topology_job'`).Scan(&job))
	_, err = r.db.ExecContext(t.Context(), `SELECT alter_job($1,config=>' {"enabled":true,"batch_size":100}'::jsonb)`, job)
	require.NoError(t, err)
	_, err = r.db.ExecContext(t.Context(), `CALL run_job($1)`, job)
	require.ErrorContains(t, err, "test cancellation")
	var leased bool
	var cursor sql.NullTime
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT next_run_at>now(),scanned_through FROM container_topology_compaction_state WHERE device_id=$1`, r.databaseID).Scan(&leased, &cursor))
	require.True(t, leased, "cancelled batch must leave scheduling backoff committed")
	require.False(t, cursor.Valid, "destructive progress must roll back")
	// It must not immediately pick the failing device again.
	_, err = r.db.ExecContext(t.Context(), `CALL run_job($1)`, job)
	require.NoError(t, err)
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
	var failures int
	var deferred bool
	var lastError sql.NullString
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT failures,last_error,next_run_at>now() FROM container_topology_compaction_state WHERE device_id=$1`, r.databaseID).Scan(&failures, &lastError, &deferred))
	require.Zero(t, failures)
	require.False(t, lastError.Valid)
	require.True(t, deferred)

	require.NoError(t, tx.Rollback())
	_, err = r.db.ExecContext(t.Context(), `SELECT compact_container_topology($1,now()-INTERVAL '10 days',100)`, r.databaseID)
	require.NoError(t, err)
	var recent int
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM container_topology_observation WHERE collected_at>now()-INTERVAL '10 days' AND last_confirmed_at IS NULL`).Scan(&recent))
	require.Equal(t, 1, recent)
	migration, err := os.ReadFile("../../../migrations/current/001022_internal_topology_compaction.down.sql")
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
