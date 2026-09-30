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
