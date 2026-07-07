-- Fleet dashboard metric rollups. The 90 second bucket width must match
-- models.FleetMetricRollupBucketDuration and the client dashboard's shortest
-- fleet granularity.

-- name: DeleteFleetMetricRollupsForWindow :exec
DELETE FROM fleet_metric_rollup_90s
WHERE bucket >= sqlc.arg('start_time')::timestamptz
  AND bucket < sqlc.arg('end_time')::timestamptz;

-- name: UpsertFleetMetricRollups :exec
WITH per_device_bucket AS (
    SELECT
        time_bucket(INTERVAL '90 seconds', dm.time)::timestamptz AS bucket,
        dm.device_identifier,
        AVG(hash_rate_hs) AS avg_hash_rate,
        MIN(hash_rate_hs) AS min_hash_rate,
        MAX(hash_rate_hs) AS max_hash_rate,
        last(hash_rate_hs, dm.time) FILTER (WHERE hash_rate_hs IS NOT NULL) AS latest_hash_rate,
        COUNT(hash_rate_hs)::bigint AS hash_rate_points,
        MIN(temp_c) AS min_temp,
        MAX(temp_c) AS max_temp,
        SUM(temp_c) AS sum_temp,
        last(temp_c, dm.time) FILTER (WHERE temp_c IS NOT NULL) AS latest_temp,
        COUNT(temp_c)::bigint AS temp_points,
        MIN(fan_rpm) AS min_fan_rpm,
        MAX(fan_rpm) AS max_fan_rpm,
        SUM(fan_rpm) AS sum_fan_rpm,
        COUNT(fan_rpm)::bigint AS fan_rpm_points,
        AVG(power_w) AS avg_power,
        MIN(power_w) AS min_power,
        MAX(power_w) AS max_power,
        last(power_w, dm.time) FILTER (WHERE power_w IS NOT NULL) AS latest_power,
        COUNT(power_w)::bigint AS power_points,
        MIN(efficiency_jh) AS min_efficiency,
        MAX(efficiency_jh) AS max_efficiency,
        SUM(efficiency_jh) AS sum_efficiency,
        COUNT(efficiency_jh)::bigint AS efficiency_points
    FROM device_metrics dm
    WHERE dm.time >= sqlc.arg('start_time')::timestamptz
      AND dm.time < sqlc.arg('end_time')::timestamptz
    GROUP BY bucket, dm.device_identifier
),
device_org AS (
    SELECT DISTINCT ON (d.device_identifier)
        d.device_identifier,
        d.org_id,
        COALESCE(d.site_id, 0)::bigint AS site_id
    FROM device d
    JOIN (
        SELECT DISTINCT device_identifier
        FROM per_device_bucket
    ) ids ON ids.device_identifier = d.device_identifier
    ORDER BY d.device_identifier, (d.deleted_at IS NULL) DESC, d.updated_at DESC, d.id DESC
),
rollup AS (
    SELECT
        p.bucket,
        o.org_id,
        o.site_id,
        SUM(p.avg_hash_rate)::float8 AS avg_hash_rate,
        SUM(p.min_hash_rate)::float8 AS min_hash_rate,
        SUM(p.max_hash_rate)::float8 AS max_hash_rate,
        SUM(p.latest_hash_rate)::float8 AS latest_hash_rate,
        COUNT(*) FILTER (WHERE p.hash_rate_points > 0)::bigint AS hash_rate_device_count,
        MIN(p.min_temp)::float8 AS min_temp,
        MAX(p.max_temp)::float8 AS max_temp,
        SUM(p.sum_temp)::float8 AS sum_temp,
        SUM(p.temp_points)::bigint AS temp_points,
        COUNT(*) FILTER (WHERE p.temp_points > 0)::bigint AS temp_device_count,
        -- Thresholds must match tempThresholdCold/Hot/Critical in telemetry_store.go.
        COUNT(*) FILTER (WHERE p.latest_temp < 0)::int AS temp_cold_count,
        COUNT(*) FILTER (WHERE p.latest_temp >= 0 AND p.latest_temp < 70)::int AS temp_ok_count,
        COUNT(*) FILTER (WHERE p.latest_temp >= 70 AND p.latest_temp < 90)::int AS temp_hot_count,
        COUNT(*) FILTER (WHERE p.latest_temp >= 90)::int AS temp_critical_count,
        MIN(p.min_fan_rpm)::float8 AS min_fan_rpm,
        MAX(p.max_fan_rpm)::float8 AS max_fan_rpm,
        SUM(p.sum_fan_rpm)::float8 AS sum_fan_rpm,
        SUM(p.fan_rpm_points)::bigint AS fan_rpm_points,
        COUNT(*) FILTER (WHERE p.fan_rpm_points > 0)::bigint AS fan_rpm_device_count,
        SUM(p.avg_power)::float8 AS avg_power,
        SUM(p.min_power)::float8 AS min_power,
        SUM(p.max_power)::float8 AS max_power,
        SUM(p.latest_power)::float8 AS latest_power,
        COUNT(*) FILTER (WHERE p.power_points > 0)::bigint AS power_device_count,
        MIN(p.min_efficiency)::float8 AS min_efficiency,
        MAX(p.max_efficiency)::float8 AS max_efficiency,
        SUM(p.sum_efficiency)::float8 AS sum_efficiency,
        SUM(p.efficiency_points)::bigint AS efficiency_points,
        COUNT(*) FILTER (WHERE p.efficiency_points > 0)::bigint AS efficiency_device_count
    FROM per_device_bucket p
    JOIN device_org o ON o.device_identifier = p.device_identifier
    GROUP BY p.bucket, o.org_id, o.site_id
)
INSERT INTO fleet_metric_rollup_90s (
    bucket, org_id, site_id,
    avg_hash_rate, min_hash_rate, max_hash_rate, latest_hash_rate, hash_rate_device_count,
    min_temp, max_temp, sum_temp, temp_points, temp_device_count,
    temp_cold_count, temp_ok_count, temp_hot_count, temp_critical_count,
    min_fan_rpm, max_fan_rpm, sum_fan_rpm, fan_rpm_points, fan_rpm_device_count,
    avg_power, min_power, max_power, latest_power, power_device_count,
    min_efficiency, max_efficiency, sum_efficiency, efficiency_points, efficiency_device_count
)
SELECT
    bucket, org_id, site_id,
    avg_hash_rate, min_hash_rate, max_hash_rate, latest_hash_rate, hash_rate_device_count,
    min_temp, max_temp, sum_temp, temp_points, temp_device_count,
    temp_cold_count, temp_ok_count, temp_hot_count, temp_critical_count,
    min_fan_rpm, max_fan_rpm, sum_fan_rpm, fan_rpm_points, fan_rpm_device_count,
    avg_power, min_power, max_power, latest_power, power_device_count,
    min_efficiency, max_efficiency, sum_efficiency, efficiency_points, efficiency_device_count
FROM rollup
ON CONFLICT (org_id, site_id, bucket) DO UPDATE SET
    avg_hash_rate = EXCLUDED.avg_hash_rate,
    min_hash_rate = EXCLUDED.min_hash_rate,
    max_hash_rate = EXCLUDED.max_hash_rate,
    latest_hash_rate = EXCLUDED.latest_hash_rate,
    hash_rate_device_count = EXCLUDED.hash_rate_device_count,
    min_temp = EXCLUDED.min_temp,
    max_temp = EXCLUDED.max_temp,
    sum_temp = EXCLUDED.sum_temp,
    temp_points = EXCLUDED.temp_points,
    temp_device_count = EXCLUDED.temp_device_count,
    temp_cold_count = EXCLUDED.temp_cold_count,
    temp_ok_count = EXCLUDED.temp_ok_count,
    temp_hot_count = EXCLUDED.temp_hot_count,
    temp_critical_count = EXCLUDED.temp_critical_count,
    min_fan_rpm = EXCLUDED.min_fan_rpm,
    max_fan_rpm = EXCLUDED.max_fan_rpm,
    sum_fan_rpm = EXCLUDED.sum_fan_rpm,
    fan_rpm_points = EXCLUDED.fan_rpm_points,
    fan_rpm_device_count = EXCLUDED.fan_rpm_device_count,
    avg_power = EXCLUDED.avg_power,
    min_power = EXCLUDED.min_power,
    max_power = EXCLUDED.max_power,
    latest_power = EXCLUDED.latest_power,
    power_device_count = EXCLUDED.power_device_count,
    min_efficiency = EXCLUDED.min_efficiency,
    max_efficiency = EXCLUDED.max_efficiency,
    sum_efficiency = EXCLUDED.sum_efficiency,
    efficiency_points = EXCLUDED.efficiency_points,
    efficiency_device_count = EXCLUDED.efficiency_device_count;

-- name: GetOrgFleetMetricRollups :many
SELECT
    bucket,
    COALESCE(SUM(avg_hash_rate), 0)::float8 AS avg_hash_rate,
    COALESCE(SUM(min_hash_rate), 0)::float8 AS min_hash_rate,
    COALESCE(SUM(max_hash_rate), 0)::float8 AS max_hash_rate,
    COALESCE(SUM(latest_hash_rate), 0)::float8 AS latest_hash_rate,
    SUM(hash_rate_device_count)::bigint AS hash_rate_device_count,
    CASE WHEN SUM(temp_points) > 0 THEN (SUM(sum_temp) / SUM(temp_points)) ELSE 0 END::float8 AS avg_temp,
    COALESCE(MIN(min_temp), 0)::float8 AS min_temp,
    COALESCE(MAX(max_temp), 0)::float8 AS max_temp,
    COALESCE(SUM(sum_temp), 0)::float8 AS sum_temp,
    SUM(temp_points)::bigint AS temp_points,
    SUM(temp_device_count)::bigint AS temp_device_count,
    SUM(temp_cold_count)::int AS temp_cold_count,
    SUM(temp_ok_count)::int AS temp_ok_count,
    SUM(temp_hot_count)::int AS temp_hot_count,
    SUM(temp_critical_count)::int AS temp_critical_count,
    CASE WHEN SUM(fan_rpm_points) > 0 THEN (SUM(sum_fan_rpm) / SUM(fan_rpm_points)) ELSE 0 END::float8 AS avg_fan_rpm,
    COALESCE(MIN(min_fan_rpm), 0)::float8 AS min_fan_rpm,
    COALESCE(MAX(max_fan_rpm), 0)::float8 AS max_fan_rpm,
    COALESCE(SUM(sum_fan_rpm), 0)::float8 AS sum_fan_rpm,
    SUM(fan_rpm_points)::bigint AS fan_rpm_points,
    SUM(fan_rpm_device_count)::bigint AS fan_rpm_device_count,
    COALESCE(SUM(avg_power), 0)::float8 AS avg_power,
    COALESCE(SUM(min_power), 0)::float8 AS min_power,
    COALESCE(SUM(max_power), 0)::float8 AS max_power,
    COALESCE(SUM(latest_power), 0)::float8 AS latest_power,
    SUM(power_device_count)::bigint AS power_device_count,
    CASE WHEN SUM(efficiency_points) > 0 THEN (SUM(sum_efficiency) / SUM(efficiency_points)) ELSE 0 END::float8 AS avg_efficiency,
    COALESCE(MIN(min_efficiency), 0)::float8 AS min_efficiency,
    COALESCE(MAX(max_efficiency), 0)::float8 AS max_efficiency,
    COALESCE(SUM(sum_efficiency), 0)::float8 AS sum_efficiency,
    SUM(efficiency_points)::bigint AS efficiency_points,
    SUM(efficiency_device_count)::bigint AS efficiency_device_count
FROM fleet_metric_rollup_90s
WHERE org_id = sqlc.arg('org_id')
  AND bucket >= sqlc.arg('start_time')::timestamptz
  AND bucket < sqlc.arg('end_time')::timestamptz
GROUP BY bucket
ORDER BY bucket ASC;

-- name: GetLatestFleetMetricRollupBucket :one
SELECT COALESCE(
    (
        SELECT latest_bucket
        FROM fleet_metric_rollup_progress
        WHERE id = TRUE
    ),
    'epoch'::timestamptz
)::timestamptz AS bucket;

-- name: GetFleetMetricRollupCoverage :one
SELECT
    COALESCE(
        (
            SELECT earliest_bucket
            FROM fleet_metric_rollup_progress
            WHERE id = TRUE
        ),
        'epoch'::timestamptz
    )::timestamptz AS earliest_bucket,
    COALESCE(
        (
            SELECT latest_bucket
            FROM fleet_metric_rollup_progress
            WHERE id = TRUE
        ),
        'epoch'::timestamptz
    )::timestamptz AS latest_bucket;

-- name: AdvanceFleetMetricRollupProgress :exec
INSERT INTO fleet_metric_rollup_progress (id, earliest_bucket, latest_bucket, updated_at)
VALUES (
    TRUE,
    sqlc.arg('earliest_bucket')::timestamptz,
    sqlc.arg('latest_bucket')::timestamptz,
    NOW()
)
ON CONFLICT (id) DO UPDATE SET
    earliest_bucket = CASE
        WHEN EXCLUDED.earliest_bucket <= fleet_metric_rollup_progress.latest_bucket + INTERVAL '90 seconds'
         AND fleet_metric_rollup_progress.earliest_bucket <= EXCLUDED.latest_bucket + INTERVAL '90 seconds'
            THEN LEAST(fleet_metric_rollup_progress.earliest_bucket, EXCLUDED.earliest_bucket)
        ELSE EXCLUDED.earliest_bucket
    END,
    latest_bucket = CASE
        WHEN EXCLUDED.earliest_bucket <= fleet_metric_rollup_progress.latest_bucket + INTERVAL '90 seconds'
         AND fleet_metric_rollup_progress.earliest_bucket <= EXCLUDED.latest_bucket + INTERVAL '90 seconds'
            THEN GREATEST(fleet_metric_rollup_progress.latest_bucket, EXCLUDED.latest_bucket)
        ELSE EXCLUDED.latest_bucket
    END,
    updated_at = NOW();
