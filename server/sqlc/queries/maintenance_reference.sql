-- name: ResolveMaintenanceMinerContext :one
SELECT
    d.device_identifier AS miner_identifier,
    COALESCE(d.site_id, dsr.site_id) AS site_id,
    COALESCE(s.name, '') AS site_name,
    COALESCE(d.building_id, dsr.building_id) AS building_id,
    COALESCE(b.name, '') AS building_name,
    dsr.zone,
    rack.id AS rack_id,
    rack.label AS rack_label,
    COALESCE(grp.label, '') AS group_label
FROM device d
LEFT JOIN device_set_membership rack_member
  ON rack_member.device_id = d.id AND rack_member.org_id = d.org_id AND rack_member.device_set_type = 'rack'
LEFT JOIN device_set rack
  ON rack.id = rack_member.device_set_id AND rack.org_id = d.org_id AND rack.deleted_at IS NULL
LEFT JOIN device_set_rack dsr
  ON dsr.device_set_id = rack.id AND dsr.org_id = d.org_id
LEFT JOIN site s
  ON s.id = COALESCE(d.site_id, dsr.site_id) AND s.org_id = d.org_id AND s.deleted_at IS NULL
LEFT JOIN building b
  ON b.id = COALESCE(d.building_id, dsr.building_id) AND b.org_id = d.org_id AND b.deleted_at IS NULL
LEFT JOIN LATERAL (
    SELECT sets.label
    FROM device_set_membership member
    JOIN device_set sets ON sets.id = member.device_set_id AND sets.org_id = member.org_id
    WHERE member.device_id = d.id AND member.org_id = d.org_id
      AND member.device_set_type = 'group' AND sets.deleted_at IS NULL
    ORDER BY sets.label, sets.id
    LIMIT 1
) grp ON TRUE
WHERE d.org_id = sqlc.arg('org_id') AND d.device_identifier = sqlc.arg('miner_identifier')
  AND d.deleted_at IS NULL;

-- name: ResolveMaintenanceLocationContext :one
SELECT
    s.id AS site_id,
    COALESCE(s.name, '') AS site_name,
    b.id AS building_id,
    COALESCE(b.name, '') AS building_name
FROM (SELECT 1) singleton
LEFT JOIN building b
  ON b.id = sqlc.narg('building_id') AND b.org_id = sqlc.arg('org_id') AND b.deleted_at IS NULL
LEFT JOIN site s
  ON s.id = COALESCE(sqlc.narg('site_id'), b.site_id)
 AND s.org_id = sqlc.arg('org_id') AND s.deleted_at IS NULL
WHERE (sqlc.narg('site_id')::bigint IS NULL OR s.id IS NOT NULL)
  AND (sqlc.narg('building_id')::bigint IS NULL OR b.id IS NOT NULL)
  AND (sqlc.narg('site_id')::bigint IS NULL OR sqlc.narg('building_id')::bigint IS NULL OR b.site_id = s.id);

-- name: ResolveMaintenanceAssignee :one
SELECT u.id AS user_id, u.username, COALESCE(r.name, '') AS role_name
FROM user_organization uo
JOIN "user" u ON u.id = uo.user_id AND u.deleted_at IS NULL
LEFT JOIN role r ON r.id = uo.role_id AND r.deleted_at IS NULL
WHERE uo.organization_id = sqlc.arg('org_id') AND uo.user_id = sqlc.arg('user_id')
  AND uo.deleted_at IS NULL;

-- name: ListMaintenanceAssignees :many
SELECT u.id AS user_id, u.username, COALESCE(r.name, '') AS role_name
FROM user_organization uo
JOIN "user" u ON u.id = uo.user_id AND u.deleted_at IS NULL
LEFT JOIN role r ON r.id = uo.role_id AND r.deleted_at IS NULL
WHERE uo.organization_id = sqlc.arg('org_id') AND uo.deleted_at IS NULL
ORDER BY LOWER(u.username), u.id;
