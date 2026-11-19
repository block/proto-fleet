DO $$
DECLARE
    retention_job_id integer;
    retention_config jsonb;
BEGIN
    SELECT job_id, config INTO STRICT retention_job_id, retention_config
    FROM timescaledb_information.jobs
    WHERE hypertable_schema = 'public'
      AND hypertable_name = 'notification_metric_sample'
      AND proc_name = 'policy_retention';

    PERFORM alter_job(retention_job_id,
        config => retention_config || jsonb_build_object('drop_after', INTERVAL '7 days'));
END;
$$;
