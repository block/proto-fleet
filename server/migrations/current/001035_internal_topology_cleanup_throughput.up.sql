-- requires-shared: 1000
-- Keep one worker and bounded transactions. At 50 new receipts per run, the
-- former ten-second schedule cannot keep up with 100 controllers polling every
-- fifteen seconds. A one-second interval leaves capacity to drain a backlog
-- without increasing the batch cap or contending with parallel cleanup workers.
-- Drifting scheduling leaves a full interval after success and avoids snapping
-- failed runs to the next one-second slot. Timescale still caps ordinary failure
-- backoff at five intervals; the committed per-controller lease remains separate.
-- Preserve the rollout flag, scheduling state, runtime limit and retry setting.
SELECT alter_job(job_id, schedule_interval => INTERVAL '1 second', fixed_schedule => false)
FROM timescaledb_information.jobs
WHERE proc_schema = 'public' AND proc_name = 'compact_container_topology_job';
