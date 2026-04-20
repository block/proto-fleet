-- Drop continuous aggregate policies first
SELECT remove_continuous_aggregate_policy('device_status_daily', if_exists => true);
SELECT remove_continuous_aggregate_policy('device_status_hourly', if_exists => true);

-- Drop continuous aggregates
DROP MATERIALIZED VIEW IF EXISTS device_status_daily;
DROP MATERIALIZED VIEW IF EXISTS device_status_hourly;
