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
    COUNT(hash_rate_hs)::bigint AS hash_rate_points,
    AVG(temp_c) AS avg_temp,
    MAX(temp_c) AS max_temp,
    MIN(temp_c) AS min_temp,
    COUNT(temp_c)::bigint AS temp_points,
    AVG(fan_rpm) AS avg_fan_rpm,
    AVG(power_w) AS avg_power,
    SUM(power_w) AS total_power,
    COUNT(power_w)::bigint AS power_points,
    AVG(efficiency_jh) AS avg_efficiency,
    COUNT(efficiency_jh)::bigint AS efficiency_points,
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
    time_bucket('1 day', bucket) AS bucket,
    device_identifier,
    (SUM(avg_hash_rate * hash_rate_points)::double precision / NULLIF(SUM(hash_rate_points), 0)::double precision)::double precision AS avg_hash_rate,
    MAX(max_hash_rate) AS max_hash_rate,
    MIN(min_hash_rate) AS min_hash_rate,
    (SUM(avg_temp * temp_points)::double precision / NULLIF(SUM(temp_points), 0)::double precision)::double precision AS avg_temp,
    MAX(max_temp) AS max_temp,
    MIN(min_temp) AS min_temp,
    (SUM(avg_power * power_points)::double precision / NULLIF(SUM(power_points), 0)::double precision)::double precision AS avg_power,
    (SUM(avg_efficiency * efficiency_points)::double precision / NULLIF(SUM(efficiency_points), 0)::double precision)::double precision AS avg_efficiency,
    SUM(data_points)::bigint AS data_points
FROM device_metrics_hourly
GROUP BY 1, device_identifier
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
    time_bucket('1 day', bucket) AS bucket,
    device_identifier,
    SUM(temp_below_0)::int AS temp_below_0,
    SUM(temp_0_10)::int AS temp_0_10,
    SUM(temp_10_20)::int AS temp_10_20,
    SUM(temp_20_30)::int AS temp_20_30,
    SUM(temp_30_40)::int AS temp_30_40,
    SUM(temp_40_50)::int AS temp_40_50,
    SUM(temp_50_60)::int AS temp_50_60,
    SUM(temp_60_70)::int AS temp_60_70,
    SUM(temp_70_80)::int AS temp_70_80,
    SUM(temp_80_90)::int AS temp_80_90,
    SUM(temp_90_100)::int AS temp_90_100,
    SUM(temp_100_plus)::int AS temp_100_plus,
    SUM(hashing_count)::int AS hashing_count,
    SUM(not_hashing_count)::int AS not_hashing_count,
    SUM(data_points)::int AS data_points
FROM device_status_hourly
GROUP BY 1, device_identifier
WITH NO DATA;

SELECT add_continuous_aggregate_policy('device_status_daily',
    start_offset => INTERVAL '7 days',
    end_offset => INTERVAL '1 day',
    schedule_interval => INTERVAL '6 hours');

CREATE INDEX idx_device_status_daily_device
    ON device_status_daily(device_identifier, bucket DESC);

SELECT remove_retention_policy('device_metrics', if_exists => true);
SELECT add_retention_policy('device_metrics', INTERVAL '3 days',
    schedule_interval => INTERVAL '6 hours');
