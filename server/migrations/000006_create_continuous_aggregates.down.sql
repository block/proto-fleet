-- Drop continuous aggregate policies first
SELECT remove_continuous_aggregate_policy('device_metrics_daily', if_exists => true);
SELECT remove_continuous_aggregate_policy('device_metrics_hourly', if_exists => true);

-- Drop continuous aggregates
DROP MATERIALIZED VIEW IF EXISTS device_metrics_daily;
DROP MATERIALIZED VIEW IF EXISTS device_metrics_hourly;

-- Note: We don't drop timescaledb_toolkit extension as it may be used by other schemas
-- DROP EXTENSION IF EXISTS timescaledb_toolkit;
