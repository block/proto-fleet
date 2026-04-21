-- Proto Fleet Status Aggregates
-- Stores temperature histogram and uptime status for flexible threshold configuration
-- Temperature histogram allows computing Cold/Ok/Hot/Critical at query time with configurable thresholds

-- =====================================================
-- Hourly status aggregates with temperature histogram
-- Used for 24h-10d dashboard queries
-- =====================================================
CREATE MATERIALIZED VIEW device_status_hourly
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 hour', time) AS bucket,
    device_identifier,
    -- Temperature histogram (10°C buckets for flexibility)
    -- All buckets explicitly check for NOT NULL to ensure consistent handling
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
    -- Uptime status counts
    SUM(CASE WHEN health = 'health_healthy_active' THEN 1 ELSE 0 END)::int AS hashing_count,
    SUM(CASE WHEN health IS NULL OR health != 'health_healthy_active' THEN 1 ELSE 0 END)::int AS not_hashing_count,
    COUNT(*)::int AS data_points
FROM device_metrics
GROUP BY bucket, device_identifier
WITH NO DATA;

-- Refresh policy rationale (matches device_metrics_hourly for consistency):
-- - start_offset: 1 day lookback catches late-arriving data and backfills gaps
-- - end_offset: 1 hour excludes incomplete current hour still receiving data
-- - schedule_interval: 30 min balances freshness with DB load (used for 24h-10d queries)
SELECT add_continuous_aggregate_policy('device_status_hourly',
    start_offset => INTERVAL '1 day',
    end_offset => INTERVAL '1 hour',
    schedule_interval => INTERVAL '30 minutes');

-- Create index for efficient queries (match existing pattern)
CREATE INDEX idx_device_status_hourly_device ON device_status_hourly(device_identifier, bucket DESC);

-- =====================================================
-- Daily status aggregates with temperature histogram
-- Used for >10d dashboard queries (14d, 30d, 90d, 1y)
-- =====================================================
CREATE MATERIALIZED VIEW device_status_daily
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 day', time) AS bucket,
    device_identifier,
    -- Temperature histogram (same structure as hourly)
    -- All buckets explicitly check for NOT NULL to ensure consistent handling
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
    -- Uptime status counts
    SUM(CASE WHEN health = 'health_healthy_active' THEN 1 ELSE 0 END)::int AS hashing_count,
    SUM(CASE WHEN health IS NULL OR health != 'health_healthy_active' THEN 1 ELSE 0 END)::int AS not_hashing_count,
    COUNT(*)::int AS data_points
FROM device_metrics
GROUP BY bucket, device_identifier
WITH NO DATA;

-- Refresh policy rationale (matches device_metrics_daily for consistency):
-- - start_offset: 7 day lookback catches late data and timezone edge cases
-- - end_offset: 1 day excludes incomplete current day still receiving data
-- - schedule_interval: 6 hours is sufficient for >10d queries where staleness is less critical
SELECT add_continuous_aggregate_policy('device_status_daily',
    start_offset => INTERVAL '7 days',
    end_offset => INTERVAL '1 day',
    schedule_interval => INTERVAL '6 hours');

-- Create index for efficient queries (match existing pattern)
CREATE INDEX idx_device_status_daily_device ON device_status_daily(device_identifier, bucket DESC);
