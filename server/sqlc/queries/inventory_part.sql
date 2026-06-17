-- name: CreateInventoryPart :one
INSERT INTO inventory_part (
    org_id, name, type, manufacturer, part_number,
    site_id, on_hand, reorder_point, bin_location
) VALUES (
    sqlc.arg('org_id'),
    sqlc.arg('name'),
    sqlc.arg('type'),
    sqlc.narg('manufacturer'),
    sqlc.narg('part_number'),
    sqlc.narg('site_id'),
    sqlc.arg('on_hand'),
    sqlc.arg('reorder_point'),
    sqlc.narg('bin_location')
)
RETURNING *;

-- name: GetInventoryPart :one
SELECT *
FROM inventory_part
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL;

-- name: ListInventoryParts :many
SELECT ip.*
FROM inventory_part ip
WHERE ip.org_id = sqlc.arg('org_id')
  AND ip.deleted_at IS NULL
  AND (sqlc.narg('filter_site_ids')::bigint[] IS NULL
       OR ip.site_id = ANY(sqlc.narg('filter_site_ids')::bigint[]))
  AND (sqlc.narg('filter_types')::text[] IS NULL
       OR ip.type = ANY(sqlc.narg('filter_types')::text[]))
  AND (sqlc.narg('filter_low_stock')::boolean IS NULL
       OR sqlc.narg('filter_low_stock')::boolean = false
       OR (ip.on_hand - ip.allocated) <= ip.reorder_point)
  AND (sqlc.narg('cursor_id')::bigint IS NULL
       OR ip.id < sqlc.narg('cursor_id')::bigint)
ORDER BY ip.name, ip.id
LIMIT sqlc.arg('limit_n')::int;

-- name: UpdateInventoryPart :one
UPDATE inventory_part
SET on_hand       = COALESCE(sqlc.narg('on_hand'), on_hand),
    reorder_point = COALESCE(sqlc.narg('reorder_point'), reorder_point),
    bin_location  = COALESCE(sqlc.narg('bin_location'), bin_location),
    updated_at    = CURRENT_TIMESTAMP
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteInventoryPart :execrows
UPDATE inventory_part
SET deleted_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL;

-- name: GetInventoryInsights :one
-- Aggregate stats for the inventory tab insights row.
SELECT
    COALESCE(SUM(on_hand), 0)::int AS total_on_hand,
    COALESCE(SUM(allocated), 0)::int AS total_allocated,
    COUNT(*) FILTER (WHERE (on_hand - allocated) <= reorder_point)::int AS low_stock_count,
    COUNT(DISTINCT site_id)::int AS sites_count
FROM inventory_part
WHERE org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL;

-- name: ListPartsBySite :many
-- Parts at a given site for the ticket completion part picker.
SELECT *
FROM inventory_part
WHERE org_id = sqlc.arg('org_id')
  AND site_id = sqlc.arg('site_id')
  AND deleted_at IS NULL
  AND (on_hand - allocated) > 0
ORDER BY name;

-- name: DecrementPartStock :exec
-- Decrements on_hand for a part when used in a repair. Called per part
-- in the ticket completion transaction.
UPDATE inventory_part
SET on_hand = on_hand - sqlc.arg('quantity')::int,
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL
  AND on_hand >= sqlc.arg('quantity')::int;

-- name: IncrementPartAllocated :exec
-- Allocates stock to an active repair.
UPDATE inventory_part
SET allocated = allocated + sqlc.arg('quantity')::int,
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL;

-- name: DecrementPartAllocated :exec
-- Releases allocated stock (repair cancelled or completed).
UPDATE inventory_part
SET allocated = GREATEST(0, allocated - sqlc.arg('quantity')::int),
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL;
