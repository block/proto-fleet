SELECT remove_retention_policy('fleet_telemetry_poll_heartbeat', if_exists => true);
SELECT remove_continuous_aggregate_policy('fleet_telemetry_poll_heartbeat', if_exists => true);
DROP MATERIALIZED VIEW IF EXISTS fleet_telemetry_poll_heartbeat;
