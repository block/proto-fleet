-- requires-shared: 1000
-- Update the existing retention jobs in place to keep their schedules.
DO $$
DECLARE
    diagnostic_table text;
    retention_job_id integer;
    retention_config jsonb;
BEGIN
    FOREACH diagnostic_table IN ARRAY ARRAY['hashboard_diagnostic_observation', 'container_module_diagnostic_observation']
    LOOP
        SELECT job_id, config INTO STRICT retention_job_id, retention_config
        FROM timescaledb_information.jobs
        WHERE hypertable_schema = 'public'
          AND hypertable_name = diagnostic_table
          AND proc_name = 'policy_retention';

        PERFORM alter_job(retention_job_id,
            config => retention_config || jsonb_build_object('drop_after', INTERVAL '14 days'));
    END LOOP;
END;
$$;
