-- Proto Fleet PostgreSQL Continuous Aggregates
-- Pre-computed aggregations for dashboard performance

-- Enable TimescaleDB Toolkit for time-weighted aggregates
-- Provides accurate energy calculations for irregular sampling intervals
-- Requires timescale/timescaledb-ha image (not the basic timescaledb image)
CREATE EXTENSION IF NOT EXISTS timescaledb_toolkit;

-- =====================================================
-- Hourly device metrics aggregate
-- Used for hourly dashboard charts
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

-- Add refresh policy: refresh every 30 minutes, keep real-time data for last hour
SELECT add_continuous_aggregate_policy('device_metrics_hourly',
    start_offset => INTERVAL '1 day',
    end_offset => INTERVAL '1 hour',
    schedule_interval => INTERVAL '30 minutes');

-- =====================================================
-- Daily device metrics aggregate
-- Used for long-term dashboards and reports
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
    SUM(power_w) / COUNT(*) * 24 AS energy_kwh_estimate,
    AVG(efficiency_jh) AS avg_efficiency,
    COUNT(*) AS data_points
FROM device_metrics
GROUP BY bucket, device_identifier
WITH NO DATA;

-- Add refresh policy: refresh every 6 hours
SELECT add_continuous_aggregate_policy('device_metrics_daily',
    start_offset => INTERVAL '7 days',
    end_offset => INTERVAL '1 day',
    schedule_interval => INTERVAL '6 hours');

-- =====================================================
-- Create indexes on continuous aggregates for faster queries
-- =====================================================
CREATE INDEX idx_device_metrics_hourly_device ON device_metrics_hourly(device_identifier, bucket DESC);
CREATE INDEX idx_device_metrics_daily_device ON device_metrics_daily(device_identifier, bucket DESC);
