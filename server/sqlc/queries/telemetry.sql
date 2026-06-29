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

-- name: GetRawCombinedMetricBuckets :many
-- Returns dashboard-ready metric rollups from raw telemetry, grouped by
-- requested chart bucket. The inner grouping preserves existing raw semantics:
-- cumulative metrics use per-device aggregates before summing into fleet totals,
-- while point-in-time metrics aggregate across all reported samples.
WITH filtered AS (
    SELECT
        time_bucket(sqlc.arg('bucket_interval')::text::interval, time)::timestamptz AS bucket,
        time,
        device_identifier,
        hash_rate_hs,
        temp_c,
        fan_rpm,
        power_w,
        efficiency_jh
    FROM device_metrics
    WHERE time >= sqlc.arg('start_time')
      AND time <= sqlc.arg('end_time')
      AND (
           sqlc.narg('device_identifiers_filter')::text IS NULL
        OR device_identifier = ANY(sqlc.arg('device_identifier_values')::text[])
      )
),
per_device_bucket AS (
    SELECT
        bucket,
        device_identifier,
        AVG(hash_rate_hs) AS avg_hash_rate,
        MIN(hash_rate_hs) AS min_hash_rate,
        MAX(hash_rate_hs) AS max_hash_rate,
        (array_agg(hash_rate_hs ORDER BY time DESC) FILTER (WHERE hash_rate_hs IS NOT NULL))[1] AS latest_hash_rate,
        AVG(temp_c) AS avg_temp,
        MIN(temp_c) AS min_temp,
        MAX(temp_c) AS max_temp,
        COUNT(temp_c)::bigint AS temp_points,
        AVG(fan_rpm) AS avg_fan_rpm,
        MIN(fan_rpm) AS min_fan_rpm,
        MAX(fan_rpm) AS max_fan_rpm,
        COUNT(fan_rpm)::bigint AS fan_rpm_points,
        AVG(power_w) AS avg_power,
        MIN(power_w) AS min_power,
        MAX(power_w) AS max_power,
        (array_agg(power_w ORDER BY time DESC) FILTER (WHERE power_w IS NOT NULL))[1] AS latest_power,
        AVG(efficiency_jh) AS avg_efficiency,
        MIN(efficiency_jh) AS min_efficiency,
        MAX(efficiency_jh) AS max_efficiency,
        COUNT(efficiency_jh)::bigint AS efficiency_points
    FROM filtered
    GROUP BY bucket, device_identifier
)
SELECT
    bucket,
    COALESCE(SUM(avg_hash_rate), 0)::double precision AS avg_hash_rate,
    COALESCE(SUM(min_hash_rate), 0)::double precision AS min_hash_rate,
    COALESCE(SUM(max_hash_rate), 0)::double precision AS max_hash_rate,
    (COUNT(avg_hash_rate) > 0)::boolean AS hash_rate_min_max_available,
    COALESCE(SUM(latest_hash_rate), 0)::double precision AS sum_hash_rate,
    COUNT(avg_hash_rate)::bigint AS hash_rate_count,
    COUNT(avg_hash_rate)::bigint AS hash_rate_device_count,
    COALESCE((
        CASE
            WHEN SUM(temp_points) > 0
            THEN (SUM(avg_temp * temp_points)::double precision / SUM(temp_points)::double precision)
        END
    ), 0)::double precision AS avg_temp,
    COALESCE(MIN(min_temp), 0)::double precision AS min_temp,
    COALESCE(MAX(max_temp), 0)::double precision AS max_temp,
    (COUNT(avg_temp) > 0)::boolean AS temp_min_max_available,
    COALESCE(SUM(avg_temp * temp_points), 0)::double precision AS sum_temp,
    COALESCE(SUM(temp_points), 0)::bigint AS temp_count,
    COUNT(avg_temp)::bigint AS temp_device_count,
    COALESCE((
        CASE
            WHEN SUM(fan_rpm_points) > 0
            THEN (SUM(avg_fan_rpm * fan_rpm_points)::double precision / SUM(fan_rpm_points)::double precision)
        END
    ), 0)::double precision AS avg_fan_rpm,
    COALESCE(MIN(min_fan_rpm), 0)::double precision AS min_fan_rpm,
    COALESCE(MAX(max_fan_rpm), 0)::double precision AS max_fan_rpm,
    (COUNT(avg_fan_rpm) > 0)::boolean AS fan_rpm_min_max_available,
    COALESCE(SUM(avg_fan_rpm * fan_rpm_points), 0)::double precision AS sum_fan_rpm,
    COALESCE(SUM(fan_rpm_points), 0)::bigint AS fan_rpm_count,
    COUNT(avg_fan_rpm)::bigint AS fan_rpm_device_count,
    COALESCE(SUM(avg_power), 0)::double precision AS avg_power,
    COALESCE(SUM(min_power), 0)::double precision AS min_power,
    COALESCE(SUM(max_power), 0)::double precision AS max_power,
    (COUNT(avg_power) > 0)::boolean AS power_min_max_available,
    COALESCE(SUM(latest_power), 0)::double precision AS sum_power,
    COUNT(avg_power)::bigint AS power_count,
    COUNT(avg_power)::bigint AS power_device_count,
    COALESCE((
        CASE
            WHEN SUM(efficiency_points) > 0
            THEN (SUM(avg_efficiency * efficiency_points)::double precision / SUM(efficiency_points)::double precision)
        END
    ), 0)::double precision AS avg_efficiency,
    COALESCE(MIN(min_efficiency), 0)::double precision AS min_efficiency,
    COALESCE(MAX(max_efficiency), 0)::double precision AS max_efficiency,
    (COUNT(avg_efficiency) > 0)::boolean AS efficiency_min_max_available,
    COALESCE(SUM(avg_efficiency * efficiency_points), 0)::double precision AS sum_efficiency,
    COALESCE(SUM(efficiency_points), 0)::bigint AS efficiency_count,
    COUNT(avg_efficiency)::bigint AS efficiency_device_count
FROM per_device_bucket
GROUP BY bucket
ORDER BY bucket ASC;

-- name: GetRawTemperatureStatusBuckets :many
-- Counts each device once per chart bucket using its latest sample with a
-- populated temperature. Buckets with telemetry but no temperature samples are
-- returned with zero counts so chart buckets stay aligned with metrics.
WITH filtered AS (
    SELECT
        time_bucket(sqlc.arg('bucket_interval')::text::interval, time)::timestamptz AS bucket,
        time,
        device_identifier,
        temp_c
    FROM device_metrics
    WHERE time >= sqlc.arg('start_time')
      AND time <= sqlc.arg('end_time')
      AND (
           sqlc.narg('device_identifiers_filter')::text IS NULL
        OR device_identifier = ANY(sqlc.arg('device_identifier_values')::text[])
      )
),
bucket_times AS (
    SELECT DISTINCT bucket
    FROM filtered
),
latest_temp AS (
    SELECT DISTINCT ON (bucket, device_identifier)
        bucket,
        device_identifier,
        temp_c
    FROM filtered
    WHERE temp_c IS NOT NULL
    ORDER BY bucket, device_identifier, time DESC
),
temp_counts AS (
    SELECT
        bucket,
        SUM(CASE WHEN temp_c < 0 THEN 1 ELSE 0 END)::int AS cold_count,
        SUM(CASE WHEN temp_c >= 0 AND temp_c < 70 THEN 1 ELSE 0 END)::int AS ok_count,
        SUM(CASE WHEN temp_c >= 70 AND temp_c < 90 THEN 1 ELSE 0 END)::int AS hot_count,
        SUM(CASE WHEN temp_c >= 90 THEN 1 ELSE 0 END)::int AS critical_count
    FROM latest_temp
    GROUP BY bucket
)
SELECT
    bucket_times.bucket,
    COALESCE(temp_counts.cold_count, 0)::int AS cold_count,
    COALESCE(temp_counts.ok_count, 0)::int AS ok_count,
    COALESCE(temp_counts.hot_count, 0)::int AS hot_count,
    COALESCE(temp_counts.critical_count, 0)::int AS critical_count
FROM bucket_times
LEFT JOIN temp_counts ON temp_counts.bucket = bucket_times.bucket
ORDER BY bucket_times.bucket ASC;

-- name: GetHourlyCombinedMetricBuckets :many
-- Returns dashboard-ready metric rollups from per-device hourly aggregates.
-- This preserves the existing aggregate-table semantics while avoiding a large
-- device-by-bucket result set in Go.
WITH filtered AS (
    SELECT
        bucket,
        device_identifier,
        avg_hash_rate,
        min_hash_rate,
        max_hash_rate,
        avg_temp,
        min_temp,
        max_temp,
        avg_fan_rpm,
        avg_power,
        avg_efficiency,
        data_points
    FROM device_metrics_hourly
    WHERE bucket >= sqlc.arg('start_time')
      AND bucket <= sqlc.arg('end_time')
      AND (
           sqlc.narg('device_identifiers_filter')::text IS NULL
        OR device_identifier = ANY(sqlc.arg('device_identifier_values')::text[])
      )
)
SELECT
    filtered.bucket::timestamptz AS bucket,
    COALESCE(SUM(avg_hash_rate), 0)::double precision AS avg_hash_rate,
    COALESCE((CASE
        WHEN COUNT(avg_hash_rate) > 0
         AND COUNT(avg_hash_rate) = COUNT(min_hash_rate)
         AND COUNT(avg_hash_rate) = COUNT(max_hash_rate)
        THEN SUM(min_hash_rate)::double precision
    END), 0)::double precision AS min_hash_rate,
    COALESCE((CASE
        WHEN COUNT(avg_hash_rate) > 0
         AND COUNT(avg_hash_rate) = COUNT(min_hash_rate)
         AND COUNT(avg_hash_rate) = COUNT(max_hash_rate)
        THEN SUM(max_hash_rate)::double precision
    END), 0)::double precision AS max_hash_rate,
    (
        COUNT(avg_hash_rate) > 0
        AND COUNT(avg_hash_rate) = COUNT(min_hash_rate)
        AND COUNT(avg_hash_rate) = COUNT(max_hash_rate)
    )::boolean AS hash_rate_min_max_available,
    COALESCE(SUM(avg_hash_rate), 0)::double precision AS sum_hash_rate,
    COUNT(avg_hash_rate)::bigint AS hash_rate_count,
    COUNT(avg_hash_rate)::bigint AS hash_rate_device_count,
    COALESCE((CASE
        WHEN SUM(data_points) FILTER (WHERE avg_temp IS NOT NULL) > 0
        THEN (
            (SUM(avg_temp * data_points) FILTER (WHERE avg_temp IS NOT NULL))::double precision
            / (SUM(data_points) FILTER (WHERE avg_temp IS NOT NULL))::double precision
        )
    END), 0)::double precision AS avg_temp,
    COALESCE((CASE
        WHEN COUNT(avg_temp) > 0
         AND COUNT(avg_temp) = COUNT(min_temp)
         AND COUNT(avg_temp) = COUNT(max_temp)
        THEN MIN(min_temp)::double precision
    END), 0)::double precision AS min_temp,
    COALESCE((CASE
        WHEN COUNT(avg_temp) > 0
         AND COUNT(avg_temp) = COUNT(min_temp)
         AND COUNT(avg_temp) = COUNT(max_temp)
        THEN MAX(max_temp)::double precision
    END), 0)::double precision AS max_temp,
    (
        COUNT(avg_temp) > 0
        AND COUNT(avg_temp) = COUNT(min_temp)
        AND COUNT(avg_temp) = COUNT(max_temp)
    )::boolean AS temp_min_max_available,
    COALESCE(SUM(avg_temp), 0)::double precision AS sum_temp,
    COUNT(avg_temp)::bigint AS temp_count,
    COUNT(avg_temp)::bigint AS temp_device_count,
    COALESCE((CASE
        WHEN SUM(data_points) FILTER (WHERE avg_fan_rpm > 0) > 0
        THEN (
            (SUM(avg_fan_rpm * data_points) FILTER (WHERE avg_fan_rpm > 0))::double precision
            / (SUM(data_points) FILTER (WHERE avg_fan_rpm > 0))::double precision
        )
    END), 0)::double precision AS avg_fan_rpm,
    0::double precision AS min_fan_rpm,
    0::double precision AS max_fan_rpm,
    false::boolean AS fan_rpm_min_max_available,
    COALESCE(SUM(avg_fan_rpm) FILTER (WHERE avg_fan_rpm > 0), 0)::double precision AS sum_fan_rpm,
    COUNT(avg_fan_rpm) FILTER (WHERE avg_fan_rpm > 0)::bigint AS fan_rpm_count,
    COUNT(avg_fan_rpm) FILTER (WHERE avg_fan_rpm > 0)::bigint AS fan_rpm_device_count,
    COALESCE(SUM(avg_power) FILTER (WHERE avg_power > 0), 0)::double precision AS avg_power,
    0::double precision AS min_power,
    0::double precision AS max_power,
    false::boolean AS power_min_max_available,
    COALESCE(SUM(avg_power) FILTER (WHERE avg_power > 0), 0)::double precision AS sum_power,
    COUNT(avg_power) FILTER (WHERE avg_power > 0)::bigint AS power_count,
    COUNT(avg_power) FILTER (WHERE avg_power > 0)::bigint AS power_device_count,
    COALESCE((CASE
        WHEN SUM(data_points) FILTER (WHERE avg_efficiency > 0) > 0
        THEN (
            (SUM(avg_efficiency * data_points) FILTER (WHERE avg_efficiency > 0))::double precision
            / (SUM(data_points) FILTER (WHERE avg_efficiency > 0))::double precision
        )
    END), 0)::double precision AS avg_efficiency,
    0::double precision AS min_efficiency,
    0::double precision AS max_efficiency,
    false::boolean AS efficiency_min_max_available,
    COALESCE(SUM(avg_efficiency) FILTER (WHERE avg_efficiency > 0), 0)::double precision AS sum_efficiency,
    COUNT(avg_efficiency) FILTER (WHERE avg_efficiency > 0)::bigint AS efficiency_count,
    COUNT(avg_efficiency) FILTER (WHERE avg_efficiency > 0)::bigint AS efficiency_device_count
FROM filtered
GROUP BY filtered.bucket
ORDER BY filtered.bucket ASC;

-- name: GetDailyCombinedMetricBuckets :many
-- Returns dashboard-ready metric rollups from per-device daily aggregates.
WITH filtered AS (
    SELECT
        bucket,
        device_identifier,
        avg_hash_rate,
        min_hash_rate,
        max_hash_rate,
        avg_temp,
        min_temp,
        max_temp,
        avg_power,
        avg_efficiency,
        data_points
    FROM device_metrics_daily
    WHERE bucket >= sqlc.arg('start_time')
      AND bucket <= sqlc.arg('end_time')
      AND (
           sqlc.narg('device_identifiers_filter')::text IS NULL
        OR device_identifier = ANY(sqlc.arg('device_identifier_values')::text[])
      )
)
SELECT
    filtered.bucket::timestamptz AS bucket,
    COALESCE(SUM(avg_hash_rate), 0)::double precision AS avg_hash_rate,
    COALESCE((CASE
        WHEN COUNT(avg_hash_rate) > 0
         AND COUNT(avg_hash_rate) = COUNT(min_hash_rate)
         AND COUNT(avg_hash_rate) = COUNT(max_hash_rate)
        THEN SUM(min_hash_rate)::double precision
    END), 0)::double precision AS min_hash_rate,
    COALESCE((CASE
        WHEN COUNT(avg_hash_rate) > 0
         AND COUNT(avg_hash_rate) = COUNT(min_hash_rate)
         AND COUNT(avg_hash_rate) = COUNT(max_hash_rate)
        THEN SUM(max_hash_rate)::double precision
    END), 0)::double precision AS max_hash_rate,
    (
        COUNT(avg_hash_rate) > 0
        AND COUNT(avg_hash_rate) = COUNT(min_hash_rate)
        AND COUNT(avg_hash_rate) = COUNT(max_hash_rate)
    )::boolean AS hash_rate_min_max_available,
    COALESCE(SUM(avg_hash_rate), 0)::double precision AS sum_hash_rate,
    COUNT(avg_hash_rate)::bigint AS hash_rate_count,
    COUNT(avg_hash_rate)::bigint AS hash_rate_device_count,
    COALESCE((CASE
        WHEN SUM(data_points) FILTER (WHERE avg_temp IS NOT NULL) > 0
        THEN (
            (SUM(avg_temp * data_points) FILTER (WHERE avg_temp IS NOT NULL))::double precision
            / (SUM(data_points) FILTER (WHERE avg_temp IS NOT NULL))::double precision
        )
    END), 0)::double precision AS avg_temp,
    COALESCE((CASE
        WHEN COUNT(avg_temp) > 0
         AND COUNT(avg_temp) = COUNT(min_temp)
         AND COUNT(avg_temp) = COUNT(max_temp)
        THEN MIN(min_temp)::double precision
    END), 0)::double precision AS min_temp,
    COALESCE((CASE
        WHEN COUNT(avg_temp) > 0
         AND COUNT(avg_temp) = COUNT(min_temp)
         AND COUNT(avg_temp) = COUNT(max_temp)
        THEN MAX(max_temp)::double precision
    END), 0)::double precision AS max_temp,
    (
        COUNT(avg_temp) > 0
        AND COUNT(avg_temp) = COUNT(min_temp)
        AND COUNT(avg_temp) = COUNT(max_temp)
    )::boolean AS temp_min_max_available,
    COALESCE(SUM(avg_temp), 0)::double precision AS sum_temp,
    COUNT(avg_temp)::bigint AS temp_count,
    COUNT(avg_temp)::bigint AS temp_device_count,
    0::double precision AS avg_fan_rpm,
    0::double precision AS min_fan_rpm,
    0::double precision AS max_fan_rpm,
    false::boolean AS fan_rpm_min_max_available,
    0::double precision AS sum_fan_rpm,
    0::bigint AS fan_rpm_count,
    0::bigint AS fan_rpm_device_count,
    COALESCE(SUM(avg_power) FILTER (WHERE avg_power > 0), 0)::double precision AS avg_power,
    0::double precision AS min_power,
    0::double precision AS max_power,
    false::boolean AS power_min_max_available,
    COALESCE(SUM(avg_power) FILTER (WHERE avg_power > 0), 0)::double precision AS sum_power,
    COUNT(avg_power) FILTER (WHERE avg_power > 0)::bigint AS power_count,
    COUNT(avg_power) FILTER (WHERE avg_power > 0)::bigint AS power_device_count,
    COALESCE((CASE
        WHEN SUM(data_points) FILTER (WHERE avg_efficiency > 0) > 0
        THEN (
            (SUM(avg_efficiency * data_points) FILTER (WHERE avg_efficiency > 0))::double precision
            / (SUM(data_points) FILTER (WHERE avg_efficiency > 0))::double precision
        )
    END), 0)::double precision AS avg_efficiency,
    0::double precision AS min_efficiency,
    0::double precision AS max_efficiency,
    false::boolean AS efficiency_min_max_available,
    COALESCE(SUM(avg_efficiency) FILTER (WHERE avg_efficiency > 0), 0)::double precision AS sum_efficiency,
    COUNT(avg_efficiency) FILTER (WHERE avg_efficiency > 0)::bigint AS efficiency_count,
    COUNT(avg_efficiency) FILTER (WHERE avg_efficiency > 0)::bigint AS efficiency_device_count
FROM filtered
GROUP BY filtered.bucket
ORDER BY filtered.bucket ASC;

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
