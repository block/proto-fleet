package timescaledb

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestTopologyThroughputMigrationPreservesJobAndProgress(t *testing.T) {
	r := newContainerRollupRig(t)
	r.base = time.Now().UTC().Add(-20 * 24 * time.Hour)
	r.store(t, r.snapshot(0, false, 100e12, 200e12))
	var job int
	var enabled bool
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT job_id,(config->>'enabled')::boolean
 FROM timescaledb_information.jobs WHERE proc_name='compact_container_topology_job'`).Scan(&job, &enabled))
	require.False(t, enabled, "fresh installs must remain opt-in")
	// This is an existing operator configuration, including an unknown future key.
	_, err := r.db.ExecContext(t.Context(), `SELECT alter_job($1,config=>' {"enabled":true,"batch_size":17,"operator_note":"preserve"}'::jsonb)`, job)
	require.NoError(t, err)
	_, err = r.db.ExecContext(t.Context(), `UPDATE container_topology_compaction_state SET next_run_at=now()+INTERVAL '1 minute',
 scanned_through=$2,anchor_id=(SELECT id FROM container_topology_observation WHERE device_id=$1),examined=7,deleted=3,failures=2,last_error='previous failure' WHERE device_id=$1`, r.databaseID, r.base)
	require.NoError(t, err)
	var before string
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT row_to_json(s)::text FROM container_topology_compaction_state s WHERE device_id=$1`, r.databaseID).Scan(&before))
	for _, step := range []struct {
		direction string
		seconds   int
	}{{"down", 10}, {"up", 1}} {
		migration, err := os.ReadFile("../../../migrations/current/001034_internal_topology_cleanup_throughput." + step.direction + ".sql")
		require.NoError(t, err)
		_, err = r.db.ExecContext(t.Context(), string(migration))
		require.NoError(t, err)
		var interval, runtime, retry int
		var scheduled, configPreserved bool
		var after string
		require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT extract(epoch FROM schedule_interval)::int,
   extract(epoch FROM max_runtime)::int,extract(epoch FROM retry_period)::int,scheduled,
   config=' {"enabled":true,"batch_size":17,"operator_note":"preserve"}'::jsonb
   FROM timescaledb_information.jobs WHERE job_id=$1`, job).Scan(&interval, &runtime, &retry, &scheduled, &configPreserved))
		require.Equal(t, step.seconds, interval)
		require.Equal(t, 2, runtime)
		require.Equal(t, 60, retry)
		require.False(t, scheduled, "migration must not resume a paused job")
		require.True(t, configPreserved)
		require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT row_to_json(s)::text FROM container_topology_compaction_state s WHERE device_id=$1`, r.databaseID).Scan(&after))
		require.Equal(t, before, after, "migration must preserve cursors, counters and leases")
	}
}

// This wall-clock capacity test deliberately enables only the real cleanup job.
// Run separately on an isolated instance; a busy shared developer/CI database
// cannot establish scheduling capacity. The regular suite covers deadline, FK,
// contention, cancellation and migration behavior without this multi-minute run.
func TestTopologyCompactionScheduledCapacity(t *testing.T) {
	if os.Getenv("CT1_TOPOLOGY_CAPACITY_TEST") != "1" {
		t.Skip("set CT1_TOPOLOGY_CAPACITY_TEST=1 on an isolated TimescaleDB for the wall-clock capacity test")
	}
	const controllers = 100
	const tanks = 6
	const historyHours = 216
	const forwardPerController = 100
	const retriesPerController = 50
	const arrivalInterval = 15 * time.Second
	const arrivalDuration = 150 * time.Second
	const drainDeadline = 4 * time.Minute
	svc := testutil.NewDatabaseService(t, nil)
	database := svc.DB
	user := svc.CreateSuperAdminUser()
	ids := make([]int64, controllers)
	for i := range ids {
		ids[i] = svc.CreateDevice(user.OrganizationID, "proto").DatabaseID
	}
	base := time.Now().UTC().Truncate(time.Hour).Add(-20 * 24 * time.Hour)
	recent := time.Now().UTC().Truncate(time.Hour).Add(-9 * 24 * time.Hour)
	exec := func(query string, args ...any) {
		t.Helper()
		_, err := database.ExecContext(t.Context(), query, args...)
		require.NoError(t, err)
	}
	exec(`UPDATE discovered_device SET device_class=2 WHERE id IN (SELECT discovered_device_id FROM device WHERE id=ANY($1))`, pq.Array(ids))
	exec(`INSERT INTO container_inventory(device_id,org_id,observed_at) SELECT id,org_id,$2 FROM device WHERE id=ANY($1)`, pq.Array(ids), recent)
	exec(`INSERT INTO container_topology_version(device_id,fingerprint,topology)
  SELECT unnest($1::bigint[]),'capacity-test','{}'`, pq.Array(ids))
	exec(`INSERT INTO container_tank(device_id,position,first_seen_at,last_seen_at)
  SELECT id,n,$2,$2 FROM unnest($1::bigint[]) id CROSS JOIN generate_series(1,$3) n`, pq.Array(ids), base, tanks)
	exec(`INSERT INTO container_topology_observation(device_id,version_id,collected_at,complete)
  SELECT device_id,id,$1::timestamptz+n*INTERVAL '15 seconds',true
  FROM container_topology_version CROSS JOIN generate_series(0,$2) n`, base, retriesPerController+forwardPerController)
	exec(`UPDATE container_topology_observation SET last_confirmed_at=$1::timestamptz+$2*INTERVAL '15 seconds' WHERE collected_at=$1`, base, retriesPerController)
	exec(`UPDATE container_topology_compaction_state s SET scanned_through=$1::timestamptz+$2*INTERVAL '15 seconds',
  anchor_id=o.id FROM container_topology_observation o WHERE o.device_id=s.device_id AND o.collected_at=$1`, base, retriesPerController)
	exec(`INSERT INTO container_topology_compaction_pending(observation_id,device_id)
  SELECT id,device_id FROM container_topology_observation WHERE collected_at>$1 AND collected_at<=$1::timestamptz+$2*INTERVAL '15 seconds'`, base, retriesPerController)
	// Nine days of hourly chunks and six tank segments per controller. These are
	// retained FK targets, distinct from the old, unreferenced cleanup workload.
	exec(`INSERT INTO container_topology_observation(device_id,version_id,collected_at,complete)
  SELECT device_id,id,$1::timestamptz+n*INTERVAL '1 hour',true
  FROM container_topology_version CROSS JOIN generate_series(0,$2-1) n`, recent, historyHours)
	exec(`INSERT INTO container_tank_metrics(tank_id,time,topology_observation_id,topology_version_id,coverage)
  SELECT t.id,o.collected_at,o.id,o.version_id,'{}' FROM container_topology_observation o
  JOIN container_tank t ON t.device_id=o.device_id WHERE o.collected_at>=$1`, recent)
	exec(`SELECT compress_chunk(c) FROM show_chunks('container_tank_metrics') c`)
	exec(`ANALYZE container_tank_metrics`)
	var job int
	require.NoError(t, database.QueryRowContext(t.Context(), `SELECT job_id FROM timescaledb_information.jobs WHERE proc_name='compact_container_topology_job'`).Scan(&job))
	// No manual run_job calls or shortened test schedule: exercise the migration's
	// actual cadence and background-worker max_runtime, including worker startup.
	exec(`SELECT alter_job(job_id,config=>jsonb_set(config,'{enabled}','true'),scheduled=>true,next_start=>now()) FROM timescaledb_information.jobs WHERE job_id=$1`, job)
	t.Cleanup(func() {
		// t.Context is canceled before cleanup, but the job must be stopped
		// even when an assertion fails before the normal drain completes.
		//nolint:usetesting
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := database.ExecContext(ctx, `SELECT alter_job($1,scheduled=>false)`, job)
		require.NoError(t, err)
	})
	started := time.Now()
	nextArrival := arrivalInterval
	nextReport := 30 * time.Second
	cohorts := 0
	expectedForward := controllers * forwardPerController
	peakBacklog := expectedForward
	maxIngest := time.Duration(0)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	waitForTick := func() {
		t.Helper()
		select {
		case <-t.Context().Done():
			t.Fatal(t.Context().Err())
		case <-ticker.C:
		}
	}
	type cleanupProgress struct {
		examined int
		pending  int
		serviced int
		failures int
	}
	readProgress := func() cleanupProgress {
		t.Helper()
		var progress cleanupProgress
		require.NoError(t, database.QueryRowContext(t.Context(), `SELECT sum(examined),count(*) FILTER(WHERE examined>0),sum(failures)
   FROM container_topology_compaction_state`).Scan(&progress.examined, &progress.serviced, &progress.failures))
		require.NoError(t, database.QueryRowContext(t.Context(), `SELECT count(*) FROM container_topology_compaction_pending`).Scan(&progress.pending))
		return progress
	}
	for time.Since(started) < arrivalDuration {
		waitForTick()
		elapsed := time.Since(started)
		for elapsed >= nextArrival && nextArrival <= arrivalDuration {
			cohorts++
			at := base.Add(time.Duration(retriesPerController+forwardPerController+cohorts) * arrivalInterval)
			ingestStarted := time.Now()
			// New eligible receipts simulate the moving ten-day cutoff; a separate
			// current observation exercises ingestion under the same organization lock.
			tx, err := database.BeginTx(t.Context(), nil)
			require.NoError(t, err)
			t.Cleanup(func() { _ = tx.Rollback() })
			_, err = tx.ExecContext(t.Context(), `SELECT id FROM organization WHERE id=$1 FOR NO KEY UPDATE`, user.OrganizationID)
			require.NoError(t, err)
			_, err = tx.ExecContext(t.Context(), `INSERT INTO container_topology_observation(device_id,version_id,collected_at,complete)
    SELECT device_id,id,at,true FROM container_topology_version CROSS JOIN (VALUES ($1::timestamptz),(now())) times(at)`, at)
			require.NoError(t, err)
			require.NoError(t, tx.Commit())
			expectedForward += controllers
			maxIngest = max(maxIngest, time.Since(ingestStarted))
			nextArrival += arrivalInterval
		}
		progress := readProgress()
		backlog := expectedForward - progress.examined
		peakBacklog = max(peakBacklog, backlog)
		require.Zero(t, progress.failures, "cleanup must not hide failures while claiming capacity")
		if elapsed >= nextReport {
			t.Logf("after %s: examined=%d forward backlog=%d retry backlog=%d serviced=%d/%d", elapsed.Round(time.Second), progress.examined, backlog, progress.pending, progress.serviced, controllers)
			nextReport += 30 * time.Second
		}
	}
	sustained := time.Since(started)
	progress := readProgress()
	rate := float64(progress.examined) / sustained.Seconds()
	require.Equal(t, controllers, progress.serviced, "every controller must receive a turn under sustained arrivals")
	require.Greater(t, rate, 2*float64(controllers)/arrivalInterval.Seconds(), "forward capacity must exceed arrival rate with at least 100%% drain margin")
	require.Less(t, expectedForward-progress.examined, controllers*forwardPerController, "backlog must shrink during arrivals")
	require.LessOrEqual(t, peakBacklog, controllers*(forwardPerController+1), "backlog must stay bounded under arrivals")
	require.Less(t, maxIngest, 2*time.Second, "cleanup must not stall ingestion beyond its runtime budget")
	t.Logf("sustained forward rate %.2f/s versus %.2f/s arrivals; maximum ingestion transaction %s", rate, float64(controllers)/arrivalInterval.Seconds(), maxIngest)
	deadline := time.Now().Add(drainDeadline)
	for progress.examined < expectedForward || progress.pending > 0 {
		require.True(t, time.Now().Before(deadline), "backlog failed to drain: examined=%d pending=%d", progress.examined, progress.pending)
		waitForTick()
		progress = readProgress()
		require.Zero(t, progress.failures)
	}
	dbtest.PauseBackgroundJobs(t, database)
	var retained, recentReceipts, oldReceipts, jobFailures int
	require.NoError(t, database.QueryRowContext(t.Context(), `SELECT count(*) FROM container_tank_metrics`).Scan(&retained))
	require.Equal(t, controllers*tanks*historyHours, retained, "all compressed samples and FK targets must survive")
	require.NoError(t, database.QueryRowContext(t.Context(), `SELECT count(*) FILTER(WHERE collected_at>=$1),
 count(*) FILTER(WHERE collected_at<now()-INTERVAL '10 days') FROM container_topology_observation`, recent.Add(historyHours*time.Hour)).Scan(&recentReceipts, &oldReceipts))
	require.Equal(t, controllers*cohorts, recentReceipts, "current ingestion must remain exact")
	require.Equal(t, controllers, oldReceipts, "only one historical span anchor per controller should remain")
	require.NoError(t, database.QueryRowContext(t.Context(), `SELECT total_failures FROM timescaledb_information.job_stats WHERE job_id=$1`, job).Scan(&jobFailures))
	require.Zero(t, jobFailures, "background timeouts/crashes must not be mistaken for successful progress")
	var slow int
	require.NoError(t, database.QueryRowContext(t.Context(), `SELECT count(*) FROM container_topology_compaction_state WHERE last_duration_ms>=2000`).Scan(&slow))
	require.Zero(t, slow)
	t.Logf("drained %d forward receipts and %d retries across %d controllers in %s; retained %d compressed samples",
		progress.examined, controllers*retriesPerController, controllers, time.Since(started).Round(time.Second), retained)
}
