-- Keep the existing job IDs, schedules, and operator settings.
DO $$
DECLARE
    updated_jobs integer;
BEGIN
    PERFORM alter_job(job_id, config => config || jsonb_build_object(
        CASE proc_name
            WHEN 'policy_refresh_continuous_aggregate' THEN 'start_offset'
            ELSE 'drop_after'
        END, INTERVAL '14 days'))
    FROM timescaledb_information.jobs
    WHERE hypertable_schema = 'public'
      AND hypertable_name = 'miner_state_snapshot_device_1m'
      AND proc_name IN ('policy_refresh_continuous_aggregate', 'policy_retention');

    GET DIAGNOSTICS updated_jobs = ROW_COUNT;
    IF updated_jobs <> 2 THEN
        RAISE EXCEPTION 'expected one refresh and one retention job for miner_state_snapshot_device_1m';
    END IF;
END;
$$;
