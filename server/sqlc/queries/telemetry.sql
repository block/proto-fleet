-- Telemetry queries for device_metrics table and continuous aggregates
-- Note: All device identification uses device_identifier (TEXT), not device_id (BIGINT)

-- name: GetDeviceOutcomeAverages :many
-- Returns one row per currently active, organization-scoped device. Baseline
-- and comparison averages are calculated in one scan so both sides use the
-- same raw telemetry source and exact device population.
WITH scoped_devices AS (
    SELECT DISTINCT device_identifier
    FROM device
    WHERE org_id = sqlc.arg('org_id')
      AND deleted_at IS NULL
      AND device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
)
SELECT
    dm.device_identifier,
    COALESCE(AVG(dm.hash_rate_hs) FILTER (
        WHERE dm.time >= sqlc.arg('baseline_start') AND dm.time < sqlc.arg('baseline_end')
    ), 0)::float8 AS baseline_hashrate,
    COUNT(dm.hash_rate_hs) FILTER (
        WHERE dm.time >= sqlc.arg('baseline_start') AND dm.time < sqlc.arg('baseline_end')
    )::bigint AS baseline_hashrate_points,
    COALESCE(AVG(dm.hash_rate_hs) FILTER (
        WHERE dm.time >= sqlc.arg('comparison_start') AND dm.time < sqlc.arg('comparison_end')
    ), 0)::float8 AS comparison_hashrate,
    COUNT(dm.hash_rate_hs) FILTER (
        WHERE dm.time >= sqlc.arg('comparison_start') AND dm.time < sqlc.arg('comparison_end')
    )::bigint AS comparison_hashrate_points,
    COALESCE(AVG(dm.efficiency_jh) FILTER (
        WHERE dm.time >= sqlc.arg('baseline_start') AND dm.time < sqlc.arg('baseline_end')
    ), 0)::float8 AS baseline_efficiency,
    COUNT(dm.efficiency_jh) FILTER (
        WHERE dm.time >= sqlc.arg('baseline_start') AND dm.time < sqlc.arg('baseline_end')
    )::bigint AS baseline_efficiency_points,
    COALESCE(AVG(dm.efficiency_jh) FILTER (
        WHERE dm.time >= sqlc.arg('comparison_start') AND dm.time < sqlc.arg('comparison_end')
    ), 0)::float8 AS comparison_efficiency,
    COUNT(dm.efficiency_jh) FILTER (
        WHERE dm.time >= sqlc.arg('comparison_start') AND dm.time < sqlc.arg('comparison_end')
    )::bigint AS comparison_efficiency_points,
    COALESCE(AVG(dm.power_w) FILTER (
        WHERE dm.time >= sqlc.arg('baseline_start') AND dm.time < sqlc.arg('baseline_end')
    ), 0)::float8 AS baseline_power,
    COUNT(dm.power_w) FILTER (
        WHERE dm.time >= sqlc.arg('baseline_start') AND dm.time < sqlc.arg('baseline_end')
    )::bigint AS baseline_power_points,
    COALESCE(AVG(dm.power_w) FILTER (
        WHERE dm.time >= sqlc.arg('comparison_start') AND dm.time < sqlc.arg('comparison_end')
    ), 0)::float8 AS comparison_power,
    COUNT(dm.power_w) FILTER (
        WHERE dm.time >= sqlc.arg('comparison_start') AND dm.time < sqlc.arg('comparison_end')
    )::bigint AS comparison_power_points
FROM device_metrics dm
JOIN scoped_devices sd USING (device_identifier)
WHERE dm.time >= sqlc.arg('baseline_start')
  AND dm.time < sqlc.arg('comparison_end')
GROUP BY dm.device_identifier
ORDER BY dm.device_identifier;

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

-- name: GetDeviceMetricsRawBucketAggregates :many
WITH per_device_bucket AS (
    SELECT
        time_bucket(make_interval(secs => sqlc.arg('bucket_seconds')::double precision), dm.time)::timestamptz AS bucket,
        dm.device_identifier,
        AVG(hash_rate_hs) AS avg_hash_rate,
        MIN(hash_rate_hs) AS min_hash_rate,
        MAX(hash_rate_hs) AS max_hash_rate,
        last(hash_rate_hs, dm.time) FILTER (WHERE hash_rate_hs IS NOT NULL) AS latest_hash_rate,
        COUNT(hash_rate_hs)::bigint AS hash_rate_points,
        AVG(temp_c) AS avg_temp,
        MIN(temp_c) AS min_temp,
        MAX(temp_c) AS max_temp,
        SUM(temp_c) AS sum_temp,
        last(temp_c, dm.time) FILTER (WHERE temp_c IS NOT NULL) AS latest_temp,
        COUNT(temp_c)::bigint AS temp_points,
        AVG(fan_rpm) AS avg_fan_rpm,
        MIN(fan_rpm) AS min_fan_rpm,
        MAX(fan_rpm) AS max_fan_rpm,
        SUM(fan_rpm) AS sum_fan_rpm,
        COUNT(fan_rpm)::bigint AS fan_rpm_points,
        AVG(power_w) AS avg_power,
        MIN(power_w) AS min_power,
        MAX(power_w) AS max_power,
        last(power_w, dm.time) FILTER (WHERE power_w IS NOT NULL) AS latest_power,
        COUNT(power_w)::bigint AS power_points,
        AVG(efficiency_jh) AS avg_efficiency,
        MIN(efficiency_jh) AS min_efficiency,
        MAX(efficiency_jh) AS max_efficiency,
        SUM(efficiency_jh) AS sum_efficiency,
        COUNT(efficiency_jh)::bigint AS efficiency_points
    FROM device_metrics dm
    WHERE dm.device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
      AND dm.time >= sqlc.arg('start_time')
      AND dm.time <= sqlc.arg('end_time')
    GROUP BY bucket, dm.device_identifier
)
SELECT
    bucket,
    COALESCE(SUM(avg_hash_rate), 0)::float8 AS avg_hash_rate,
    COALESCE(SUM(min_hash_rate), 0)::float8 AS min_hash_rate,
    COALESCE(SUM(max_hash_rate), 0)::float8 AS max_hash_rate,
    COALESCE(SUM(latest_hash_rate), 0)::float8 AS latest_hash_rate,
    COUNT(*) FILTER (WHERE hash_rate_points > 0)::bigint AS hash_rate_device_count,
    CASE WHEN SUM(temp_points) > 0 THEN (SUM(sum_temp) / SUM(temp_points)) ELSE 0 END::float8 AS avg_temp,
    COALESCE(MIN(min_temp), 0)::float8 AS min_temp,
    COALESCE(MAX(max_temp), 0)::float8 AS max_temp,
    COALESCE(SUM(sum_temp), 0)::float8 AS sum_temp,
    SUM(temp_points)::bigint AS temp_points,
    COUNT(*) FILTER (WHERE temp_points > 0)::bigint AS temp_device_count,
    COUNT(*) FILTER (WHERE latest_temp < 0)::int AS temp_cold_count,
    COUNT(*) FILTER (WHERE latest_temp >= 0 AND latest_temp < 70)::int AS temp_ok_count,
    COUNT(*) FILTER (WHERE latest_temp >= 70 AND latest_temp < 90)::int AS temp_hot_count,
    COUNT(*) FILTER (WHERE latest_temp >= 90)::int AS temp_critical_count,
    CASE WHEN SUM(fan_rpm_points) > 0 THEN (SUM(sum_fan_rpm) / SUM(fan_rpm_points)) ELSE 0 END::float8 AS avg_fan_rpm,
    COALESCE(MIN(min_fan_rpm), 0)::float8 AS min_fan_rpm,
    COALESCE(MAX(max_fan_rpm), 0)::float8 AS max_fan_rpm,
    COALESCE(SUM(sum_fan_rpm), 0)::float8 AS sum_fan_rpm,
    SUM(fan_rpm_points)::bigint AS fan_rpm_points,
    COUNT(*) FILTER (WHERE fan_rpm_points > 0)::bigint AS fan_rpm_device_count,
    COALESCE(SUM(avg_power), 0)::float8 AS avg_power,
    COALESCE(SUM(min_power), 0)::float8 AS min_power,
    COALESCE(SUM(max_power), 0)::float8 AS max_power,
    COALESCE(SUM(latest_power), 0)::float8 AS latest_power,
    COUNT(*) FILTER (WHERE power_points > 0)::bigint AS power_device_count,
    CASE WHEN SUM(efficiency_points) > 0 THEN (SUM(sum_efficiency) / SUM(efficiency_points)) ELSE 0 END::float8 AS avg_efficiency,
    COALESCE(MIN(min_efficiency), 0)::float8 AS min_efficiency,
    COALESCE(MAX(max_efficiency), 0)::float8 AS max_efficiency,
    COALESCE(SUM(sum_efficiency), 0)::float8 AS sum_efficiency,
    SUM(efficiency_points)::bigint AS efficiency_points,
    COUNT(*) FILTER (WHERE efficiency_points > 0)::bigint AS efficiency_device_count
FROM per_device_bucket
GROUP BY bucket
ORDER BY bucket ASC;

-- name: GetAllDeviceMetricsRawBucketAggregates :many
WITH per_device_bucket AS (
    SELECT
        time_bucket(make_interval(secs => sqlc.arg('bucket_seconds')::double precision), dm.time)::timestamptz AS bucket,
        dm.device_identifier,
        AVG(hash_rate_hs) AS avg_hash_rate,
        MIN(hash_rate_hs) AS min_hash_rate,
        MAX(hash_rate_hs) AS max_hash_rate,
        last(hash_rate_hs, dm.time) FILTER (WHERE hash_rate_hs IS NOT NULL) AS latest_hash_rate,
        COUNT(hash_rate_hs)::bigint AS hash_rate_points,
        AVG(temp_c) AS avg_temp,
        MIN(temp_c) AS min_temp,
        MAX(temp_c) AS max_temp,
        SUM(temp_c) AS sum_temp,
        last(temp_c, dm.time) FILTER (WHERE temp_c IS NOT NULL) AS latest_temp,
        COUNT(temp_c)::bigint AS temp_points,
        AVG(fan_rpm) AS avg_fan_rpm,
        MIN(fan_rpm) AS min_fan_rpm,
        MAX(fan_rpm) AS max_fan_rpm,
        SUM(fan_rpm) AS sum_fan_rpm,
        COUNT(fan_rpm)::bigint AS fan_rpm_points,
        AVG(power_w) AS avg_power,
        MIN(power_w) AS min_power,
        MAX(power_w) AS max_power,
        last(power_w, dm.time) FILTER (WHERE power_w IS NOT NULL) AS latest_power,
        COUNT(power_w)::bigint AS power_points,
        AVG(efficiency_jh) AS avg_efficiency,
        MIN(efficiency_jh) AS min_efficiency,
        MAX(efficiency_jh) AS max_efficiency,
        SUM(efficiency_jh) AS sum_efficiency,
        COUNT(efficiency_jh)::bigint AS efficiency_points
    FROM device_metrics dm
    WHERE dm.time >= sqlc.arg('start_time')
      AND dm.time <= sqlc.arg('end_time')
    GROUP BY bucket, dm.device_identifier
)
SELECT
    bucket,
    COALESCE(SUM(avg_hash_rate), 0)::float8 AS avg_hash_rate,
    COALESCE(SUM(min_hash_rate), 0)::float8 AS min_hash_rate,
    COALESCE(SUM(max_hash_rate), 0)::float8 AS max_hash_rate,
    COALESCE(SUM(latest_hash_rate), 0)::float8 AS latest_hash_rate,
    COUNT(*) FILTER (WHERE hash_rate_points > 0)::bigint AS hash_rate_device_count,
    CASE WHEN SUM(temp_points) > 0 THEN (SUM(sum_temp) / SUM(temp_points)) ELSE 0 END::float8 AS avg_temp,
    COALESCE(MIN(min_temp), 0)::float8 AS min_temp,
    COALESCE(MAX(max_temp), 0)::float8 AS max_temp,
    COALESCE(SUM(sum_temp), 0)::float8 AS sum_temp,
    SUM(temp_points)::bigint AS temp_points,
    COUNT(*) FILTER (WHERE temp_points > 0)::bigint AS temp_device_count,
    COUNT(*) FILTER (WHERE latest_temp < 0)::int AS temp_cold_count,
    COUNT(*) FILTER (WHERE latest_temp >= 0 AND latest_temp < 70)::int AS temp_ok_count,
    COUNT(*) FILTER (WHERE latest_temp >= 70 AND latest_temp < 90)::int AS temp_hot_count,
    COUNT(*) FILTER (WHERE latest_temp >= 90)::int AS temp_critical_count,
    CASE WHEN SUM(fan_rpm_points) > 0 THEN (SUM(sum_fan_rpm) / SUM(fan_rpm_points)) ELSE 0 END::float8 AS avg_fan_rpm,
    COALESCE(MIN(min_fan_rpm), 0)::float8 AS min_fan_rpm,
    COALESCE(MAX(max_fan_rpm), 0)::float8 AS max_fan_rpm,
    COALESCE(SUM(sum_fan_rpm), 0)::float8 AS sum_fan_rpm,
    SUM(fan_rpm_points)::bigint AS fan_rpm_points,
    COUNT(*) FILTER (WHERE fan_rpm_points > 0)::bigint AS fan_rpm_device_count,
    COALESCE(SUM(avg_power), 0)::float8 AS avg_power,
    COALESCE(SUM(min_power), 0)::float8 AS min_power,
    COALESCE(SUM(max_power), 0)::float8 AS max_power,
    COALESCE(SUM(latest_power), 0)::float8 AS latest_power,
    COUNT(*) FILTER (WHERE power_points > 0)::bigint AS power_device_count,
    CASE WHEN SUM(efficiency_points) > 0 THEN (SUM(sum_efficiency) / SUM(efficiency_points)) ELSE 0 END::float8 AS avg_efficiency,
    COALESCE(MIN(min_efficiency), 0)::float8 AS min_efficiency,
    COALESCE(MAX(max_efficiency), 0)::float8 AS max_efficiency,
    COALESCE(SUM(sum_efficiency), 0)::float8 AS sum_efficiency,
    SUM(efficiency_points)::bigint AS efficiency_points,
    COUNT(*) FILTER (WHERE efficiency_points > 0)::bigint AS efficiency_device_count
FROM per_device_bucket
GROUP BY bucket
ORDER BY bucket ASC;

-- name: GetOrgDeviceMetricsRawBucketAggregates :many
WITH latest_device AS (
    SELECT DISTINCT ON (d.device_identifier)
        d.device_identifier,
        d.org_id
    FROM device d
    ORDER BY d.device_identifier, (d.deleted_at IS NULL) DESC, d.updated_at DESC, d.id DESC
),
org_device AS (
    SELECT device_identifier
    FROM latest_device
    WHERE org_id = sqlc.arg('org_id')
),
per_device_bucket AS (
    SELECT
        time_bucket(make_interval(secs => sqlc.arg('bucket_seconds')::double precision), dm.time)::timestamptz AS bucket,
        dm.device_identifier,
        AVG(hash_rate_hs) AS avg_hash_rate,
        MIN(hash_rate_hs) AS min_hash_rate,
        MAX(hash_rate_hs) AS max_hash_rate,
        last(hash_rate_hs, dm.time) FILTER (WHERE hash_rate_hs IS NOT NULL) AS latest_hash_rate,
        COUNT(hash_rate_hs)::bigint AS hash_rate_points,
        AVG(temp_c) AS avg_temp,
        MIN(temp_c) AS min_temp,
        MAX(temp_c) AS max_temp,
        SUM(temp_c) AS sum_temp,
        last(temp_c, dm.time) FILTER (WHERE temp_c IS NOT NULL) AS latest_temp,
        COUNT(temp_c)::bigint AS temp_points,
        AVG(fan_rpm) AS avg_fan_rpm,
        MIN(fan_rpm) AS min_fan_rpm,
        MAX(fan_rpm) AS max_fan_rpm,
        SUM(fan_rpm) AS sum_fan_rpm,
        COUNT(fan_rpm)::bigint AS fan_rpm_points,
        AVG(power_w) AS avg_power,
        MIN(power_w) AS min_power,
        MAX(power_w) AS max_power,
        last(power_w, dm.time) FILTER (WHERE power_w IS NOT NULL) AS latest_power,
        COUNT(power_w)::bigint AS power_points,
        AVG(efficiency_jh) AS avg_efficiency,
        MIN(efficiency_jh) AS min_efficiency,
        MAX(efficiency_jh) AS max_efficiency,
        SUM(efficiency_jh) AS sum_efficiency,
        COUNT(efficiency_jh)::bigint AS efficiency_points
    FROM device_metrics dm
    JOIN org_device USING (device_identifier)
    WHERE dm.time >= sqlc.arg('start_time')
      AND dm.time <= sqlc.arg('end_time')
    GROUP BY bucket, dm.device_identifier
)
SELECT
    p.bucket,
    COALESCE(SUM(p.avg_hash_rate), 0)::float8 AS avg_hash_rate,
    COALESCE(SUM(p.min_hash_rate), 0)::float8 AS min_hash_rate,
    COALESCE(SUM(p.max_hash_rate), 0)::float8 AS max_hash_rate,
    COALESCE(SUM(p.latest_hash_rate), 0)::float8 AS latest_hash_rate,
    COUNT(*) FILTER (WHERE p.hash_rate_points > 0)::bigint AS hash_rate_device_count,
    CASE WHEN SUM(p.temp_points) > 0 THEN (SUM(p.sum_temp) / SUM(p.temp_points)) ELSE 0 END::float8 AS avg_temp,
    COALESCE(MIN(p.min_temp), 0)::float8 AS min_temp,
    COALESCE(MAX(p.max_temp), 0)::float8 AS max_temp,
    COALESCE(SUM(p.sum_temp), 0)::float8 AS sum_temp,
    SUM(p.temp_points)::bigint AS temp_points,
    COUNT(*) FILTER (WHERE p.temp_points > 0)::bigint AS temp_device_count,
    COUNT(*) FILTER (WHERE p.latest_temp < 0)::int AS temp_cold_count,
    COUNT(*) FILTER (WHERE p.latest_temp >= 0 AND p.latest_temp < 70)::int AS temp_ok_count,
    COUNT(*) FILTER (WHERE p.latest_temp >= 70 AND p.latest_temp < 90)::int AS temp_hot_count,
    COUNT(*) FILTER (WHERE p.latest_temp >= 90)::int AS temp_critical_count,
    CASE WHEN SUM(p.fan_rpm_points) > 0 THEN (SUM(p.sum_fan_rpm) / SUM(p.fan_rpm_points)) ELSE 0 END::float8 AS avg_fan_rpm,
    COALESCE(MIN(p.min_fan_rpm), 0)::float8 AS min_fan_rpm,
    COALESCE(MAX(p.max_fan_rpm), 0)::float8 AS max_fan_rpm,
    COALESCE(SUM(p.sum_fan_rpm), 0)::float8 AS sum_fan_rpm,
    SUM(p.fan_rpm_points)::bigint AS fan_rpm_points,
    COUNT(*) FILTER (WHERE p.fan_rpm_points > 0)::bigint AS fan_rpm_device_count,
    COALESCE(SUM(p.avg_power), 0)::float8 AS avg_power,
    COALESCE(SUM(p.min_power), 0)::float8 AS min_power,
    COALESCE(SUM(p.max_power), 0)::float8 AS max_power,
    COALESCE(SUM(p.latest_power), 0)::float8 AS latest_power,
    COUNT(*) FILTER (WHERE p.power_points > 0)::bigint AS power_device_count,
    CASE WHEN SUM(p.efficiency_points) > 0 THEN (SUM(p.sum_efficiency) / SUM(p.efficiency_points)) ELSE 0 END::float8 AS avg_efficiency,
    COALESCE(MIN(p.min_efficiency), 0)::float8 AS min_efficiency,
    COALESCE(MAX(p.max_efficiency), 0)::float8 AS max_efficiency,
    COALESCE(SUM(p.sum_efficiency), 0)::float8 AS sum_efficiency,
    SUM(p.efficiency_points)::bigint AS efficiency_points,
    COUNT(*) FILTER (WHERE p.efficiency_points > 0)::bigint AS efficiency_device_count
FROM per_device_bucket p
GROUP BY p.bucket
ORDER BY p.bucket ASC;

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

-- name: GetOrgDeviceMetricsHourlyAggregates :many
-- Returns hourly aggregates for the current devices in an org.
-- COALESCE handles NULL values from AVG() when all source values are NULL
WITH latest_device AS (
    SELECT DISTINCT ON (d.device_identifier)
        d.device_identifier,
        d.org_id
    FROM device d
    ORDER BY d.device_identifier, (d.deleted_at IS NULL) DESC, d.updated_at DESC, d.id DESC
)
SELECT
    dmh.bucket,
    dmh.device_identifier,
    COALESCE(dmh.avg_hash_rate, 0) AS avg_hash_rate,
    dmh.max_hash_rate,
    dmh.min_hash_rate,
    COALESCE(dmh.avg_temp, 0) AS avg_temp,
    dmh.max_temp,
    dmh.min_temp,
    COALESCE(dmh.avg_fan_rpm, 0) AS avg_fan_rpm,
    COALESCE(dmh.avg_power, 0) AS avg_power,
    dmh.total_power,
    COALESCE(dmh.avg_efficiency, 0) AS avg_efficiency,
    dmh.data_points
FROM device_metrics_hourly dmh
JOIN latest_device d USING (device_identifier)
WHERE d.org_id = sqlc.arg('org_id')
  AND dmh.bucket >= sqlc.arg('start_time')
  AND dmh.bucket <= sqlc.arg('end_time')
ORDER BY dmh.bucket ASC;

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

-- name: GetOrgDeviceStatusHourlyAggregates :many
-- Returns hourly status aggregates for the current devices in an org.
WITH latest_device AS (
    SELECT DISTINCT ON (d.device_identifier)
        d.device_identifier,
        d.org_id
    FROM device d
    ORDER BY d.device_identifier, (d.deleted_at IS NULL) DESC, d.updated_at DESC, d.id DESC
)
SELECT
    dsh.bucket,
    dsh.device_identifier,
    dsh.temp_below_0,
    dsh.temp_0_10,
    dsh.temp_10_20,
    dsh.temp_20_30,
    dsh.temp_30_40,
    dsh.temp_40_50,
    dsh.temp_50_60,
    dsh.temp_60_70,
    dsh.temp_70_80,
    dsh.temp_80_90,
    dsh.temp_90_100,
    dsh.temp_100_plus,
    dsh.hashing_count,
    dsh.not_hashing_count,
    dsh.data_points
FROM device_status_hourly dsh
JOIN latest_device d USING (device_identifier)
WHERE d.org_id = sqlc.arg('org_id')
  AND dsh.bucket >= sqlc.arg('start_time')
  AND dsh.bucket <= sqlc.arg('end_time')
ORDER BY dsh.bucket ASC;

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

-- name: DisableSyncCommit :exec
SET LOCAL synchronous_commit = off;
