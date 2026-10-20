-- requires-shared: 1000
-- Keep one worker and bounded transactions. At 50 new receipts per run, the
-- former ten-second schedule cannot keep up with 100 controllers polling every
-- fifteen seconds. A one-second interval leaves capacity to drain a backlog
-- without increasing the batch cap or contending with parallel cleanup workers.
-- Preserve the rollout flag, scheduling state, runtime limit and retry policy.
SELECT alter_job(job_id, schedule_interval => INTERVAL '1 second')
FROM timescaledb_information.jobs
WHERE proc_schema = 'public' AND proc_name = 'compact_container_topology_job';
