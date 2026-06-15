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
    WHERE ds.deleted_at IS NULL
      AND dsr.building_id IS NOT NULL
      AND dsr.org_id = sqlc.arg('org_id')
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
-- Sets device_set_rack.building_id = NULL (and clears the free-form
-- zone label + grid position) for every live rack pointing at the
-- given building. Org guard reads `device_set_rack.org_id` directly
-- (denormalized from device_set in migration 000046, kept in lockstep
-- via the composite FK on `(device_set_id, org_id) →
-- device_set(id, org_id)`). The EXISTS subquery on device_set skips
-- soft-deleted rack collections so the cascade count matches
-- ListBuildings.rack_count's filter. aisle_index / position_in_aisle
-- are cleared because they're meaningless without a parent building
-- (the CHECK constraint on device_set_rack enforces this).
UPDATE device_set_rack dsr
SET building_id = NULL,
    zone = NULL,
    aisle_index = NULL,
    position_in_aisle = NULL
WHERE dsr.org_id = sqlc.arg('org_id')
  AND dsr.building_id = sqlc.arg('building_id')
  AND EXISTS (
      SELECT 1 FROM device_set ds
      WHERE ds.id = dsr.device_set_id
        AND ds.deleted_at IS NULL
  );

-- name: ListBuildingRacks :many
-- Returns racks currently assigned to a building with their grid
-- position. Used by ManageBuildingModal to seed the layout grid.
-- Excludes soft-deleted rack collections; org guard is checked
-- against the denormalized org_id on device_set_rack.
--
-- Cursor-paginated by (ds.label, dsr.device_set_id). The cursor pair
-- breaks ties deterministically when labels collide. cursor_label /
-- cursor_id are NULL on the first page; when provided, only rows
-- strictly greater than the cursor are returned. Caller asks for
-- `limit_n + 1` rows to detect whether more pages exist.
SELECT
    dsr.device_set_id AS rack_id,
    ds.label          AS rack_label,
    dsr.aisle_index,
    dsr.position_in_aisle
FROM device_set_rack dsr
JOIN device_set ds ON ds.id = dsr.device_set_id
WHERE dsr.org_id = sqlc.arg('org_id')
  AND dsr.building_id = sqlc.arg('building_id')
  AND ds.deleted_at IS NULL
  AND (
       sqlc.narg('cursor_label')::text IS NULL
    OR (ds.label, dsr.device_set_id) > (sqlc.narg('cursor_label')::text, sqlc.narg('cursor_id')::bigint)
  )
ORDER BY ds.label, dsr.device_set_id
LIMIT sqlc.arg('limit_n')::int;

-- name: ListRacksOutsideBuildingBounds :many
-- Returns the first rack row whose (aisle_index, position_in_aisle)
-- would fall outside the proposed (aisles, racks_per_aisle) layout.
-- Used by UpdateBuilding's shrink guard which only needs proof of
-- one orphan to reject the shrink — caller surfaces the label +
-- coordinates in the error and stops. LIMIT 1 keeps the scan cheap
-- on large buildings.
SELECT
    dsr.device_set_id AS rack_id,
    ds.label          AS rack_label,
    dsr.aisle_index,
    dsr.position_in_aisle
FROM device_set_rack dsr
JOIN device_set ds ON ds.id = dsr.device_set_id
WHERE dsr.org_id = sqlc.arg('org_id')
  AND dsr.building_id = sqlc.arg('building_id')
  AND ds.deleted_at IS NULL
  AND dsr.aisle_index IS NOT NULL
  AND dsr.position_in_aisle IS NOT NULL
  AND (
       dsr.aisle_index >= sqlc.arg('new_aisles')::int
    OR dsr.position_in_aisle >= sqlc.arg('new_racks_per_aisle')::int
  )
ORDER BY ds.label
LIMIT 1;

-- name: SetRackBuildingPosition :exec
-- Writes the rack's grid placement (aisle_index, position_in_aisle).
-- Caller must have already set building_id via UpdateRackPlacement —
-- this query intentionally does not touch building_id so the two
-- writes stay separable. Both fields are paired by application logic
-- and the device_set_rack CHECK constraint: passing one without the
-- other is rejected at the SQL layer.
UPDATE device_set_rack
SET aisle_index = sqlc.narg('aisle_index')::int,
    position_in_aisle = sqlc.narg('position_in_aisle')::int
WHERE device_set_id = sqlc.arg('rack_id')
  AND org_id = sqlc.arg('org_id');

-- name: SetRackBuildingPositionBulkClear :exec
-- Bulk variant of SetRackBuildingPosition that nulls (aisle_index,
-- position_in_aisle) for every rack in @rack_ids. Used by
-- AssignRacksToBuilding's pass-1 vacate so every rack in the batch
-- holds NULL position before pass-2 reclaims cells. Mirrors the
-- single-row query's intentional no-touch on building_id —
-- UpdateRackPlacement already settled that.
UPDATE device_set_rack
SET aisle_index = NULL,
    position_in_aisle = NULL
WHERE device_set_id = ANY(sqlc.arg('rack_ids')::bigint[])
  AND org_id = sqlc.arg('org_id');

-- name: SetRackBuildingPositionBulkPlace :exec
-- Bulk variant of SetRackBuildingPosition that writes per-rack
-- (aisle_index, position_in_aisle) via parallel arrays joined on
-- ordinal position. Used by AssignRacksToBuilding's pass-2 after
-- pass-1 has vacated every cell touched by the batch. Arrays must be
-- the same length and parallel-aligned with @rack_ids; callers that
-- have a "place nothing" pass-2 should skip the query entirely.
UPDATE device_set_rack dsr
SET aisle_index = u.aisle_index,
    position_in_aisle = u.position_in_aisle
FROM (
    SELECT
        rack_ids[i]            AS rack_id,
        aisle_indexes[i]       AS aisle_index,
        position_in_aisles[i]  AS position_in_aisle
    FROM (
        SELECT
            sqlc.arg('rack_ids')::bigint[]          AS rack_ids,
            sqlc.arg('aisle_indexes')::int[]        AS aisle_indexes,
            sqlc.arg('position_in_aisles')::int[]   AS position_in_aisles
    ) arrs
    CROSS JOIN generate_subscripts(arrs.rack_ids, 1) AS i
) AS u
WHERE dsr.device_set_id = u.rack_id
  AND dsr.org_id = sqlc.arg('org_id');

-- name: AssignBuildingsToSiteBulk :execrows
-- Bulk variant of AssignBuildingToSite. Updates building.site_id for
-- every building in @building_ids in one statement. Caller is
-- expected to have row-locked each building first (canonical lock
-- order: site → buildings). Returns the row count of buildings
-- actually moved (skips soft-deleted rows).
UPDATE building
SET site_id    = sqlc.narg('site_id'),
    updated_at = CURRENT_TIMESTAMP
WHERE id = ANY(sqlc.arg('building_ids')::bigint[])
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL;

-- name: BuildingBelongsToOrg :one
SELECT EXISTS(
    SELECT 1 FROM building
    WHERE id = sqlc.arg('id')
      AND org_id = sqlc.arg('org_id')
      AND deleted_at IS NULL
) AS belongs;

-- name: BuildingsByIDs :many
-- Returns the subset of requested IDs that correspond to live
-- buildings in the org. Caller diffs against the requested set
-- to detect cross-org or missing IDs.
SELECT id
FROM building
WHERE org_id = $1
  AND deleted_at IS NULL
  AND id = ANY(@ids::bigint[]);
