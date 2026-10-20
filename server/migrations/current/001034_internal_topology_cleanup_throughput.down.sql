-- Restore the original fixed cadence without changing enablement or progress.
SELECT alter_job(job_id, schedule_interval => INTERVAL '10 seconds', fixed_schedule => true)
FROM timescaledb_information.jobs
WHERE proc_schema = 'public' AND proc_name = 'compact_container_topology_job';
