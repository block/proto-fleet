-- Restores 000082's 1-day start_offset.
DO $$
DECLARE
    refresh_job_id integer;
BEGIN
    FOR refresh_job_id IN
        SELECT j.job_id
        FROM timescaledb_information.jobs j
        WHERE j.proc_name = 'policy_refresh_continuous_aggregate'
          AND j.hypertable_name IN (
              SELECT ca.view_name
              FROM timescaledb_information.continuous_aggregates ca
              WHERE ca.view_name = 'fleet_telemetry_poll_heartbeat'
              UNION ALL
              SELECT ca.materialization_hypertable_name
              FROM timescaledb_information.continuous_aggregates ca
              WHERE ca.view_name = 'fleet_telemetry_poll_heartbeat'
          )
    LOOP
        PERFORM public.delete_job(refresh_job_id);
    END LOOP;
END $$;

SELECT add_continuous_aggregate_policy('fleet_telemetry_poll_heartbeat',
    start_offset => INTERVAL '1 day',
    end_offset => INTERVAL '1 minute',
    schedule_interval => INTERVAL '30 seconds');
