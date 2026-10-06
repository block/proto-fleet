-- name: GetContainerModuleScope :one
SELECT d.org_id, d.site_id FROM container_module m
JOIN container_tank t ON t.id = m.tank_id JOIN device d ON d.id = t.device_id
WHERE m.id = sqlc.arg('module_id') AND d.org_id = sqlc.arg('org_id') AND d.deleted_at IS NULL;

-- name: ListContainerModuleHistory :many
SELECT mm.time, mm.collected_at, mm.hashrate_hs, mm.power_w, mm.temperature_c
FROM container_module_metrics mm
JOIN container_module m ON m.id = mm.module_id
JOIN container_tank t ON t.id = m.tank_id JOIN device d ON d.id = t.device_id
WHERE mm.module_id = sqlc.arg('module_id') AND d.org_id = sqlc.arg('org_id') AND d.deleted_at IS NULL
  AND NOT EXISTS (
    SELECT 1 FROM jsonb_to_recordset(sqlc.arg('authority')::jsonb) AS a(org_wide boolean, sites bigint[])
    WHERE NOT (CASE WHEN a.org_wide THEN d.site_id IS NULL OR NOT (d.site_id = ANY(a.sites))
                   ELSE COALESCE(d.site_id = ANY(a.sites), FALSE) END)
  )
  AND mm.time >= sqlc.arg('start_time')::timestamptz AND mm.time < sqlc.arg('end_time')::timestamptz
  AND (NOT sqlc.arg('has_cursor')::boolean OR (mm.time, mm.collected_at) > (sqlc.arg('after_time')::timestamptz, sqlc.arg('after_collected_at')::timestamptz))
ORDER BY mm.time, mm.collected_at LIMIT sqlc.arg('page_limit')::int;

-- Filter choices are metadata about readable miners, not organization-wide data.
-- name: ListFleetInventoryOptions :one
SELECT COALESCE(array_agg(DISTINCT dd.model ORDER BY dd.model) FILTER (WHERE dd.model <> ''), '{}'::text[])::text[] AS models,
       COALESCE(array_agg(DISTINCT dd.firmware_version ORDER BY dd.firmware_version) FILTER (WHERE dd.firmware_version <> ''), '{}'::text[])::text[] AS firmware_versions
FROM fleet_mining_asset m
JOIN device d ON d.id=m.id AND d.org_id=m.org_id
JOIN discovered_device dd ON dd.id=d.discovered_device_id
WHERE m.kind=5 AND m.org_id=sqlc.arg('org_id') AND NOT EXISTS (
 SELECT 1 FROM jsonb_to_recordset(sqlc.arg('authority')::jsonb) AS a(org_wide boolean, sites bigint[])
 WHERE NOT (CASE WHEN a.org_wide THEN m.site_id IS NULL OR NOT (m.site_id=ANY(a.sites))
                ELSE COALESCE(m.site_id=ANY(a.sites),FALSE) END)
);

-- name: GetContainerTankScope :one
SELECT d.site_id FROM container_tank t
JOIN device d ON d.id = t.device_id
JOIN discovered_device dd ON dd.id = d.discovered_device_id AND dd.org_id = d.org_id
WHERE t.id = sqlc.arg('tank_id') AND t.present
  AND d.device_identifier = sqlc.arg('container_identifier') AND d.org_id = sqlc.arg('org_id')
  AND d.deleted_at IS NULL AND dd.deleted_at IS NULL AND dd.device_class = 2;

-- Aggregate in the database, never transfer raw module history to the browser.
-- Scope is rechecked in this statement to cover reassignment after the scope read.
-- name: GetContainerTankHistory :many
WITH members AS MATERIALIZED (
  SELECT m.id FROM container_module m
  JOIN container_tank t ON t.id = m.tank_id
  JOIN device d ON d.id = t.device_id
  JOIN discovered_device dd ON dd.id = d.discovered_device_id AND dd.org_id = d.org_id
  WHERE t.id = sqlc.arg('tank_id') AND t.present AND m.retired_at IS NULL
    AND d.device_identifier = sqlc.arg('container_identifier') AND d.org_id = sqlc.arg('org_id')
    AND d.deleted_at IS NULL AND dd.deleted_at IS NULL AND dd.device_class = 2
    AND NOT EXISTS (
      SELECT 1 FROM jsonb_to_recordset(sqlc.arg('authority')::jsonb) AS a(org_wide boolean, sites bigint[])
      WHERE NOT (CASE WHEN a.org_wide THEN d.site_id IS NULL OR NOT (d.site_id = ANY(a.sites))
                     ELSE COALESCE(d.site_id = ANY(a.sites), FALSE) END)
    )
), latest_samples AS (
  SELECT DISTINCT ON (mm.module_id, mm.time) mm.*
  FROM container_module_metrics mm JOIN members m ON m.id = mm.module_id
  WHERE mm.time >= sqlc.arg('start_time')::timestamptz AND mm.time < sqlc.arg('end_time')::timestamptz
  ORDER BY mm.module_id, mm.time, mm.collected_at DESC
), samples AS (
  SELECT module_id, floor(extract(epoch FROM time) / sqlc.arg('granularity_seconds')::int)::bigint
      * sqlc.arg('granularity_seconds')::int AS bucket,
    CASE WHEN hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8 THEN hashrate_hs END AS hashrate,
    CASE WHEN power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8 THEN power_w END AS power,
    CASE WHEN temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8 THEN temperature_c END AS temperature
  FROM latest_samples
), module_buckets AS (
  -- Each module contributes once per bucket, irrespective of source timestamp
  -- skew or sample frequency. Missing values never become zero observations.
  SELECT bucket, module_id,
    avg(hashrate) AS hash_avg, min(hashrate) AS hash_min, max(hashrate) AS hash_max,
    avg(power) AS power_avg, min(power) AS power_min, max(power) AS power_max,
    avg(temperature) AS temp_avg, min(temperature) AS temp_min, max(temperature) AS temp_max,
    avg(hashrate) FILTER (WHERE power IS NOT NULL) AS paired_hash_avg,
    min(hashrate) FILTER (WHERE power IS NOT NULL) AS paired_hash_min,
    max(hashrate) FILTER (WHERE power IS NOT NULL) AS paired_hash_max,
    avg(power) FILTER (WHERE hashrate IS NOT NULL) AS paired_power_avg,
    min(power) FILTER (WHERE hashrate IS NOT NULL) AS paired_power_min,
    max(power) FILTER (WHERE hashrate IS NOT NULL) AS paired_power_max
  FROM samples GROUP BY bucket, module_id
), totals AS (
  SELECT bucket,
    sum(hash_avg) AS hash_avg, sum(hash_min) AS hash_min, sum(hash_max) AS hash_max,
    sum(power_avg) AS power_avg, sum(power_min) AS power_min, sum(power_max) AS power_max,
    avg(temp_avg) AS temp_avg, min(temp_min) AS temp_min, max(temp_max) AS temp_max,
    sum(paired_hash_avg) AS paired_hash_avg, sum(paired_hash_min) AS paired_hash_min, sum(paired_hash_max) AS paired_hash_max,
    sum(paired_power_avg) AS paired_power_avg, sum(paired_power_min) AS paired_power_min, sum(paired_power_max) AS paired_power_max,
    count(hash_avg)::int AS hash_count, count(power_avg)::int AS power_count,
    count(temp_avg)::int AS temp_count, count(paired_hash_avg)::int AS paired_count
  FROM module_buckets GROUP BY bucket
), readings AS (
  SELECT bucket, 2::int AS measurement_type, hash_count AS module_count,
    hash_avg / 1e12 AS average, hash_min / 1e12 AS minimum, hash_max / 1e12 AS maximum FROM totals
  UNION ALL
  SELECT bucket, 3, power_count, power_avg / 1000, power_min / 1000, power_max / 1000 FROM totals
  UNION ALL
  SELECT bucket, 4, paired_count,
    paired_power_avg / (CASE WHEN paired_hash_avg > 0 THEN paired_hash_avg END) * 1e12,
    paired_power_min / (CASE WHEN paired_hash_max > 0 THEN paired_hash_max END) * 1e12,
    paired_power_max / (CASE WHEN paired_hash_min > 0 THEN paired_hash_min END) * 1e12 FROM totals
  UNION ALL
  SELECT bucket, 1, temp_count, temp_avg, temp_min, temp_max FROM totals
)
SELECT bucket::bigint AS bucket, measurement_type::int AS measurement_type,
  (CASE WHEN average IS NOT NULL THEN module_count ELSE 0 END)::int AS module_count,
  (average IS NOT NULL)::boolean AS has_values,
  (minimum IS NOT NULL)::boolean AS has_minimum,
  (maximum IS NOT NULL)::boolean AS has_maximum,
  COALESCE(average, 0)::float8 AS average,
  COALESCE(minimum, 0)::float8 AS minimum,
  COALESCE(maximum, 0)::float8 AS maximum
FROM readings ORDER BY bucket, measurement_type;
