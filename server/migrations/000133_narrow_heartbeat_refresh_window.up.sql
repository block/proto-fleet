-- 000082 set start_offset to 1 day, so every 30s refresh re-scanned roughly a day of notification_metric_sample; since 000121 cut chunks to 1 hour and compresses after 4 hours, most of that window is compressed chunks. Narrowed to 30 minutes so a refresh stays inside the uncompressed region. Only protofleet-ingest-stalled reads this aggregate and it reads max(bucket), so no consumer needs the wider backfill.
-- Replacing the job also clears a paused job_status: add_continuous_aggregate_policy creates the new job scheduled.

-- delete_job over a lookup rather than remove_continuous_aggregate_policy: the spelling of that function's if-exists argument differs across TimescaleDB versions. Matching both the view name and the materialization hypertable name covers either way timescaledb_information.jobs labels a refresh policy.
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
    start_offset => INTERVAL '30 minutes',
    end_offset => INTERVAL '1 minute',
    schedule_interval => INTERVAL '30 seconds');
