-- name: CreateSite :one
-- Org-scoped insert. The unique partial index on (org_id, name) where
-- deleted_at IS NULL surfaces name collisions as a unique-violation; the
-- service layer maps that to AlreadyExists.
INSERT INTO site (
    org_id,
    name,
    description,
    location_city,
    location_state,
    timezone,
    power_capacity_mw,
    network_config
) VALUES (
    sqlc.arg('org_id'),
    sqlc.arg('name'),
    sqlc.narg('description'),
    sqlc.narg('location_city'),
    sqlc.narg('location_state'),
    sqlc.narg('timezone'),
    sqlc.narg('power_capacity_mw'),
    sqlc.narg('network_config')
)
RETURNING *;

-- name: GetSite :one
SELECT *
FROM site
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL;

-- name: ListSites :many
-- Returns each site with attachment counts so the delete-confirm dialog
-- can show "N miners, M buildings" without an extra round trip.
SELECT
    s.*,
    COALESCE(d.device_count, 0)::bigint AS device_count,
    COALESCE(b.building_count, 0)::bigint AS building_count
FROM site s
LEFT JOIN (
    SELECT device.site_id, COUNT(*) AS device_count
    FROM device
    WHERE device.org_id = sqlc.arg('org_id')
      AND device.deleted_at IS NULL
      AND device.site_id IS NOT NULL
    GROUP BY device.site_id
) d ON d.site_id = s.id
LEFT JOIN (
    SELECT building.site_id, COUNT(*) AS building_count
    FROM building
    WHERE building.org_id = sqlc.arg('org_id')
      AND building.deleted_at IS NULL
      AND building.site_id IS NOT NULL
    GROUP BY building.site_id
) b ON b.site_id = s.id
WHERE s.org_id = sqlc.arg('org_id')
  AND s.deleted_at IS NULL
ORDER BY s.name;

-- name: UpdateSite :exec
UPDATE site
SET name              = sqlc.arg('name'),
    description       = sqlc.narg('description'),
    location_city     = sqlc.narg('location_city'),
    location_state    = sqlc.narg('location_state'),
    timezone          = sqlc.narg('timezone'),
    power_capacity_mw = sqlc.narg('power_capacity_mw'),
    network_config    = sqlc.narg('network_config'),
    updated_at        = CURRENT_TIMESTAMP
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL;

-- name: SoftDeleteSite :execrows
-- Caller is expected to also unassign attached devices and buildings in
-- the same transaction (cascade-unassign — see plan J3).
UPDATE site
SET deleted_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL;

-- name: UnassignDevicesFromSite :execrows
-- Sets device.site_id = NULL for every live device pointing at the given
-- site within the org. Used by site delete (cascade-unassign) and by the
-- "Unassigned" reassign target.
UPDATE device
SET site_id = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE org_id = sqlc.arg('org_id')
  AND site_id = sqlc.arg('site_id')
  AND deleted_at IS NULL;

-- name: UnassignBuildingsFromSite :execrows
-- Sets building.site_id = NULL for every live building pointing at the
-- given site within the org.
UPDATE building
SET site_id = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE org_id = sqlc.arg('org_id')
  AND site_id = sqlc.arg('site_id')
  AND deleted_at IS NULL;

-- name: SiteBelongsToOrg :one
SELECT EXISTS(
    SELECT 1 FROM site
    WHERE id = sqlc.arg('id')
      AND org_id = sqlc.arg('org_id')
      AND deleted_at IS NULL
) AS belongs;
