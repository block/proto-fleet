SELECT remove_retention_policy('device_metrics', if_exists => true);
SELECT add_retention_policy('device_metrics', INTERVAL '8 days',
    schedule_interval => INTERVAL '6 hours');

SELECT remove_continuous_aggregate_policy('device_metrics_daily', if_exists => true);
SELECT remove_continuous_aggregate_policy('device_metrics_hourly', if_exists => true);
SELECT remove_continuous_aggregate_policy('device_status_daily', if_exists => true);

DROP INDEX IF EXISTS idx_device_metrics_daily_device;
DROP INDEX IF EXISTS idx_device_metrics_hourly_device;
DROP INDEX IF EXISTS idx_device_status_daily_device;

DROP MATERIALIZED VIEW IF EXISTS device_metrics_daily;
DROP MATERIALIZED VIEW IF EXISTS device_metrics_hourly;
DROP MATERIALIZED VIEW IF EXISTS device_status_daily;

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

CREATE MATERIALIZED VIEW device_status_daily
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 day', time) AS bucket,
    device_identifier,
    SUM(CASE WHEN temp_c IS NOT NULL AND temp_c < 0 THEN 1 ELSE 0 END)::int AS temp_below_0,
    SUM(CASE WHEN temp_c IS NOT NULL AND temp_c >= 0 AND temp_c < 10 THEN 1 ELSE 0 END)::int AS temp_0_10,
    SUM(CASE WHEN temp_c IS NOT NULL AND temp_c >= 10 AND temp_c < 20 THEN 1 ELSE 0 END)::int AS temp_10_20,
    SUM(CASE WHEN temp_c IS NOT NULL AND temp_c >= 20 AND temp_c < 30 THEN 1 ELSE 0 END)::int AS temp_20_30,
    SUM(CASE WHEN temp_c IS NOT NULL AND temp_c >= 30 AND temp_c < 40 THEN 1 ELSE 0 END)::int AS temp_30_40,
    SUM(CASE WHEN temp_c IS NOT NULL AND temp_c >= 40 AND temp_c < 50 THEN 1 ELSE 0 END)::int AS temp_40_50,
    SUM(CASE WHEN temp_c IS NOT NULL AND temp_c >= 50 AND temp_c < 60 THEN 1 ELSE 0 END)::int AS temp_50_60,
    SUM(CASE WHEN temp_c IS NOT NULL AND temp_c >= 60 AND temp_c < 70 THEN 1 ELSE 0 END)::int AS temp_60_70,
    SUM(CASE WHEN temp_c IS NOT NULL AND temp_c >= 70 AND temp_c < 80 THEN 1 ELSE 0 END)::int AS temp_70_80,
    SUM(CASE WHEN temp_c IS NOT NULL AND temp_c >= 80 AND temp_c < 90 THEN 1 ELSE 0 END)::int AS temp_80_90,
    SUM(CASE WHEN temp_c IS NOT NULL AND temp_c >= 90 AND temp_c < 100 THEN 1 ELSE 0 END)::int AS temp_90_100,
    SUM(CASE WHEN temp_c IS NOT NULL AND temp_c >= 100 THEN 1 ELSE 0 END)::int AS temp_100_plus,
    SUM(CASE WHEN health = 'health_healthy_active' THEN 1 ELSE 0 END)::int AS hashing_count,
    SUM(CASE WHEN health IS NULL OR health != 'health_healthy_active' THEN 1 ELSE 0 END)::int AS not_hashing_count,
    COUNT(*)::int AS data_points
FROM device_metrics
GROUP BY bucket, device_identifier
WITH NO DATA;

SELECT add_continuous_aggregate_policy('device_status_daily',
    start_offset => INTERVAL '7 days',
    end_offset => INTERVAL '1 day',
    schedule_interval => INTERVAL '6 hours');

CREATE INDEX idx_device_status_daily_device
    ON device_status_daily(device_identifier, bucket DESC);
