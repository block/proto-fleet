-- Telemetry queries for device_metrics table and continuous aggregates
-- Note: All device identification uses device_identifier (TEXT), not device_id (BIGINT)

-- name: InsertDeviceMetrics :exec
-- site_id is row-stamped from device.site_id (looked up by
-- device_identifier) so per-site telemetry filters use the row-stamped
-- site even after the device is reassigned. Inline sub-select rather
-- than a CTE+SELECT INSERT — ON CONFLICT on the device_metrics
-- hypertable PK requires VALUES-shape INSERT. The sub-select does NOT
-- filter by deleted_at: telemetry from a soft-deleted device is still
-- legitimate per-site history, matching InsertError /
-- InsertMinerStateSnapshot which also stamp from the device row
-- regardless of soft-delete state. Duplicate historical identifiers are
-- resolved deterministically, preferring the live device row.
INSERT INTO device_metrics (
    time,
    device_identifier,
    hash_rate_hs,
    hash_rate_hs_kind,
    temp_c,
    temp_c_kind,
    fan_rpm,
    fan_rpm_kind,
    power_w,
    power_w_kind,
    efficiency_jh,
    efficiency_jh_kind,
    voltage_v,
    voltage_v_kind,
    current_a,
    current_a_kind,
    inlet_temp_c,
    outlet_temp_c,
    ambient_temp_c,
    chip_count,
    chip_count_kind,
    chip_frequency_mhz,
    health,
    site_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
    $11, $12, $13, $14, $15, $16, $17, $18, $19, $20,
    $21, $22, $23,
    (
        SELECT site_id
        FROM device
        WHERE device_identifier = $2
        ORDER BY (deleted_at IS NULL) DESC, updated_at DESC, id DESC
        LIMIT 1
    )
) ON CONFLICT (time, device_identifier) DO NOTHING;

-- name: GetLatestDeviceMetrics :many
SELECT
    dm.time,
    dm.device_identifier,
    dm.hash_rate_hs,
    dm.hash_rate_hs_kind,
    dm.temp_c,
    dm.temp_c_kind,
    dm.fan_rpm,
    dm.fan_rpm_kind,
    dm.power_w,
    dm.power_w_kind,
    dm.efficiency_jh,
    dm.efficiency_jh_kind,
    dm.voltage_v,
    dm.voltage_v_kind,
    dm.current_a,
    dm.current_a_kind,
    dm.inlet_temp_c,
    dm.outlet_temp_c,
    dm.ambient_temp_c,
    dm.chip_count,
    dm.chip_count_kind,
    dm.chip_frequency_mhz,
    dm.health,
    dm.site_id
FROM unnest(sqlc.arg('device_identifiers')::text[]) AS ids(device_identifier)
CROSS JOIN LATERAL (
    SELECT *
    FROM device_metrics
    WHERE device_metrics.device_identifier = ids.device_identifier
      AND device_metrics.time >= $1
    ORDER BY device_metrics.time DESC
    LIMIT 1
) dm;

-- name: GetLatestAllDeviceMetrics :many
SELECT DISTINCT ON (device_identifier)
    time,
    device_identifier,
    hash_rate_hs,
    hash_rate_hs_kind,
    temp_c,
    temp_c_kind,
    fan_rpm,
    fan_rpm_kind,
    power_w,
    power_w_kind,
    efficiency_jh,
    efficiency_jh_kind,
    voltage_v,
    voltage_v_kind,
    current_a,
    current_a_kind,
    inlet_temp_c,
    outlet_temp_c,
    ambient_temp_c,
    chip_count,
    chip_count_kind,
    chip_frequency_mhz,
    health,
    site_id
FROM device_metrics
WHERE time >= $1
ORDER BY device_identifier, time DESC;

-- name: GetDeviceMetricsTimeSeries :many
SELECT
    time,
    device_identifier,
    hash_rate_hs,
    hash_rate_hs_kind,
    temp_c,
    temp_c_kind,
    fan_rpm,
    fan_rpm_kind,
    power_w,
    power_w_kind,
    efficiency_jh,
    efficiency_jh_kind,
    voltage_v,
    voltage_v_kind,
    current_a,
    current_a_kind,
    inlet_temp_c,
    outlet_temp_c,
    ambient_temp_c,
    chip_count,
    chip_count_kind,
    chip_frequency_mhz,
    health,
    site_id
FROM device_metrics
WHERE device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND time >= $1
  AND time <= $2
ORDER BY time ASC
LIMIT sqlc.arg('max_rows')::int;

-- name: GetDeviceMetricsTimeSeriesByTimeScan :many
-- Large explicit device selectors, such as building pages with thousands of
-- miners, can make Postgres choose thousands of device_identifier index scans
-- and then sort the result. This variant intentionally makes the device
-- predicate non-indexable so the planner walks the time-ordered index once,
-- applies the in-memory device filter, and stops at max_rows.
SELECT
    time,
    device_identifier,
    hash_rate_hs,
    hash_rate_hs_kind,
    temp_c,
    temp_c_kind,
    fan_rpm,
    fan_rpm_kind,
    power_w,
    power_w_kind,
    efficiency_jh,
    efficiency_jh_kind,
    voltage_v,
    voltage_v_kind,
    current_a,
    current_a_kind,
    inlet_temp_c,
    outlet_temp_c,
    ambient_temp_c,
    chip_count,
    chip_count_kind,
    chip_frequency_mhz,
    health,
    site_id
FROM device_metrics
WHERE (device_identifier || '') = ANY(sqlc.arg('device_identifiers')::text[])
  AND time >= $1
  AND time <= $2
ORDER BY time ASC
LIMIT sqlc.arg('max_rows')::int;

-- name: GetDeviceMetricsTimeSeriesAggregatesByTimeScan :many
-- Large raw combined-metrics selectors should not stream thousands of samples
-- into Go only to collapse them into chart buckets. This preserves the
-- time-ordered max_rows window from GetDeviceMetricsTimeSeriesByTimeScan, then
-- performs the same bucket-level aggregation in Postgres.
WITH limited_metrics AS (
    SELECT
        time_bucket(sqlc.arg('bucket_interval')::text::interval, dm.time)::timestamptz AS bucket,
        dm.time,
        dm.device_identifier,
        dm.hash_rate_hs,
        dm.temp_c,
        dm.fan_rpm,
        dm.power_w,
        dm.efficiency_jh
    FROM device_metrics dm
    WHERE (dm.device_identifier || '') = ANY(sqlc.arg('device_identifiers')::text[])
      AND dm.time >= sqlc.arg('start_time')
      AND dm.time <= sqlc.arg('end_time')
    ORDER BY dm.time ASC
    LIMIT sqlc.arg('max_rows')::int
),
per_device AS (
    SELECT
        bucket,
        device_identifier,
        AVG(hash_rate_hs) AS hash_rate_avg,
        MIN(hash_rate_hs) AS hash_rate_min,
        MAX(hash_rate_hs) AS hash_rate_max,
        (ARRAY_AGG(hash_rate_hs ORDER BY time DESC) FILTER (WHERE hash_rate_hs IS NOT NULL))[1] AS hash_rate_latest,
        AVG(power_w) AS power_avg,
        MIN(power_w) AS power_min,
        MAX(power_w) AS power_max,
        (ARRAY_AGG(power_w ORDER BY time DESC) FILTER (WHERE power_w IS NOT NULL))[1] AS power_latest
    FROM limited_metrics
    GROUP BY bucket, device_identifier
),
cumulative_metrics AS (
    SELECT
        bucket,
        SUM(hash_rate_avg) AS hash_rate_avg,
        SUM(hash_rate_min) AS hash_rate_min,
        SUM(hash_rate_max) AS hash_rate_max,
        SUM(hash_rate_latest) AS hash_rate_sum,
        COUNT(hash_rate_avg)::int AS hash_rate_device_count,
        SUM(power_avg) AS power_avg,
        SUM(power_min) AS power_min,
        SUM(power_max) AS power_max,
        SUM(power_latest) AS power_sum,
        COUNT(power_avg)::int AS power_device_count
    FROM per_device
    GROUP BY bucket
),
non_cumulative_metrics AS (
    SELECT
        bucket,
        AVG(temp_c) AS temp_avg,
        MIN(temp_c) AS temp_min,
        MAX(temp_c) AS temp_max,
        SUM(temp_c) AS temp_sum,
        COUNT(temp_c)::int AS temp_sample_count,
        COUNT(DISTINCT device_identifier) FILTER (WHERE temp_c IS NOT NULL)::int AS temp_device_count,
        AVG(efficiency_jh) AS efficiency_avg,
        MIN(efficiency_jh) AS efficiency_min,
        MAX(efficiency_jh) AS efficiency_max,
        SUM(efficiency_jh) AS efficiency_sum,
        COUNT(efficiency_jh)::int AS efficiency_sample_count,
        COUNT(DISTINCT device_identifier) FILTER (WHERE efficiency_jh IS NOT NULL)::int AS efficiency_device_count,
        AVG(fan_rpm) AS fan_rpm_avg,
        MIN(fan_rpm) AS fan_rpm_min,
        MAX(fan_rpm) AS fan_rpm_max,
        SUM(fan_rpm) AS fan_rpm_sum,
        COUNT(fan_rpm)::int AS fan_rpm_sample_count,
        COUNT(DISTINCT device_identifier) FILTER (WHERE fan_rpm IS NOT NULL)::int AS fan_rpm_device_count
    FROM limited_metrics
    GROUP BY bucket
),
latest_temp_per_device AS (
    SELECT DISTINCT ON (bucket, device_identifier)
        bucket,
        device_identifier,
        temp_c
    FROM limited_metrics
    WHERE temp_c IS NOT NULL
    ORDER BY bucket, device_identifier, time DESC
),
temperature_status AS (
    SELECT
        bucket,
        SUM(CASE WHEN temp_c < 0 THEN 1 ELSE 0 END)::int AS temp_cold_count,
        SUM(CASE WHEN temp_c >= 0 AND temp_c < 70 THEN 1 ELSE 0 END)::int AS temp_ok_count,
        SUM(CASE WHEN temp_c >= 70 AND temp_c < 90 THEN 1 ELSE 0 END)::int AS temp_hot_count,
        SUM(CASE WHEN temp_c >= 90 THEN 1 ELSE 0 END)::int AS temp_critical_count
    FROM latest_temp_per_device
    GROUP BY bucket
),
buckets AS (
    SELECT DISTINCT bucket
    FROM limited_metrics
)
SELECT
    b.bucket,
    COALESCE(cm.hash_rate_avg, 0)::double precision AS hash_rate_avg,
    COALESCE(cm.hash_rate_min, 0)::double precision AS hash_rate_min,
    COALESCE(cm.hash_rate_max, 0)::double precision AS hash_rate_max,
    COALESCE(cm.hash_rate_sum, 0)::double precision AS hash_rate_sum,
    COALESCE(cm.hash_rate_device_count, 0)::int AS hash_rate_device_count,
    COALESCE(cm.power_avg, 0)::double precision AS power_avg,
    COALESCE(cm.power_min, 0)::double precision AS power_min,
    COALESCE(cm.power_max, 0)::double precision AS power_max,
    COALESCE(cm.power_sum, 0)::double precision AS power_sum,
    COALESCE(cm.power_device_count, 0)::int AS power_device_count,
    COALESCE(nc.temp_avg, 0)::double precision AS temp_avg,
    COALESCE(nc.temp_min, 0)::double precision AS temp_min,
    COALESCE(nc.temp_max, 0)::double precision AS temp_max,
    COALESCE(nc.temp_sum, 0)::double precision AS temp_sum,
    COALESCE(nc.temp_sample_count, 0)::int AS temp_sample_count,
    COALESCE(nc.temp_device_count, 0)::int AS temp_device_count,
    COALESCE(nc.efficiency_avg, 0)::double precision AS efficiency_avg,
    COALESCE(nc.efficiency_min, 0)::double precision AS efficiency_min,
    COALESCE(nc.efficiency_max, 0)::double precision AS efficiency_max,
    COALESCE(nc.efficiency_sum, 0)::double precision AS efficiency_sum,
    COALESCE(nc.efficiency_sample_count, 0)::int AS efficiency_sample_count,
    COALESCE(nc.efficiency_device_count, 0)::int AS efficiency_device_count,
    COALESCE(nc.fan_rpm_avg, 0)::double precision AS fan_rpm_avg,
    COALESCE(nc.fan_rpm_min, 0)::double precision AS fan_rpm_min,
    COALESCE(nc.fan_rpm_max, 0)::double precision AS fan_rpm_max,
    COALESCE(nc.fan_rpm_sum, 0)::double precision AS fan_rpm_sum,
    COALESCE(nc.fan_rpm_sample_count, 0)::int AS fan_rpm_sample_count,
    COALESCE(nc.fan_rpm_device_count, 0)::int AS fan_rpm_device_count,
    COALESCE(ts.temp_cold_count, 0)::int AS temp_cold_count,
    COALESCE(ts.temp_ok_count, 0)::int AS temp_ok_count,
    COALESCE(ts.temp_hot_count, 0)::int AS temp_hot_count,
    COALESCE(ts.temp_critical_count, 0)::int AS temp_critical_count
FROM buckets b
LEFT JOIN cumulative_metrics cm ON cm.bucket = b.bucket
LEFT JOIN non_cumulative_metrics nc ON nc.bucket = b.bucket
LEFT JOIN temperature_status ts ON ts.bucket = b.bucket
ORDER BY b.bucket ASC;

-- name: GetAllDeviceMetricsTimeSeries :many
-- Returns time series metrics for ALL devices within a time range.
-- Used when DeviceSelector_AllDevices is specified (empty device list).
SELECT
    time,
    device_identifier,
    hash_rate_hs,
    hash_rate_hs_kind,
    temp_c,
    temp_c_kind,
    fan_rpm,
    fan_rpm_kind,
    power_w,
    power_w_kind,
    efficiency_jh,
    efficiency_jh_kind,
    voltage_v,
    voltage_v_kind,
    current_a,
    current_a_kind,
    inlet_temp_c,
    outlet_temp_c,
    ambient_temp_c,
    chip_count,
    chip_count_kind,
    chip_frequency_mhz,
    health,
    site_id
FROM device_metrics
WHERE time >= $1
  AND time <= $2
ORDER BY time ASC
LIMIT sqlc.arg('max_rows')::int;

-- name: GetDeviceMetricsHourlyAggregates :many
-- COALESCE handles NULL values from AVG() when all source values are NULL
SELECT
    bucket,
    device_identifier,
    COALESCE(avg_hash_rate, 0) AS avg_hash_rate,
    max_hash_rate,
    min_hash_rate,
    COALESCE(avg_temp, 0) AS avg_temp,
    max_temp,
    min_temp,
    COALESCE(avg_fan_rpm, 0) AS avg_fan_rpm,
    COALESCE(avg_power, 0) AS avg_power,
    total_power,
    COALESCE(avg_efficiency, 0) AS avg_efficiency,
    data_points
FROM device_metrics_hourly
WHERE device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND bucket >= $1
  AND bucket <= $2
ORDER BY bucket ASC;

-- name: GetDeviceMetricsDailyAggregates :many
-- COALESCE handles NULL values from AVG() when all source values are NULL
SELECT
    bucket,
    device_identifier,
    COALESCE(avg_hash_rate, 0) AS avg_hash_rate,
    max_hash_rate,
    min_hash_rate,
    COALESCE(avg_temp, 0) AS avg_temp,
    max_temp,
    min_temp,
    COALESCE(avg_power, 0) AS avg_power,
    COALESCE(avg_efficiency, 0) AS avg_efficiency,
    data_points
FROM device_metrics_daily
WHERE device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND bucket >= $1
  AND bucket <= $2
ORDER BY bucket ASC;

-- name: GetAllDeviceMetricsHourlyAggregates :many
-- Returns hourly aggregates for ALL devices within a time range.
-- COALESCE handles NULL values from AVG() when all source values are NULL
SELECT
    bucket,
    device_identifier,
    COALESCE(avg_hash_rate, 0) AS avg_hash_rate,
    max_hash_rate,
    min_hash_rate,
    COALESCE(avg_temp, 0) AS avg_temp,
    max_temp,
    min_temp,
    COALESCE(avg_fan_rpm, 0) AS avg_fan_rpm,
    COALESCE(avg_power, 0) AS avg_power,
    total_power,
    COALESCE(avg_efficiency, 0) AS avg_efficiency,
    data_points
FROM device_metrics_hourly
WHERE bucket >= $1
  AND bucket <= $2
ORDER BY bucket ASC;

-- name: GetAllDeviceMetricsDailyAggregates :many
-- Returns daily aggregates for ALL devices within a time range.
-- COALESCE handles NULL values from AVG() when all source values are NULL
SELECT
    bucket,
    device_identifier,
    COALESCE(avg_hash_rate, 0) AS avg_hash_rate,
    max_hash_rate,
    min_hash_rate,
    COALESCE(avg_temp, 0) AS avg_temp,
    max_temp,
    min_temp,
    COALESCE(avg_power, 0) AS avg_power,
    COALESCE(avg_efficiency, 0) AS avg_efficiency,
    data_points
FROM device_metrics_daily
WHERE bucket >= $1
  AND bucket <= $2
ORDER BY bucket ASC;

-- =====================================================
-- Status aggregate queries (temperature histogram + uptime)
-- =====================================================

-- name: GetDeviceStatusHourlyAggregates :many
-- Returns hourly status aggregates for specific devices within a time range.
SELECT
    bucket,
    device_identifier,
    temp_below_0,
    temp_0_10,
    temp_10_20,
    temp_20_30,
    temp_30_40,
    temp_40_50,
    temp_50_60,
    temp_60_70,
    temp_70_80,
    temp_80_90,
    temp_90_100,
    temp_100_plus,
    hashing_count,
    not_hashing_count,
    data_points
FROM device_status_hourly
WHERE device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND bucket >= $1
  AND bucket <= $2
ORDER BY bucket ASC;

-- name: GetAllDeviceStatusHourlyAggregates :many
-- Returns hourly status aggregates for ALL devices within a time range.
SELECT
    bucket,
    device_identifier,
    temp_below_0,
    temp_0_10,
    temp_10_20,
    temp_20_30,
    temp_30_40,
    temp_40_50,
    temp_50_60,
    temp_60_70,
    temp_70_80,
    temp_80_90,
    temp_90_100,
    temp_100_plus,
    hashing_count,
    not_hashing_count,
    data_points
FROM device_status_hourly
WHERE bucket >= $1
  AND bucket <= $2
ORDER BY bucket ASC;

-- name: GetDeviceStatusDailyAggregates :many
-- Returns daily status aggregates for specific devices within a time range.
SELECT
    bucket,
    device_identifier,
    temp_below_0,
    temp_0_10,
    temp_10_20,
    temp_20_30,
    temp_30_40,
    temp_40_50,
    temp_50_60,
    temp_60_70,
    temp_70_80,
    temp_80_90,
    temp_90_100,
    temp_100_plus,
    hashing_count,
    not_hashing_count,
    data_points
FROM device_status_daily
WHERE device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND bucket >= $1
  AND bucket <= $2
ORDER BY bucket ASC;

-- name: GetAllDeviceStatusDailyAggregates :many
-- Returns daily status aggregates for ALL devices within a time range.
SELECT
    bucket,
    device_identifier,
    temp_below_0,
    temp_0_10,
    temp_10_20,
    temp_20_30,
    temp_30_40,
    temp_40_50,
    temp_50_60,
    temp_60_70,
    temp_70_80,
    temp_80_90,
    temp_90_100,
    temp_100_plus,
    hashing_count,
    not_hashing_count,
    data_points
FROM device_status_daily
WHERE bucket >= $1
  AND bucket <= $2
ORDER BY bucket ASC;
