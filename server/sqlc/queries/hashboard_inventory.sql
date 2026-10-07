-- A bounded batch returns only readable current modules. Child slots never
-- enter fleet_mining_asset or affect its counts, sorting, selection or exports.
-- name: GetModuleHashboards :one
WITH modules AS (
 SELECT m.id, d.site_id FROM container_module m JOIN container_tank t ON t.id=m.tank_id
 JOIN device d ON d.id=t.device_id JOIN discovered_device dd ON dd.id=d.discovered_device_id
 WHERE m.id=ANY(sqlc.arg('module_ids')::bigint[]) AND d.org_id=sqlc.arg('org_id')
 AND m.retired_at IS NULL AND t.present AND d.deleted_at IS NULL AND dd.deleted_at IS NULL
 AND NOT EXISTS (
  SELECT 1 FROM jsonb_to_recordset(sqlc.arg('authority')::jsonb) AS a(org_wide boolean, sites bigint[])
  WHERE NOT (CASE WHEN a.org_wide THEN d.site_id IS NULL OR NOT (d.site_id=ANY(a.sites)) ELSE COALESCE(d.site_id=ANY(a.sites),FALSE) END)
 )
), slots AS (
 SELECT m.id AS module_id, p.position,
 CASE WHEN s.observed_at > now()-INTERVAL '2 minutes' THEN COALESCE(s.occupancy,'unknown') ELSE 'unknown' END AS occupancy,
 s.observed_at, CASE WHEN s.observed_at > now()-INTERVAL '2 minutes' THEN COALESCE(s.unavailable_reason,'') ELSE '' END AS unavailable_reason,
 CASE WHEN i.id IS NOT NULL AND s.occupancy='occupied' AND s.observed_at > now()-INTERVAL '2 minutes' THEN
 jsonb_build_object('hashboard_id',i.hashboard_id,'installation_id',i.id,'module_id',m.id,'position',p.position,
 'mac_address', COALESCE(i.metadata->>'macAddress',''), 'serial', COALESCE(i.metadata->>'serial',''),
 'metadata',i.metadata,
 'state',CASE WHEN s.state_observed_at > now()-INTERVAL '2 minutes' AND s.state_observed_at <= now()+INTERVAL '5 seconds'
 AND (s.state_observed_at >= i.installed_at OR NOT EXISTS (SELECT 1 FROM hashboard_installation prior WHERE prior.hashboard_id=i.hashboard_id AND (prior.installed_at,prior.id)<(i.installed_at,i.id))) THEN s.state ELSE 'unknown' END,
 'state_observed_at',s.state_observed_at,'mining_state',s.mining_state,'isolated',s.isolated,
 'observed_at',mm.time,'collected_at',mm.collected_at,'hashrate_hs',mm.hashrate_hs,'temperature_c',mm.temperature_c,
 'hashrail_ok',mm.hashrail_ok,'refclks_ok',mm.refclks_ok,'health',CASE WHEN mm.state_observed_at >= i.installed_at OR NOT EXISTS (SELECT 1 FROM hashboard_installation prior WHERE prior.hashboard_id=i.hashboard_id AND (prior.installed_at,prior.id)<(i.installed_at,i.id)) THEN mm.health ELSE NULL END) END AS board
 FROM modules m CROSS JOIN generate_series(1,3) p(position)
 LEFT JOIN hashboard_slot s ON s.module_id=m.id AND s.position=p.position
 LEFT JOIN hashboard_installation i ON i.module_id=m.id AND i.position=p.position AND i.removed_at IS NULL AND i.site_id IS NOT DISTINCT FROM m.site_id
 LEFT JOIN hashboard_latest_metrics mm ON mm.hashboard_id=i.hashboard_id AND mm.installation_id=i.id
 AND mm.time=s.health_observed_at AND mm.time > now()-INTERVAL '2 minutes' AND mm.time <= now()+INTERVAL '5 seconds'
), result AS (
 SELECT module_id, jsonb_agg(jsonb_build_object('position',position,'occupancy',occupancy,'observed_at',observed_at,'unavailable_reason',unavailable_reason,'board',board) ORDER BY position) AS slots
 FROM slots GROUP BY module_id
)
SELECT COALESCE(jsonb_agg(to_jsonb(result) ORDER BY module_id),'[]'::jsonb)::jsonb AS modules FROM result;

-- name: ListHashboardHistory :many
SELECT mm.hashboard_id, mm.installation_id, mm.time, mm.collected_at,
 (CASE WHEN evidence.current_state THEN mm.state ELSE 'unknown' END)::text AS state,
 mm.state_observed_at, COALESCE(evidence.current_state,FALSE)::boolean AS state_in_installation,
 mm.mining_state, mm.isolated, mm.hashrate_hs, mm.temperature_c, mm.hashrail_ok, mm.refclks_ok, mm.health,
 i.module_id, i.position
FROM hashboard_metrics mm JOIN hashboard h ON h.id=mm.hashboard_id
JOIN hashboard_installation i ON i.id=mm.installation_id
CROSS JOIN LATERAL (SELECT mm.state_observed_at >= i.installed_at OR NOT EXISTS (SELECT 1 FROM hashboard_installation prior WHERE prior.hashboard_id=i.hashboard_id AND (prior.installed_at,prior.id)<(i.installed_at,i.id)) AS current_state) evidence
WHERE h.id=sqlc.arg('hashboard_id') AND h.org_id=sqlc.arg('org_id')
 AND NOT EXISTS (
  SELECT 1 FROM jsonb_to_recordset(sqlc.arg('authority')::jsonb) AS a(org_wide boolean, sites bigint[])
  WHERE NOT (CASE WHEN a.org_wide THEN i.site_id IS NULL OR NOT (i.site_id=ANY(a.sites)) ELSE COALESCE(i.site_id=ANY(a.sites),FALSE) END)
 )
 AND mm.time >= sqlc.arg('start_time')::timestamptz AND mm.time < sqlc.arg('end_time')::timestamptz
 AND (NOT sqlc.arg('has_cursor')::boolean OR (mm.time,mm.collected_at) > (sqlc.arg('after_time')::timestamptz,sqlc.arg('after_collected_at')::timestamptz))
ORDER BY mm.time, mm.collected_at LIMIT sqlc.arg('page_limit')::int;

-- Access is based on any readable installation, including closed placements.
-- It is independent of the requested sample range and current hardware location.
-- name: CanReadHashboardHistory :one
SELECT EXISTS (
 SELECT 1 FROM hashboard h JOIN hashboard_installation i ON i.hashboard_id=h.id
 WHERE h.id=sqlc.arg('hashboard_id') AND h.org_id=sqlc.arg('org_id')
 AND NOT EXISTS (
  SELECT 1 FROM jsonb_to_recordset(sqlc.arg('authority')::jsonb) AS a(org_wide boolean, sites bigint[])
  WHERE NOT (CASE WHEN a.org_wide THEN i.site_id IS NULL OR NOT (i.site_id=ANY(a.sites)) ELSE COALESCE(i.site_id=ANY(a.sites),FALSE) END)
 )
)::boolean AS allowed;
