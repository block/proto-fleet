-- Recreate metrics continuous aggregates without energy_kwh_estimate.
-- TimescaleDB continuous aggregates cannot be ALTERed in place, so we must
-- drop and recreate them. Data repopulates via refresh policies (hourly: ~30min,
-- daily: ~6h). Raw data queries (<=24h) are unaffected.

-- Drop existing policies and views (daily depends on hourly ordering doesn't matter,
-- but drop daily first for clarity)
SELECT remove_continuous_aggregate_policy('device_metrics_daily', if_exists => true);
SELECT remove_continuous_aggregate_policy('device_metrics_hourly', if_exists => true);

DROP INDEX IF EXISTS idx_device_metrics_daily_device;
DROP INDEX IF EXISTS idx_device_metrics_hourly_device;

DROP MATERIALIZED VIEW IF EXISTS device_metrics_daily;
DROP MATERIALIZED VIEW IF EXISTS device_metrics_hourly;

-- =====================================================
-- Hourly device metrics aggregate
-- =====================================================
CREATE MATERIALIZED VIEW device_metrics_hourly
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 hour', time) AS bucket,
    device_identifier,
    AVG(hash_rate_hs) AS avg_hash_rate,
    MAX(hash_rate_hs) AS max_hash_rate,
    MIN(hash_rate_hs) AS min_hash_rate,
    AVG(temp_c) AS avg_temp,
    MAX(temp_c) AS max_temp,
    MIN(temp_c) AS min_temp,
    AVG(fan_rpm) AS avg_fan_rpm,
    AVG(power_w) AS avg_power,
    SUM(power_w) AS total_power,
    AVG(efficiency_jh) AS avg_efficiency,
    COUNT(*) AS data_points
FROM device_metrics
GROUP BY bucket, device_identifier
WITH NO DATA;

SELECT add_continuous_aggregate_policy('device_metrics_hourly',
    start_offset => INTERVAL '1 day',
    end_offset => INTERVAL '1 hour',
    schedule_interval => INTERVAL '30 minutes');

CREATE INDEX idx_device_metrics_hourly_device
    ON device_metrics_hourly(device_identifier, bucket DESC);

-- =====================================================
-- Daily device metrics aggregate
-- =====================================================
CREATE MATERIALIZED VIEW device_metrics_daily
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 day', time) AS bucket,
    device_identifier,
    AVG(hash_rate_hs) AS avg_hash_rate,
    MAX(hash_rate_hs) AS max_hash_rate,
    MIN(hash_rate_hs) AS min_hash_rate,
    AVG(temp_c) AS avg_temp,
    MAX(temp_c) AS max_temp,
    MIN(temp_c) AS min_temp,
    AVG(power_w) AS avg_power,
    AVG(efficiency_jh) AS avg_efficiency,
    COUNT(*) AS data_points
FROM device_metrics
GROUP BY bucket, device_identifier
WITH NO DATA;

SELECT add_continuous_aggregate_policy('device_metrics_daily',
    start_offset => INTERVAL '7 days',
    end_offset => INTERVAL '1 day',
    schedule_interval => INTERVAL '6 hours');

CREATE INDEX idx_device_metrics_daily_device
    ON device_metrics_daily(device_identifier, bucket DESC);

-- Re-add retention policies that were removed when the views were dropped.
-- These match the values from 000007_add_retention_policies.
SELECT add_retention_policy('device_metrics_hourly', INTERVAL '3 months');
SELECT add_retention_policy('device_metrics_daily', INTERVAL '3 years');
