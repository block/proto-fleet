-- Restore the previous cadence without changing enablement or deleting progress.
SELECT alter_job(job_id, schedule_interval => INTERVAL '10 seconds')
FROM timescaledb_information.jobs
WHERE proc_schema = 'public' AND proc_name = 'compact_container_topology_job';
