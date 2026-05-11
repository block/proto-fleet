-- name: CreateBuilding :one
-- `site_id` is nullable. Name is unique per (site_id, name) when site_id
-- is non-null; the partial index surfaces collisions to the service
-- layer. Unassigned buildings (site_id IS NULL) are not name-unique so
-- cascade-unassign on site delete cannot collide.
INSERT INTO building (
    org_id,
    site_id,
    name,
    description,
    power_kw,
    overhead_kw,
    aisles,
    physical_rack_count,
    racks_per_aisle,
    default_rack_rows,
    default_rack_columns,
    default_rack_order_index
) VALUES (
    sqlc.arg('org_id'),
    sqlc.narg('site_id'),
    sqlc.arg('name'),
    sqlc.narg('description'),
    sqlc.narg('power_kw'),
    sqlc.narg('overhead_kw'),
    sqlc.narg('aisles'),
    sqlc.narg('physical_rack_count'),
    sqlc.narg('racks_per_aisle'),
    sqlc.narg('default_rack_rows'),
    sqlc.narg('default_rack_columns'),
    sqlc.arg('default_rack_order_index')
)
RETURNING *;

-- name: GetBuilding :one
SELECT *
FROM building
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL;

-- name: ListBuildingsByOrg :many
-- Lists every live building in the org with its rack count. The site
-- filter is folded into the query via two narg flags rather than two
-- separate queries: pass `site_id` for "buildings under this site",
-- pass `unassigned_only=true` for "site_id IS NULL", or leave both
-- unset for "all buildings in org".
SELECT
    b.*,
    COALESCE(r.rack_count, 0)::bigint AS rack_count
FROM building b
LEFT JOIN (
    SELECT dsr.building_id, COUNT(*) AS rack_count
    FROM device_set_rack dsr
    JOIN device_set ds ON dsr.device_set_id = ds.id
    WHERE ds.deleted_at IS NULL AND dsr.building_id IS NOT NULL
    GROUP BY dsr.building_id
) r ON r.building_id = b.id
WHERE b.org_id = sqlc.arg('org_id')
  AND b.deleted_at IS NULL
  AND (sqlc.narg('site_id')::bigint IS NULL OR b.site_id = sqlc.narg('site_id')::bigint)
  AND (sqlc.narg('unassigned_only')::boolean IS NULL
       OR sqlc.narg('unassigned_only')::boolean = false
       OR b.site_id IS NULL)
ORDER BY b.name;

-- name: UpdateBuilding :exec
UPDATE building
SET name                     = sqlc.arg('name'),
    description              = sqlc.narg('description'),
    power_kw                 = sqlc.narg('power_kw'),
    overhead_kw              = sqlc.narg('overhead_kw'),
    aisles                   = sqlc.narg('aisles'),
    physical_rack_count      = sqlc.narg('physical_rack_count'),
    racks_per_aisle          = sqlc.narg('racks_per_aisle'),
    default_rack_rows        = sqlc.narg('default_rack_rows'),
    default_rack_columns     = sqlc.narg('default_rack_columns'),
    default_rack_order_index = sqlc.arg('default_rack_order_index'),
    updated_at               = CURRENT_TIMESTAMP
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL;

-- name: AssignBuildingToSite :execrows
-- Move a building to a different site (or to "unassigned" by passing
-- NULL). The cross-collection invariant (no rack in the building
-- contains a device assigned to a different site) is enforced in the
-- service layer before this UPDATE runs.
UPDATE building
SET site_id = sqlc.narg('site_id'),
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL;

-- name: SoftDeleteBuilding :execrows
-- Caller is expected to also unassign the building's racks in the same
-- transaction (cascade-unassign — see plan J3).
UPDATE building
SET deleted_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL;

-- name: UnassignRacksFromBuilding :execrows
-- Sets device_set_rack.building_id = NULL for every rack pointing at the
-- given building. Org guard reads `device_set_rack.org_id` directly
-- (denormalized from device_set in migration 000046, kept in lockstep
-- via the composite FK on `(device_set_id, org_id) → device_set(id,
-- org_id)`).
UPDATE device_set_rack
SET building_id = NULL
WHERE org_id = sqlc.arg('org_id')
  AND building_id = sqlc.arg('building_id');

-- name: BuildingBelongsToOrg :one
SELECT EXISTS(
    SELECT 1 FROM building
    WHERE id = sqlc.arg('id')
      AND org_id = sqlc.arg('org_id')
      AND deleted_at IS NULL
) AS belongs;
