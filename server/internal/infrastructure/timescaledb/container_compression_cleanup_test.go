package timescaledb

import (
	"os"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestContainerTankObservationRequiresMatchingCollectionTime(t *testing.T) {
	r := newContainerRollupRig(t)
	r.store(t, r.snapshot(0, false, 100e12, 200e12))
	for _, compressed := range []bool{false, true} {
		if compressed {
			_, err := r.db.ExecContext(t.Context(), `SELECT compress_chunk(c) FROM show_chunks('container_tank_metrics') c`)
			require.NoError(t, err)
		}
		_, err := r.db.ExecContext(t.Context(), `INSERT INTO container_tank_metrics(tank_id,time,topology_observation_id,coverage)
		 SELECT tank_id,time+INTERVAL '1 second',topology_observation_id,coverage FROM container_tank_metrics`)
		require.ErrorContains(t, err, "foreign key constraint", "the FK must include collection time so parent deletes can prune chunks")
	}
}

func TestTopologyReferenceMigrationPreservesCompressedAndLatestPins(t *testing.T) {
	r := newContainerRollupRig(t)
	r.db.SetMaxOpenConns(1)
	r.base = time.Now().UTC().Truncate(time.Hour).Add(-20 * 24 * time.Hour)
	for _, offset := range []time.Duration{0, time.Minute, 2 * time.Minute} {
		r.store(t, r.snapshot(offset, false, 100e12, 200e12))
	}
	_, err := r.db.ExecContext(t.Context(), `INSERT INTO container_topology_observation(device_id,version_id,collected_at,complete)
	 SELECT device_id,version_id,collected_at+INTERVAL '30 seconds',complete FROM container_topology_observation WHERE device_id=$1 AND collected_at=$2`, r.databaseID, r.base)
	require.NoError(t, err)
	_, err = r.db.ExecContext(t.Context(), `UPDATE container_tank SET present=false WHERE device_id=$1`, r.databaseID)
	require.NoError(t, err)
	_, err = r.db.ExecContext(t.Context(), `SELECT compress_chunk(c) FROM show_chunks('container_tank_metrics') c;
	 SELECT compress_chunk(c) FROM show_chunks('device_metrics') c`)
	require.NoError(t, err)
	_, err = r.db.ExecContext(t.Context(), `SELECT drop_chunks('device_metrics',older_than=>now()-INTERVAL '10 days')`)
	require.NoError(t, err)
	var ids pq.Int64Array
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT array_agg(id) FROM container_topology_observation WHERE device_id=$1`, r.databaseID).Scan(&ids))
	require.Len(t, ids, 4)
	for _, direction := range []string{"down", "up"} {
		t.Run(direction, func(t *testing.T) {
			migration, err := os.ReadFile("../../../migrations/current/001057_internal_bound_topology_reference_planning." + direction + ".sql")
			require.NoError(t, err)
			_, err = r.db.ExecContext(t.Context(), string(migration))
			require.NoError(t, err)
			var raw, latest, receipts int
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FILTER(WHERE reference_kind='raw'),count(*) FILTER(WHERE reference_kind='latest'),count(DISTINCT observation_id) FROM container_topology_references($1)`, pq.Array(ids)).Scan(&raw, &latest, &receipts))
			require.Equal(t, 3, raw, "compressed history for absent tanks must still pin its receipts")
			require.Equal(t, 1, latest)
			require.Equal(t, 3, receipts, "the receipt without telemetry must remain unpinned")
			var samples int
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM container_tank_metrics`).Scan(&samples))
			require.Equal(t, 6, samples, "replacing the lookup must preserve retained telemetry")
		})
	}
}

func TestTopologyReferenceBoundsPreserveDeviceAndLatestPins(t *testing.T) {
	r := newContainerRollupRig(t)
	r.db.SetMaxOpenConns(1)
	r.base = time.Now().UTC().Truncate(time.Hour).Add(-20 * 24 * time.Hour)
	for _, offset := range []time.Duration{0, 48 * time.Hour, 96 * time.Hour} {
		r.store(t, r.snapshot(offset, false, 100e12, 200e12))
	}
	_, err := r.db.ExecContext(t.Context(), `SELECT compress_chunk(c) FROM show_chunks('device_metrics') c;
 SELECT drop_chunks('container_tank_metrics',older_than=>now()-INTERVAL '10 days');
 SET plan_cache_mode=force_generic_plan`)
	require.NoError(t, err)
	var ids pq.Int64Array
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT array_agg(id ORDER BY collected_at) FROM container_topology_observation WHERE device_id=$1`, r.databaseID).Scan(&ids))
	require.Len(t, ids, 3)
	stmt, err := r.db.PrepareContext(t.Context(), `SELECT count(*) FILTER(WHERE reference_kind='raw'),count(*) FILTER(WHERE reference_kind='latest') FROM container_topology_references($1)`)
	require.NoError(t, err)
	t.Cleanup(func() { _ = stmt.Close() })
	for i := range 12 {
		selected := pq.Int64Array{ids[i%len(ids)]}
		if i%2 == 0 {
			selected = ids
		}
		var raw, latest int
		require.NoError(t, stmt.QueryRowContext(t.Context(), pq.Array(selected)).Scan(&raw, &latest))
		require.Equal(t, len(selected), raw, "changing bounds must preserve compressed device references")
	}
	_, err = r.db.ExecContext(t.Context(), `SELECT drop_chunks('device_metrics',older_than=>now()-INTERVAL '10 days')`)
	require.NoError(t, err)
	var raw, latest int
	require.NoError(t, stmt.QueryRowContext(t.Context(), pq.Array(ids)).Scan(&raw, &latest))
	require.Zero(t, raw)
	require.Equal(t, 1, latest, "latest references must survive after raw history expires")
	for _, empty := range []pq.Int64Array{nil, {}, {-1}} {
		require.NoError(t, stmt.QueryRowContext(t.Context(), pq.Array(empty)).Scan(&raw, &latest))
		require.Zero(t, raw)
		require.Zero(t, latest)
	}
}

func TestContainerCompressionCleanupMakesProgressWithinJobDeadline(t *testing.T) {
	r := newContainerRollupRig(t)
	r.db.SetMaxOpenConns(1)
	r.base = time.Now().UTC().Truncate(time.Hour).Add(-20 * 24 * time.Hour)
	r.store(t, r.snapshot(0, false, 100e12, 200e12))
	// Both cleanup queues have 500 unreferenced receipts. The anchor already
	// represents pending receipts, as it would after an earlier pinned batch.
	_, err := r.db.ExecContext(t.Context(), `INSERT INTO container_topology_observation(device_id,version_id,collected_at,complete)
	 SELECT device_id,version_id,collected_at+n*INTERVAL '1 second',complete
	 FROM container_topology_observation CROSS JOIN generate_series(1,1000) n WHERE device_id=$1 AND collected_at=$2`, r.databaseID, r.base)
	require.NoError(t, err)
	_, err = r.db.ExecContext(t.Context(), `UPDATE container_topology_observation SET last_confirmed_at=collected_at+INTERVAL '500 seconds' WHERE device_id=$1 AND collected_at=$2`, r.databaseID, r.base)
	require.NoError(t, err)
	_, err = r.db.ExecContext(t.Context(), `UPDATE container_topology_compaction_state SET scanned_through=$2::timestamptz+INTERVAL '500 seconds',
	 anchor_id=(SELECT id FROM container_topology_observation WHERE device_id=$1 AND collected_at=$2) WHERE device_id=$1`, r.databaseID, r.base)
	require.NoError(t, err)
	_, err = r.db.ExecContext(t.Context(), `INSERT INTO container_topology_compaction_pending(observation_id,device_id,next_check_at)
	 SELECT id,device_id,now() FROM container_topology_observation WHERE device_id=$1 AND collected_at>$2 AND collected_at<=$2::timestamptz+INTERVAL '500 seconds'`, r.databaseID, r.base)
	require.NoError(t, err)

	// Cover nine days of hourly chunks with multiple tanks. The plan assertion
	// below detects per-tank scans without needing millions of retained rows.
	recent := time.Now().UTC().Truncate(time.Hour).Add(-9 * 24 * time.Hour)
	_, err = r.db.ExecContext(t.Context(), `INSERT INTO container_tank(device_id,position,first_seen_at,last_seen_at)
	 SELECT $1,n,$2,$2 FROM generate_series(3,6) n`, r.databaseID, recent)
	require.NoError(t, err)
	_, err = r.db.ExecContext(t.Context(), `INSERT INTO container_topology_observation(device_id,version_id,collected_at,complete)
	 SELECT device_id,version_id,$3::timestamptz+n*INTERVAL '1 hour',complete
	 FROM container_topology_observation CROSS JOIN generate_series(1,216) n WHERE device_id=$1 AND collected_at=$2`, r.databaseID, r.base, recent)
	require.NoError(t, err)
	_, err = r.db.ExecContext(t.Context(), `INSERT INTO container_tank_metrics(tank_id,time,topology_observation_id,topology_version_id,coverage)
	 SELECT t.id,o.collected_at,o.id,o.version_id,'{}' FROM container_topology_observation o
	 JOIN container_tank t ON t.device_id=o.device_id WHERE o.device_id=$1 AND o.collected_at>$2`, r.databaseID, recent)
	require.NoError(t, err)
	_, err = r.db.ExecContext(t.Context(), `INSERT INTO device_metrics(time,device_identifier,topology_observation_id)
 SELECT o.collected_at,d.device_identifier,o.id FROM container_topology_observation o JOIN device d ON d.id=o.device_id
 WHERE o.device_id=$1 AND o.collected_at>$2`, r.databaseID, recent)
	require.NoError(t, err)
	_, err = r.db.ExecContext(t.Context(), `SELECT compress_chunk(c) FROM show_chunks('device_metrics') c;
 SELECT compress_chunk(c) FROM show_chunks('container_tank_metrics') c; ANALYZE container_tank_metrics`)
	require.NoError(t, err)
	// Planning must not lock every retained chunk for receipts older than those
	// chunks. This catches the cost even when execution prunes all of them.
	var ids pq.Int64Array
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT ARRAY(SELECT observation_id FROM container_topology_compaction_pending WHERE device_id=$1 ORDER BY observation_id LIMIT 50)`, r.databaseID).Scan(&ids))
	tx, err := r.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	_, err = tx.ExecContext(t.Context(), `SET LOCAL plan_cache_mode=force_generic_plan`)
	require.NoError(t, err)
	var refs int
	require.NoError(t, tx.QueryRowContext(t.Context(), `SELECT count(*) FROM container_topology_references($1)`, pq.Array(ids)).Scan(&refs))
	require.Zero(t, refs)
	var unrelatedLocks int
	require.NoError(t, tx.QueryRowContext(t.Context(), `SELECT count(*) FROM pg_locks l
 JOIN timescaledb_information.chunks c ON l.relation=format('%I.%I',c.chunk_schema,c.chunk_name)::regclass
 WHERE l.pid=pg_backend_pid() AND c.hypertable_name IN ('container_tank_metrics','device_metrics') AND c.range_start>$1`, r.base.Add(time.Hour)).Scan(&unrelatedLocks))
	require.Zero(t, unrelatedLocks, "expired receipts must not plan or lock unrelated retained chunks")
	require.NoError(t, tx.Commit())
	var job int
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT job_id FROM timescaledb_information.jobs WHERE proc_name='compact_container_topology_job'`).Scan(&job))
	// Run the actual job procedure and its scheduling commit. Manual run_job
	// does not impose the background worker's max_runtime, so enforce the same
	// two-second deadline on the connection explicitly.
	_, err = r.db.ExecContext(t.Context(), `SELECT alter_job(job_id,config=>jsonb_set(config,'{enabled}','true')) FROM timescaledb_information.jobs WHERE job_id=$1`, job)
	require.NoError(t, err)
	_, err = r.db.ExecContext(t.Context(), `SET statement_timeout='2s'`)
	require.NoError(t, err)
	remaining := 1000
	for run := 0; remaining > 0 && run < 20; run++ {
		started := time.Now()
		_, err = r.db.ExecContext(t.Context(), `CALL run_job($1)`, job)
		require.NoError(t, err, "cleanup must commit progress before its deadline")
		var next int
		require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM container_topology_observation WHERE device_id=$1 AND collected_at>$2 AND collected_at<=$2::timestamptz+INTERVAL '1000 seconds'`, r.databaseID, r.base).Scan(&next))
		require.Less(t, next, remaining, "each scheduled batch must commit deletions from the backlog")
		require.LessOrEqual(t, remaining-next, 100, "cap both queues even when the stored job config requests 500")
		t.Logf("cleanup run %d: deleted %d receipts in %s", run+1, remaining-next, time.Since(started))
		remaining = next
	}
	require.Zero(t, remaining)
	var retained int
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM container_tank_metrics`).Scan(&retained))
	require.Equal(t, 1298, retained, "cleanup must retain all compressed telemetry and its FK targets")
}
