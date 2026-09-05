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
RETURNING id;

-- name: LockInventorySites :many
-- Inventory creation takes share locks in a deterministic order so a
-- concurrent soft deletion cannot update deleted_at between validation and insert.
SELECT id
FROM site
WHERE org_id = sqlc.arg('org_id')
  AND id = ANY(sqlc.arg('site_ids')::bigint[])
  AND deleted_at IS NULL
ORDER BY id
FOR SHARE;

-- name: GetInventoryPart :one
SELECT
    ip.id, ip.org_id, ip.name, ip.type, ip.manufacturer, ip.part_number,
    ip.site_id, COALESCE(s.name, '') AS site_name,
    ip.on_hand, ip.allocated, ip.reorder_point, ip.bin_location,
    ip.created_at, ip.updated_at, ip.deleted_at
FROM inventory_part ip
LEFT JOIN site s
  ON s.id = ip.site_id
 AND s.org_id = ip.org_id
 AND s.deleted_at IS NULL
WHERE ip.id = sqlc.arg('id')
  AND ip.org_id = sqlc.arg('org_id')
  AND ip.deleted_at IS NULL;

-- name: GetInventoryPartForUpdate :one
SELECT
    ip.id, ip.org_id, ip.name, ip.type, ip.manufacturer, ip.part_number,
    ip.site_id, COALESCE(s.name, '') AS site_name,
    ip.on_hand, ip.allocated, ip.reorder_point, ip.bin_location,
    ip.created_at, ip.updated_at, ip.deleted_at
FROM inventory_part ip
LEFT JOIN site s
  ON s.id = ip.site_id
 AND s.org_id = ip.org_id
 AND s.deleted_at IS NULL
WHERE ip.id = sqlc.arg('id')
  AND ip.org_id = sqlc.arg('org_id')
  AND ip.deleted_at IS NULL
FOR UPDATE OF ip;

-- name: ListInventoryParts :many
SELECT
    ip.id, ip.org_id, ip.name, ip.type, ip.manufacturer, ip.part_number,
    ip.site_id, COALESCE(s.name, '') AS site_name,
    ip.on_hand, ip.allocated, ip.reorder_point, ip.bin_location,
    ip.created_at, ip.updated_at, ip.deleted_at
FROM inventory_part ip
LEFT JOIN site s
  ON s.id = ip.site_id
 AND s.org_id = ip.org_id
 AND s.deleted_at IS NULL
WHERE ip.org_id = sqlc.arg('org_id')
  AND ip.deleted_at IS NULL
  AND (sqlc.narg('filter_site_ids')::bigint[] IS NULL
       OR ip.site_id = ANY(sqlc.narg('filter_site_ids')::bigint[]))
  AND (sqlc.narg('filter_types')::text[] IS NULL
       OR ip.type = ANY(sqlc.narg('filter_types')::text[]))
  AND (NOT sqlc.arg('filter_low_stock')::boolean
       OR (ip.on_hand - ip.allocated) <= ip.reorder_point)
  AND (sqlc.narg('cursor_id')::bigint IS NULL
       OR ip.id < sqlc.narg('cursor_id')::bigint)
ORDER BY ip.id DESC
LIMIT sqlc.arg('limit_n')::int;

-- name: CountInventoryParts :one
SELECT COUNT(*)::int
FROM inventory_part ip
WHERE ip.org_id = sqlc.arg('org_id')
  AND ip.deleted_at IS NULL
  AND (sqlc.narg('filter_site_ids')::bigint[] IS NULL
       OR ip.site_id = ANY(sqlc.narg('filter_site_ids')::bigint[]))
  AND (sqlc.narg('filter_types')::text[] IS NULL
       OR ip.type = ANY(sqlc.narg('filter_types')::text[]))
  AND (NOT sqlc.arg('filter_low_stock')::boolean
       OR (ip.on_hand - ip.allocated) <= ip.reorder_point);

-- name: UpdateInventoryPart :execrows
UPDATE inventory_part
SET on_hand       = COALESCE(sqlc.narg('on_hand'), on_hand),
    reorder_point = COALESCE(sqlc.narg('reorder_point'), reorder_point),
    bin_location  = COALESCE(sqlc.narg('bin_location'), bin_location),
    site_id       = COALESCE(sqlc.narg('site_id'), site_id),
    updated_at    = CURRENT_TIMESTAMP
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL
  AND COALESCE(sqlc.narg('on_hand'), on_hand) >= allocated
  AND (sqlc.narg('expected_on_hand')::integer IS NULL
       OR on_hand = sqlc.narg('expected_on_hand')::integer)
  AND (sqlc.narg('site_id')::bigint IS NULL
       OR site_id IS NOT DISTINCT FROM sqlc.narg('site_id')::bigint
       OR allocated = 0);

-- name: SoftDeleteInventoryPart :execrows
UPDATE inventory_part
SET deleted_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL
  AND allocated = 0;

-- name: GetInventoryInsights :one
SELECT
    COALESCE(SUM(on_hand), 0)::int AS total_on_hand,
    COALESCE(SUM(allocated), 0)::int AS total_allocated,
    COUNT(*) FILTER (WHERE (on_hand - allocated) <= reorder_point)::int AS low_stock_count,
    COUNT(DISTINCT site_id)::int AS sites_count,
    COALESCE(array_agg(DISTINCT type ORDER BY type), '{}')::text[] AS part_types
FROM inventory_part
WHERE org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL;

-- name: ListPartsBySite :many
SELECT
    ip.id, ip.org_id, ip.name, ip.type, ip.manufacturer, ip.part_number,
    ip.site_id, COALESCE(s.name, '') AS site_name,
    ip.on_hand, ip.allocated, ip.reorder_point, ip.bin_location,
    ip.created_at, ip.updated_at, ip.deleted_at
FROM inventory_part ip
JOIN site s
  ON s.id = ip.site_id
 AND s.org_id = ip.org_id
 AND s.deleted_at IS NULL
WHERE ip.org_id = sqlc.arg('org_id')
  AND ip.site_id = sqlc.arg('site_id')
  AND ip.deleted_at IS NULL
  AND (ip.on_hand - ip.allocated) > 0
ORDER BY ip.name, ip.id;

-- name: ReserveInventoryPart :execrows
UPDATE inventory_part
SET allocated = allocated + sqlc.arg('quantity')::int,
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL
  AND on_hand - allocated >= sqlc.arg('quantity')::int;

-- name: ReleaseInventoryPart :execrows
UPDATE inventory_part
SET allocated = allocated - sqlc.arg('quantity')::int,
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL
  AND allocated >= sqlc.arg('quantity')::int;

-- name: ConsumeReservedInventoryPart :execrows
UPDATE inventory_part
SET on_hand = on_hand - sqlc.arg('quantity')::int,
    allocated = allocated - sqlc.arg('quantity')::int,
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL
  AND allocated >= sqlc.arg('quantity')::int
  AND on_hand >= sqlc.arg('quantity')::int;

-- name: InventoryPartExistsBySiteAndName :one
SELECT EXISTS (
    SELECT 1
    FROM inventory_part
    WHERE org_id = sqlc.arg('org_id')
      AND site_id IS NOT DISTINCT FROM sqlc.narg('site_id')::bigint
      AND LOWER(name) = LOWER(sqlc.arg('name'))
      AND deleted_at IS NULL
);

-- name: ResolveInventorySiteByName :one
SELECT id
FROM site
WHERE org_id = sqlc.arg('org_id')
  AND name = sqlc.arg('name')
  AND deleted_at IS NULL;
