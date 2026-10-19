package timescaledb

import (
	"testing"
	"time"

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

	// Three million retained samples, spread over almost nine days of hourly
	// chunks. FK checks must not scan these for each expired receipt deletion.
	recent := time.Now().UTC().Truncate(time.Hour).Add(-9 * 24 * time.Hour)
	_, err = r.db.ExecContext(t.Context(), `INSERT INTO container_tank(device_id,position,first_seen_at,last_seen_at)
	 SELECT $1,n,$2,$2 FROM generate_series(3,60) n`, r.databaseID, recent)
	require.NoError(t, err)
	_, err = r.db.ExecContext(t.Context(), `INSERT INTO container_topology_observation(device_id,version_id,collected_at,complete)
	 SELECT device_id,version_id,$3::timestamptz+n*INTERVAL '15 seconds',complete
	 FROM container_topology_observation CROSS JOIN generate_series(1,50000) n WHERE device_id=$1 AND collected_at=$2`, r.databaseID, r.base, recent)
	require.NoError(t, err)
	_, err = r.db.ExecContext(t.Context(), `INSERT INTO container_tank_metrics(tank_id,time,topology_observation_id,topology_version_id,coverage)
	 SELECT t.id,o.collected_at,o.id,o.version_id,'{}' FROM container_topology_observation o
	 JOIN container_tank t ON t.device_id=o.device_id WHERE o.device_id=$1 AND o.collected_at>$2`, r.databaseID, recent)
	require.NoError(t, err)
	_, err = r.db.ExecContext(t.Context(), `SELECT compress_chunk(c) FROM show_chunks('container_tank_metrics') c; ANALYZE container_tank_metrics`)
	require.NoError(t, err)
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
	require.Equal(t, 3000002, retained, "cleanup must retain all compressed telemetry and its FK targets")
}
