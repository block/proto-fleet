-- name: GetContainerModuleScope :one
SELECT d.org_id, d.site_id FROM container_module m
JOIN container_tank t ON t.id = m.tank_id JOIN container_scope d ON d.id = t.device_id
WHERE m.id = sqlc.arg('module_id') AND d.org_id = sqlc.arg('org_id') AND d.deleted_at IS NULL;

-- Resolve once per page; services bind the result to the cursor and pass that
-- same value into the data query so crossing a cutoff mid-request cannot switch tiers.
-- name: GetHistoryResolution :one
SELECT container_history_resolution(sqlc.arg('start_time')::timestamptz,sqlc.arg('end_time')::timestamptz)::int AS resolution_seconds;

-- Raw reads retain the original nullable fields; older/longer windows return
-- source-bucket averages, with authorization before combining topology versions.
-- name: ListContainerModuleHistory :many
WITH resolution AS (
 SELECT sqlc.arg('resolution_seconds')::int AS seconds
), readable AS (
 SELECT mm.* FROM container_equipment_history mm
 JOIN container_module m ON m.id=mm.module_id
 JOIN container_tank t ON t.id=m.tank_id JOIN container_scope d ON d.id=t.device_id
 CROSS JOIN resolution
 WHERE mm.equipment_kind=6 AND mm.resolution_seconds=resolution.seconds AND m.id=sqlc.arg('module_id')
 AND d.org_id=sqlc.arg('org_id') AND d.deleted_at IS NULL
 AND mm.bucket>=CASE WHEN resolution.seconds=0 THEN sqlc.arg('start_time')::timestamptz ELSE to_timestamp(floor(extract(epoch FROM sqlc.arg('start_time')::timestamptz)/resolution.seconds)*resolution.seconds) END AND mm.bucket<sqlc.arg('end_time')::timestamptz
 AND NOT EXISTS (
 SELECT 1 FROM jsonb_to_recordset(sqlc.arg('authority')::jsonb) AS a(org_wide boolean,sites bigint[])
 WHERE NOT (CASE WHEN a.org_wide THEN mm.site_id IS NULL OR NOT(mm.site_id=ANY(a.sites)) ELSE COALESCE(mm.site_id=ANY(a.sites),FALSE) END)
 )
), metrics AS (
 SELECT bucket,collected_at,measurement_type,sum(total)/NULLIF(sum(samples),0) AS value,
 min(reporting_count) AS reporting,max(expected_count) AS expected,bool_and(coverage_complete) AS complete
 FROM readable GROUP BY bucket,collected_at,measurement_type
), summaries AS (
 SELECT bucket AS time,collected_at,
 max(value) FILTER(WHERE measurement_type=2)::float8 AS hashrate_hs,
 max(value) FILTER(WHERE measurement_type=3)::float8 AS power_w,
 max(value) FILTER(WHERE measurement_type=1)::float8 AS temperature_c,
 max(value) FILTER(WHERE measurement_type=4)::float8 AS efficiency_jh,
 jsonb_object_agg(CASE measurement_type WHEN 1 THEN 'temperature' WHEN 2 THEN 'hashrate' WHEN 3 THEN 'power' END,
 jsonb_build_object('reporting',reporting,'expected',expected,'topology_complete',complete)) FILTER(WHERE measurement_type<>4) AS coverage
 FROM metrics GROUP BY bucket,collected_at
)
SELECT mm.time,mm.collected_at,mm.hashrate_hs,mm.power_w,mm.temperature_c,mm.efficiency_jh,mm.coverage
FROM container_module_metrics mm JOIN readable r ON r.resolution_seconds=0 AND r.measurement_type=2
 AND r.module_id=mm.module_id AND r.bucket=mm.time AND r.collected_at=mm.collected_at
-- Bound the raw lookup directly so TimescaleDB can prune unrelated chunks
-- instead of revisiting old history for each readable sample.
WHERE mm.module_id=sqlc.arg('module_id')
 AND mm.time>=sqlc.arg('start_time')::timestamptz AND mm.time<sqlc.arg('end_time')::timestamptz
 AND (NOT sqlc.arg('has_cursor')::boolean OR (mm.time,mm.collected_at)>(sqlc.arg('after_time')::timestamptz,sqlc.arg('after_collected_at')::timestamptz))
UNION ALL
SELECT time,collected_at,hashrate_hs,power_w,temperature_c,efficiency_jh,coverage FROM summaries
CROSS JOIN resolution WHERE resolution.seconds>0 AND (NOT sqlc.arg('has_cursor')::boolean OR (time,collected_at)>(sqlc.arg('after_time')::timestamptz,sqlc.arg('after_collected_at')::timestamptz))
ORDER BY time,collected_at LIMIT sqlc.arg('page_limit')::int;

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
JOIN container_scope d ON d.id = t.device_id
JOIN discovered_device dd ON dd.id = d.discovered_device_id AND dd.org_id = d.org_id
WHERE t.id = sqlc.arg('tank_id') AND t.present
  AND d.device_identifier = sqlc.arg('container_identifier') AND d.org_id = sqlc.arg('org_id')
  AND d.deleted_at IS NULL AND dd.deleted_at IS NULL AND dd.device_class = 2;

-- Bucket immutable poll-time totals, never current module membership.
-- Historical access uses the site recorded with the sample.
-- name: GetContainerTankHistory :many
WITH resolution AS (
 SELECT container_history_resolution(sqlc.arg('start_time')::timestamptz,sqlc.arg('end_time')::timestamptz) AS seconds
), settings AS (
 SELECT seconds,GREATEST(1,seconds)*ceil(sqlc.arg('granularity_seconds')::int::numeric/GREATEST(1,seconds))::bigint AS width FROM resolution
), readings AS (
 SELECT floor(extract(epoch FROM mm.bucket)/settings.width)::bigint*settings.width AS bucket,
 mm.measurement_type,mm.samples,mm.total,mm.minimum,mm.maximum,mm.reporting_count,mm.expected_count,mm.coverage_complete,
 CASE mm.measurement_type WHEN 2 THEN 1e-12 WHEN 3 THEN 0.001 WHEN 4 THEN 1e12 ELSE 1 END::float8 AS scale
 FROM container_equipment_history mm JOIN container_tank t ON t.id=mm.tank_id
 JOIN container_scope d ON d.id=t.device_id CROSS JOIN settings
 WHERE mm.equipment_kind=4 AND mm.resolution_seconds=settings.seconds AND t.id=sqlc.arg('tank_id') AND d.device_identifier=sqlc.arg('container_identifier') AND d.org_id=sqlc.arg('org_id') AND d.deleted_at IS NULL
 AND mm.bucket>=CASE WHEN settings.seconds=0 THEN sqlc.arg('start_time')::timestamptz ELSE to_timestamp(floor(extract(epoch FROM sqlc.arg('start_time')::timestamptz)/settings.seconds)*settings.seconds) END
 AND mm.bucket<sqlc.arg('end_time')::timestamptz
 AND NOT EXISTS (
 SELECT 1 FROM jsonb_to_recordset(sqlc.arg('authority')::jsonb) AS a(org_wide boolean,sites bigint[])
 WHERE NOT (CASE WHEN a.org_wide THEN mm.site_id IS NULL OR NOT(mm.site_id=ANY(a.sites)) ELSE COALESCE(mm.site_id=ANY(a.sites),FALSE) END)
 )
)
SELECT bucket::bigint AS bucket,measurement_type::int AS measurement_type,
 sum(samples)::int AS module_count,
 (sum(samples)>0)::boolean AS has_values,
 (count(minimum)>0)::boolean AS has_minimum,
 (count(maximum)>0)::boolean AS has_maximum,
 COALESCE(sum(total)/NULLIF(sum(samples),0)*min(scale),0)::float8 AS average,
 COALESCE(min(minimum)*min(scale),0)::float8 AS minimum,COALESCE(max(maximum)*min(scale),0)::float8 AS maximum,
 min(reporting_count)::int AS reporting_count,max(expected_count)::int AS expected_count,
 bool_and(coverage_complete)::boolean AS coverage_complete
FROM readings GROUP BY bucket,measurement_type ORDER BY bucket,measurement_type;
