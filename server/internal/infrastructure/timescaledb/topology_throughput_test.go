package timescaledb

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestTopologyThroughputMigrationPreservesJobAndProgress(t *testing.T) {
	const compactionState = `SELECT row_to_json(s)::text FROM (
 SELECT device_id,scanned_through,anchor_id,next_run_at,last_run_at,last_duration_ms,
        examined,deleted,failures,last_error
 FROM container_topology_compaction_state WHERE device_id=$1
) s`
	r := newContainerRollupRig(t)
	r.base = time.Now().UTC().Add(-20 * 24 * time.Hour)
	r.store(t, r.snapshot(0, false, 100e12, 200e12))
	var job int
	var enabled bool
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT job_id,(config->>'enabled')::boolean
 FROM timescaledb_information.jobs WHERE proc_name='compact_container_topology_job'`).Scan(&job, &enabled))
	require.False(t, enabled, "fresh installs must remain opt-in")
	_, err := r.db.ExecContext(t.Context(), `UPDATE container_topology_compaction_state SET next_run_at=now()+INTERVAL '1 minute',
 scanned_through=$2,anchor_id=(SELECT id FROM container_topology_observation WHERE device_id=$1),examined=7,deleted=3,failures=2,last_error='previous failure' WHERE device_id=$1`, r.databaseID, r.base)
	require.NoError(t, err)
	var before string
	require.NoError(t, r.db.QueryRowContext(t.Context(), compactionState, r.databaseID).Scan(&before))
	var guardedProcedureSource string
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT prosrc FROM pg_proc
 WHERE oid='public.compact_container_topology_job(integer,jsonb)'::regprocedure`).Scan(&guardedProcedureSource))
	var handoffProcedureSource string
	var fullBatchProcedureSource string
	for _, scenario := range []struct {
		name      string
		scheduled bool
		config    string
	}{
		{"paused_enabled", false, `{"enabled":true,"batch_size":17,"microbatch_size":5,"operator_note":"preserve"}`},
		{"scheduled_disabled", true, `{"enabled":false,"batch_size":17,"operator_note":"preserve"}`},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			// Scheduling and the opt-in config are independent operator choices. A
			// future start keeps the real scheduler from changing progress in this test.
			_, err := r.db.ExecContext(t.Context(), `SELECT alter_job($1,config=>$2::jsonb,scheduled=>$3,next_start=>now()+INTERVAL '1 year')`, job, scenario.config, scenario.scheduled)
			require.NoError(t, err)
			for _, step := range []struct {
				migration string
				direction string
				seconds   int
				fixed     bool
			}{
				{"001035_internal_topology_cleanup_throughput", "down", 10, true},
				{"001035_internal_topology_cleanup_throughput", "up", 1, false},
				{"001069_internal_topology_cleanup_lock_handoff", "down", 1, false},
				{"001069_internal_topology_cleanup_lock_handoff", "up", 1, false},
				{"001072_internal_topology_cleanup_runtime_guard", "down", 1, false},
				{"001072_internal_topology_cleanup_runtime_guard", "up", 1, false},
			} {
				migration, err := os.ReadFile("../../../migrations/current/" + step.migration + "." + step.direction + ".sql")
				require.NoError(t, err)
				_, err = r.db.ExecContext(t.Context(), string(migration))
				require.NoError(t, err)
				var interval, runtime, retry int
				var scheduled, fixed, configPreserved bool
				var after string
				require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT extract(epoch FROM schedule_interval)::int,
   extract(epoch FROM max_runtime)::int,extract(epoch FROM retry_period)::int,scheduled,fixed_schedule,
   config=$2::jsonb FROM timescaledb_information.jobs WHERE job_id=$1`, job, scenario.config).Scan(&interval, &runtime, &retry, &scheduled, &fixed, &configPreserved))
				require.Equal(t, step.seconds, interval)
				require.Equal(t, 2, runtime)
				// This asserts the stored policy, not the effective failure retry delay,
				// which TimescaleDB caps relative to schedule_interval.
				require.Equal(t, 60, retry)
				require.Equal(t, step.fixed, fixed)
				require.Equal(t, scenario.scheduled, scheduled, "migration must preserve the operator's scheduling choice")
				require.True(t, configPreserved)
				require.NoError(t, r.db.QueryRowContext(t.Context(), compactionState, r.databaseID).Scan(&after))
				require.Equal(t, before, after, "migration must preserve cursors, counters and leases")
				if step.migration == "001069_internal_topology_cleanup_lock_handoff" {
					var telemetryColumns int
					require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM information_schema.columns
 WHERE table_schema='public' AND table_name='container_topology_compaction_state'
 AND column_name IN ('deferrals','last_deferred_at')`).Scan(&telemetryColumns))
					var procedureSource string
					require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT prosrc FROM pg_proc
 WHERE oid='public.compact_container_topology_job(integer,jsonb)'::regprocedure`).Scan(&procedureSource))
					if step.direction == "down" {
						require.Zero(t, telemetryColumns)
						require.NotEqual(t, handoffProcedureSource, procedureSource, "down migration must restore the full-batch procedure")
						require.NotContains(t, procedureSource, "microbatch_size")
						require.Contains(t, procedureSource, "compact_container_topology(target,cutoff,batch_size)")
						if fullBatchProcedureSource == "" {
							fullBatchProcedureSource = procedureSource
						} else {
							require.Equal(t, fullBatchProcedureSource, procedureSource)
						}
					} else {
						require.Equal(t, 2, telemetryColumns)
						if handoffProcedureSource == "" {
							handoffProcedureSource = procedureSource
						} else {
							require.Equal(t, handoffProcedureSource, procedureSource, "up migration must restore the lock-handoff procedure")
						}
					}
				}
				if step.migration == "001072_internal_topology_cleanup_runtime_guard" {
					var telemetryColumns int
					require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM information_schema.columns
 WHERE table_schema='public' AND table_name='container_topology_compaction_state'
 AND column_name IN ('runtime_yields','last_runtime_yield_at')`).Scan(&telemetryColumns))
					var procedureSource string
					require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT prosrc FROM pg_proc
 WHERE oid='public.compact_container_topology_job(integer,jsonb)'::regprocedure`).Scan(&procedureSource))
					if step.direction == "down" {
						require.Zero(t, telemetryColumns)
						require.Equal(t, handoffProcedureSource, procedureSource, "down migration must restore the lock-handoff procedure")
						require.NotContains(t, procedureSource, "run_budget_ms")
					} else {
						require.Equal(t, 2, telemetryColumns)
						require.Equal(t, guardedProcedureSource, procedureSource, "up migration must restore the runtime guard")
						require.Contains(t, procedureSource, "run_budget_ms")
						require.Contains(t, procedureSource, "lock_timeout_ms")
					}
				}
			}
		})
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
	// Qualify the documented rollout setting below the unchanged hard cap of 50.
	// Larger batches exceeded the 250 ms ingestion bound in this fixture.
	const batchSize = 25
	const arrivalInterval = 15 * time.Second
	const arrivalDuration = 150 * time.Second
	const flushInterval = time.Second
	const flushes = int(arrivalDuration / flushInterval)
	const flushesPerPoll = int(arrivalInterval / flushInterval)
	const schedulerStartDeadline = 30 * time.Second
	const drainDeadline = 8 * time.Minute
	svc := testutil.NewDatabaseService(t, nil)
	database := svc.DB
	user := svc.CreateSuperAdminUser()
	ids := make([]int64, controllers)
	identifiers := make([]string, controllers)
	for i := range ids {
		device := svc.CreateDevice(user.OrganizationID, "proto")
		ids[i] = device.DatabaseID
		identifiers[i] = device.ID
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
	t.Cleanup(func() {
		// t.Context is canceled before cleanup, but the job must be stopped
		// even when an assertion fails before the normal drain completes.
		//nolint:usetesting
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := database.ExecContext(ctx, `SELECT alter_job($1,scheduled=>false)`, job)
		require.NoError(t, err)
	})
	// Prove the real scheduler encounters the production ingestion lock before
	// timing capacity. Holding it only for this probe avoids manufacturing a
	// slow telemetry workload to force overlap during the measured flushes.
	probe, err := database.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = probe.Rollback() })
	_, err = sqlc.New(probe).LockHashboardOrganization(t.Context(), identifiers[0])
	require.NoError(t, err)
	var successesBefore int
	require.NoError(t, database.QueryRowContext(t.Context(), `SELECT COALESCE((SELECT total_successes FROM timescaledb_information.job_stats WHERE job_id=$1),0)`, job).Scan(&successesBefore))
	// No manual run_job calls or shortened test schedule: use the migration's
	// actual cadence and background-worker max_runtime, including worker startup.
	exec(`SELECT alter_job(job_id,config=>config || jsonb_build_object('enabled',true,'batch_size',$2::int,'microbatch_size',3),scheduled=>true,next_start=>now()) FROM timescaledb_information.jobs WHERE job_id=$1`, job, batchSize)
	// Template cloning detaches the Timescale scheduler. Start it after enabling
	// the only scheduled job so this opt-in test measures the real worker.
	exec(`SELECT _timescaledb_functions.start_background_workers()`)
	probeTicker := time.NewTicker(100 * time.Millisecond)
	defer probeTicker.Stop()
	probeDeadline := time.Now().Add(schedulerStartDeadline)
	probeDeferrals := 0
	for {
		var successes, examined, completed, failures int
		require.NoError(t, database.QueryRowContext(t.Context(), `SELECT COALESCE((SELECT total_successes FROM timescaledb_information.job_stats WHERE job_id=$1),0)`, job).Scan(&successes))
		if successes > successesBefore {
			// A completed successful job with an exact deferral count and no
			// progress proves the scheduler met ingestion lock contention.
			require.NoError(t, database.QueryRowContext(t.Context(), `SELECT sum(deferrals),sum(examined),count(last_run_at),sum(failures)
   FROM container_topology_compaction_state`).Scan(&probeDeferrals, &examined, &completed, &failures))
			require.Positive(t, probeDeferrals, "real scheduler must defer while ingestion owns the organization lock")
			require.Zero(t, examined)
			require.Zero(t, completed)
			require.Zero(t, failures)
			break
		}
		require.True(t, time.Now().Before(probeDeadline), "real scheduler did not complete the contention probe")
		select {
		case <-t.Context().Done():
			t.Fatal(t.Context().Err())
		case <-probeTicker.C:
		}
	}
	exec(`SELECT alter_job($1,scheduled=>false)`, job)
	require.NoError(t, probe.Rollback())
	// Preserve the workload, progress and counters. Remove only the probe's
	// one-minute backoff so measured service starts with every controller due.
	exec(`UPDATE container_topology_compaction_state SET next_run_at=now()`)
	t.Logf("real-scheduler contention probe: %d committed lock deferrals before capacity measurement; configured batch size=%d", probeDeferrals, batchSize)
	nextReport := 30 * time.Second
	arrivals := 0
	expectedForward := controllers * forwardPerController
	peakBacklog := expectedForward
	maxIngest := time.Duration(0)
	type cleanupProgress struct {
		examined  int
		pending   int
		serviced  int
		failures  int
		deferrals int
	}
	deferralsBefore := probeDeferrals
	readProgress := func() cleanupProgress {
		t.Helper()
		var progress cleanupProgress
		require.NoError(t, database.QueryRowContext(t.Context(), `SELECT COALESCE(sum(examined),0),COALESCE(sum(failures),0),
 COALESCE(sum(deferrals),0),count(*) FILTER(WHERE examined>0)
 FROM container_topology_compaction_state`).Scan(&progress.examined, &progress.failures, &progress.deferrals, &progress.serviced))
		require.NoError(t, database.QueryRowContext(t.Context(), `SELECT count(*) FROM container_topology_compaction_pending`).Scan(&progress.pending))
		return progress
	}
	readProgress()
	started := time.Now()
	exec(`SELECT alter_job($1,scheduled=>true,next_start=>now())`, job)
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()
	waitForTick := func() {
		t.Helper()
		select {
		case <-t.Context().Done():
			t.Fatal(t.Context().Err())
		case <-ticker.C:
		}
	}
	for flush := range flushes {
		waitForTick()
		elapsed := time.Since(started)
		// Distribute each controller's fifteen-second poll across fifteen one-second
		// telemetry flushes (six or seven controllers per transaction), rather than
		// holding the organization lock while all 100 arrive in a single burst.
		phase := flush % flushesPerPoll
		first, end := phase*controllers/flushesPerPoll, (phase+1)*controllers/flushesPerPoll
		at := base.Add(time.Duration(retriesPerController+forwardPerController+flush/flushesPerPoll+1) * arrivalInterval)
		ingestStarted := time.Now()
		tx, err := database.BeginTx(t.Context(), nil)
		beginDuration := time.Since(ingestStarted)
		require.NoError(t, err)
		t.Cleanup(func() { _ = tx.Rollback() })
		q := sqlc.New(tx)
		var lockTotal, lockMax, insertTotal, insertMax time.Duration
		for controller := first; controller < end; controller++ {
			// The production writer takes this lock for each controller inside the
			// shared telemetry transaction. Keep that order and lock mode here.
			lockStarted := time.Now()
			_, err = q.LockHashboardOrganization(t.Context(), identifiers[controller])
			lockDuration := time.Since(lockStarted)
			lockTotal += lockDuration
			lockMax = max(lockMax, lockDuration)
			require.NoError(t, err)
			// Eligible receipts model the moving ten-day cutoff, while current
			// receipts verify ingestion survives cleanup under the same lock.
			insertStarted := time.Now()
			_, err = tx.ExecContext(t.Context(), `INSERT INTO container_topology_observation(device_id,version_id,collected_at,complete)
    SELECT device_id,id,at,true FROM container_topology_version CROSS JOIN (VALUES ($1::timestamptz),(now())) times(at)
    WHERE device_id=$2`, at, ids[controller])
			insertDuration := time.Since(insertStarted)
			insertTotal += insertDuration
			insertMax = max(insertMax, insertDuration)
			require.NoError(t, err)
		}
		commitStarted := time.Now()
		require.NoError(t, tx.Commit())
		commitDuration := time.Since(commitStarted)
		arrivals += end - first
		expectedForward += end - first
		ingestDuration := time.Since(ingestStarted)
		maxIngest = max(maxIngest, ingestDuration)
		if flush < 3 || ingestDuration >= 200*time.Millisecond {
			// Lock-query latency includes round trips and execution as well as
			// possible lock waits; it cannot by itself prove organization contention.
			t.Logf("flush %d/%d (%d controllers): total=%s begin=%s organization lock queries total/max=%s/%s inserts total/max=%s/%s commit=%s",
				flush+1, flushes, end-first, ingestDuration, beginDuration, lockTotal, lockMax, insertTotal, insertMax, commitDuration)
		}
		require.Less(t, ingestDuration, 250*time.Millisecond, "cleanup must not stall ingestion: flush %d/%d (%d controllers); begin=%s lock queries total/max=%s/%s inserts total/max=%s/%s commit=%s",
			flush+1, flushes, end-first, beginDuration, lockTotal, lockMax, insertTotal, insertMax, commitDuration)
		progress := readProgress()
		backlog := expectedForward - progress.examined
		peakBacklog = max(peakBacklog, backlog)
		require.Zero(t, progress.failures, "cleanup must not hide failures while claiming capacity")
		if elapsed >= nextReport {
			t.Logf("after %s: examined=%d forward backlog=%d retry backlog=%d serviced=%d/%d lock deferrals=%d", elapsed.Round(time.Second), progress.examined, backlog, progress.pending, progress.serviced, controllers, progress.deferrals-deferralsBefore)
			nextReport += 30 * time.Second
		}
	}
	sustained := time.Since(started)
	progress := readProgress()
	rate := float64(progress.examined) / sustained.Seconds()
	require.Greater(t, rate, 2*float64(controllers)/arrivalInterval.Seconds(), "forward capacity must exceed arrival rate with at least 100%% drain margin")
	require.Less(t, expectedForward-progress.examined, controllers*forwardPerController, "backlog must shrink during arrivals")
	require.LessOrEqual(t, peakBacklog, controllers*(forwardPerController+1), "backlog must stay bounded under arrivals")
	require.Equal(t, controllers*flushes/flushesPerPoll, arrivals, "each controller must contribute one arrival per poll cycle")
	t.Logf("sustained forward rate %.2f/s versus %.2f/s arrivals; maximum ingestion transaction %s; lock deferrals during arrivals=%d (probe excluded)", rate, float64(controllers)/arrivalInterval.Seconds(), maxIngest, progress.deferrals-deferralsBefore)
	deadline := time.Now().Add(drainDeadline)
	for progress.examined < expectedForward || progress.pending > 0 {
		require.True(t, time.Now().Before(deadline), "backlog failed to drain: examined=%d pending=%d", progress.examined, progress.pending)
		waitForTick()
		progress = readProgress()
		require.Zero(t, progress.failures)
	}
	// A lock deferral after progress leaves the controller due behind older work.
	// Require eventual service within the drain bound, not before arrivals stop.
	require.Equal(t, controllers, progress.serviced, "every controller must receive service within the drain deadline")
	dbtest.PauseBackgroundJobs(t, database)
	var retained, recentReceipts, oldReceipts, jobFailures int
	require.NoError(t, database.QueryRowContext(t.Context(), `SELECT count(*) FROM container_tank_metrics`).Scan(&retained))
	require.Equal(t, controllers*tanks*historyHours, retained, "all compressed samples and FK targets must survive")
	require.NoError(t, database.QueryRowContext(t.Context(), `SELECT count(*) FILTER(WHERE collected_at>=$1),
 count(*) FILTER(WHERE collected_at<now()-INTERVAL '10 days') FROM container_topology_observation`, recent.Add(historyHours*time.Hour)).Scan(&recentReceipts, &oldReceipts))
	require.Equal(t, arrivals, recentReceipts, "current ingestion must remain exact")
	require.Equal(t, controllers, oldReceipts, "only one historical span anchor per controller should remain")
	require.NoError(t, database.QueryRowContext(t.Context(), `SELECT total_failures FROM timescaledb_information.job_stats WHERE job_id=$1`, job).Scan(&jobFailures))
	require.Zero(t, jobFailures, "background timeouts/crashes must not be mistaken for successful progress")
	var slow int
	require.NoError(t, database.QueryRowContext(t.Context(), `SELECT count(*) FROM container_topology_compaction_state WHERE last_duration_ms>=2000`).Scan(&slow))
	require.Zero(t, slow)
	t.Logf("drained %d forward receipts and %d retries across %d controllers in %s; retained %d compressed samples",
		progress.examined, controllers*retriesPerController, controllers, time.Since(started).Round(time.Second), retained)
}
