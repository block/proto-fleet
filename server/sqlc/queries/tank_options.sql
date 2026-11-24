-- Tank labels only: deliberately avoid module, health, and telemetry joins.
-- name: ListFleetTankOptions :many
WITH readable AS (
 SELECT t.id,
   (COALESCE(NULLIF(d.custom_name, ''), 'Container ' || COALESCE(NULLIF(d.serial_number, ''), d.device_identifier)) || ' T' || t.position)::text AS name,
   COALESCE(s.name, '')::text AS site_name
 FROM container_tank t
 JOIN device d ON d.id = t.device_id
 JOIN discovered_device dd ON dd.id = d.discovered_device_id AND dd.org_id = d.org_id
 LEFT JOIN site s ON s.id = d.site_id AND s.org_id = d.org_id AND s.deleted_at IS NULL
 WHERE t.present AND d.org_id = sqlc.arg('org_id') AND d.deleted_at IS NULL AND dd.deleted_at IS NULL
   AND NOT EXISTS (
     SELECT 1 FROM jsonb_to_recordset(sqlc.arg('authority')::jsonb) AS a(org_wide boolean, sites bigint[])
     WHERE NOT (CASE WHEN a.org_wide THEN d.site_id IS NULL OR NOT (d.site_id = ANY(a.sites))
                     ELSE COALESCE(d.site_id = ANY(a.sites), FALSE) END)
   )
)
SELECT id, name, site_name FROM readable
WHERE NOT sqlc.arg('has_cursor')::boolean OR (name, id) > (sqlc.arg('after_name')::text, sqlc.arg('after_id')::bigint)
ORDER BY name, id
LIMIT sqlc.arg('page_limit')::int;
