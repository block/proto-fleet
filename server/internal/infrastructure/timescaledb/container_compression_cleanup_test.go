package timescaledb

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

type topologyReferencePlan struct {
	Relation string                  `json:"Relation Name"`
	Loops    float64                 `json:"Actual Loops"`
	Plans    []topologyReferencePlan `json:"Plans"`
}

func (p topologyReferencePlan) tankLookups() float64 {
	if p.Relation == "container_tank_metrics" {
		return p.Loops
	}
	var lookups float64
	for _, child := range p.Plans {
		lookups += child.tankLookups()
	}
	return lookups
}

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
			migration, err := os.ReadFile("../../../migrations/current/001029_internal_bound_topology_reference_checks." + direction + ".sql")
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
	_, err = r.db.ExecContext(t.Context(), `SELECT compress_chunk(c) FROM show_chunks('container_tank_metrics') c; ANALYZE container_tank_metrics`)
	require.NoError(t, err)
	// Bound the actual hypertable executions independently of machine speed:
	// chunk pruning must run per receipt, rather than per receipt and tank.
	var ids pq.Int64Array
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT ARRAY(SELECT observation_id FROM container_topology_compaction_pending WHERE device_id=$1 ORDER BY observation_id LIMIT 50)`, r.databaseID).Scan(&ids))
	var rawPlan []byte
	require.NoError(t, r.db.QueryRowContext(t.Context(), `EXPLAIN (ANALYZE,FORMAT JSON) SELECT observation_id,reference_kind FROM container_topology_references($1)`, pq.Array(ids)).Scan(&rawPlan))
	var plans []struct{ Plan topologyReferencePlan }
	require.NoError(t, json.Unmarshal(rawPlan, &plans))
	require.Len(t, plans, 1)
	lookups := plans[0].Plan.tankLookups()
	require.Positive(t, lookups, "the plan must include the executed tank reference lookup")
	require.LessOrEqual(t, lookups, float64(len(ids)), "tank count must not multiply receipt chunk-pruning work")
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
