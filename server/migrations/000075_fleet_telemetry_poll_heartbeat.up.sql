-- Heartbeat continuous aggregate for the protofleet-ingest-stalled alert.

CREATE MATERIALIZED VIEW fleet_telemetry_poll_heartbeat
WITH (timescaledb.continuous, timescaledb.materialized_only = true) AS
SELECT
    time_bucket(INTERVAL '1 minute', time) AS bucket,
    organization_id,
    count(*)::bigint AS sample_count
FROM notification_metric_sample
WHERE metric = 'fleet_telemetry_poll_total'
  AND organization_id <> ''
GROUP BY bucket, organization_id
WITH NO DATA;

SELECT add_continuous_aggregate_policy('fleet_telemetry_poll_heartbeat',
    start_offset => INTERVAL '1 day',
    end_offset => INTERVAL '1 minute',
    schedule_interval => INTERVAL '30 seconds');

CREATE INDEX idx_fleet_telemetry_poll_heartbeat_org_bucket
    ON fleet_telemetry_poll_heartbeat (organization_id, bucket DESC);
