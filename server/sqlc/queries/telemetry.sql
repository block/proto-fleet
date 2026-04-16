-- Telemetry queries for device_metrics table and continuous aggregates
-- Note: All device identification uses device_identifier (TEXT), not device_id (BIGINT)

-- name: InsertDeviceMetrics :exec
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
    health
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
    $11, $12, $13, $14, $15, $16, $17, $18, $19, $20,
    $21, $22, $23
) ON CONFLICT (time, device_identifier) DO NOTHING;

-- name: GetLatestDeviceMetrics :many
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
    health
FROM device_metrics
WHERE device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND time >= $1
ORDER BY device_identifier, time DESC;

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
    health
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
    health
FROM device_metrics
WHERE device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND time >= $1
  AND time <= $2
ORDER BY time ASC
LIMIT sqlc.arg('max_rows')::int;

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
    health
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
