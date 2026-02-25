-- name: CreateCollection :one
INSERT INTO device_collection (org_id, type, label, description)
VALUES ($1, $2, $3, $4)
RETURNING id, org_id, type, label, description, created_at, updated_at;

-- name: CreateRackExtension :exec
INSERT INTO device_collection_rack (collection_id, location, rows, columns)
VALUES ($1, $2, $3, $4);

-- name: GetCollection :one
SELECT dc.id, dc.type, dc.label, dc.description, dc.created_at, dc.updated_at,
       COUNT(dcm.id)::int AS device_count
FROM device_collection dc
LEFT JOIN device_collection_membership dcm ON dc.id = dcm.collection_id
WHERE dc.id = $1 AND dc.org_id = $2 AND dc.deleted_at IS NULL
GROUP BY dc.id;

-- name: GetRackInfo :one
SELECT location, rows, columns
FROM device_collection_rack
WHERE collection_id = $1;

-- name: UpdateCollectionLabel :exec
UPDATE device_collection
SET label = $1, updated_at = CURRENT_TIMESTAMP
WHERE id = $2 AND org_id = $3 AND deleted_at IS NULL;

-- name: UpdateCollectionDescription :exec
UPDATE device_collection
SET description = $1, updated_at = CURRENT_TIMESTAMP
WHERE id = $2 AND org_id = $3 AND deleted_at IS NULL;

-- name: UpdateCollectionLabelAndDescription :exec
UPDATE device_collection
SET label = $1, description = $2, updated_at = CURRENT_TIMESTAMP
WHERE id = $3 AND org_id = $4 AND deleted_at IS NULL;

-- name: UpdateRackInfo :exec
UPDATE device_collection_rack
SET location = $1, rows = $2, columns = $3
WHERE collection_id = $4;

-- name: SoftDeleteCollection :execrows
UPDATE device_collection
SET deleted_at = CURRENT_TIMESTAMP
WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL;

-- name: ListCollectionsPaginated :many
SELECT dc.id, dc.type, dc.label, dc.description, dc.created_at, dc.updated_at,
       COUNT(dcm.id)::int AS device_count
FROM device_collection dc
LEFT JOIN device_collection_membership dcm ON dc.id = dcm.collection_id
WHERE dc.org_id = $1 AND dc.deleted_at IS NULL
GROUP BY dc.id
ORDER BY dc.label ASC, dc.id ASC
LIMIT $2;

-- name: ListCollectionsPaginatedAfter :many
SELECT dc.id, dc.type, dc.label, dc.description, dc.created_at, dc.updated_at,
       COUNT(dcm.id)::int AS device_count
FROM device_collection dc
LEFT JOIN device_collection_membership dcm ON dc.id = dcm.collection_id
WHERE dc.org_id = $1 AND dc.deleted_at IS NULL
  AND (dc.label > @cursor_label::text OR (dc.label = @cursor_label::text AND dc.id > @cursor_id::bigint))
GROUP BY dc.id
ORDER BY dc.label ASC, dc.id ASC
LIMIT $2;

-- name: ListCollectionsByTypePaginated :many
SELECT dc.id, dc.type, dc.label, dc.description, dc.created_at, dc.updated_at,
       COUNT(dcm.id)::int AS device_count
FROM device_collection dc
LEFT JOIN device_collection_membership dcm ON dc.id = dcm.collection_id
WHERE dc.org_id = $1 AND dc.type = $2 AND dc.deleted_at IS NULL
GROUP BY dc.id
ORDER BY dc.label ASC, dc.id ASC
LIMIT $3;

-- name: ListCollectionsByTypePaginatedAfter :many
SELECT dc.id, dc.type, dc.label, dc.description, dc.created_at, dc.updated_at,
       COUNT(dcm.id)::int AS device_count
FROM device_collection dc
LEFT JOIN device_collection_membership dcm ON dc.id = dcm.collection_id
WHERE dc.org_id = $1 AND dc.type = $2 AND dc.deleted_at IS NULL
  AND (dc.label > @cursor_label::text OR (dc.label = @cursor_label::text AND dc.id > @cursor_id::bigint))
GROUP BY dc.id
ORDER BY dc.label ASC, dc.id ASC
LIMIT $3;

-- name: CollectionBelongsToOrg :one
SELECT EXISTS(
    SELECT 1 FROM device_collection
    WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL
) AS belongs;

-- name: GetCollectionType :one
SELECT type FROM device_collection
WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL;

-- name: AddDevicesToCollection :execrows
INSERT INTO device_collection_membership (org_id, collection_id, collection_type, device_id, device_identifier)
SELECT $1, $2, dc.type, d.id, d.device_identifier
FROM device d
CROSS JOIN device_collection dc
WHERE d.device_identifier = ANY(@device_identifiers::text[])
  AND d.org_id = $1
  AND d.deleted_at IS NULL
  AND dc.id = $2
  AND dc.deleted_at IS NULL
ON CONFLICT (collection_id, device_id) DO NOTHING;

-- name: RemoveDevicesFromCollection :execrows
DELETE FROM device_collection_membership
WHERE collection_id = $1
  AND org_id = $2
  AND device_identifier = ANY(@device_identifiers::text[]);

-- name: ListCollectionMembersPaginated :many
SELECT dcm.id, dcm.device_identifier, dcm.created_at,
       rs.row AS slot_row, rs.col AS slot_col
FROM device_collection_membership dcm
LEFT JOIN rack_slot rs ON dcm.collection_id = rs.collection_id AND dcm.device_id = rs.device_id
WHERE dcm.collection_id = $1 AND dcm.org_id = $2
ORDER BY dcm.created_at DESC, dcm.id DESC
LIMIT $3;

-- name: ListCollectionMembersPaginatedAfter :many
SELECT dcm.id, dcm.device_identifier, dcm.created_at,
       rs.row AS slot_row, rs.col AS slot_col
FROM device_collection_membership dcm
LEFT JOIN rack_slot rs ON dcm.collection_id = rs.collection_id AND dcm.device_id = rs.device_id
WHERE dcm.collection_id = $1 AND dcm.org_id = $2
  AND (dcm.created_at < @cursor_created_at::timestamptz OR (dcm.created_at = @cursor_created_at::timestamptz AND dcm.id < @cursor_id::bigint))
ORDER BY dcm.created_at DESC, dcm.id DESC
LIMIT $3;

-- name: GetDeviceCollections :many
SELECT dc.id, dc.type, dc.label, dc.description, dc.created_at, dc.updated_at,
       (SELECT COUNT(*) FROM device_collection_membership WHERE collection_id = dc.id)::int AS device_count
FROM device_collection dc
JOIN device_collection_membership dcm ON dc.id = dcm.collection_id
WHERE dcm.device_identifier = $1
  AND dcm.org_id = $2
  AND dc.deleted_at IS NULL
ORDER BY dc.label ASC;

-- name: GetDeviceCollectionsByType :many
SELECT dc.id, dc.type, dc.label, dc.description, dc.created_at, dc.updated_at,
       (SELECT COUNT(*) FROM device_collection_membership WHERE collection_id = dc.id)::int AS device_count
FROM device_collection dc
JOIN device_collection_membership dcm ON dc.id = dcm.collection_id
WHERE dcm.device_identifier = $1
  AND dcm.org_id = $2
  AND dc.type = $3
  AND dc.deleted_at IS NULL
ORDER BY dc.label ASC;

-- name: GetGroupLabelsForDevices :many
-- Batch query to get group labels for multiple devices at once (for miner list)
SELECT dcm.device_identifier, dc.label
FROM device_collection_membership dcm
JOIN device_collection dc ON dcm.collection_id = dc.id
WHERE dcm.device_identifier = ANY(@device_identifiers::text[])
  AND dcm.org_id = $1
  AND dc.type = 'group'
  AND dc.deleted_at IS NULL
ORDER BY dcm.device_identifier, dc.label;

-- name: GetRackLabelsForDevices :many
-- Batch query to get rack label for multiple devices at once (for miner list)
-- Returns at most one rack per device due to partial unique index
SELECT dcm.device_identifier, dc.label
FROM device_collection_membership dcm
JOIN device_collection dc ON dcm.collection_id = dc.id
WHERE dcm.device_identifier = ANY(@device_identifiers::text[])
  AND dcm.org_id = $1
  AND dc.type = 'rack'
  AND dc.deleted_at IS NULL
ORDER BY dcm.device_identifier;

-- name: SetRackSlotPosition :exec
INSERT INTO rack_slot (collection_id, device_id, row, col)
SELECT dcm.collection_id, dcm.device_id, @row::int, @col::int
FROM device_collection_membership dcm
WHERE dcm.collection_id = $1
  AND dcm.device_identifier = $2
ON CONFLICT (collection_id, device_id) DO UPDATE
SET row = EXCLUDED.row, col = EXCLUDED.col;

-- name: ClearRackSlotPosition :exec
DELETE FROM rack_slot rs
WHERE rs.collection_id = $1
  AND rs.device_id = (
    SELECT dcm.device_id FROM device_collection_membership dcm
    WHERE dcm.collection_id = $1 AND dcm.device_identifier = $2
  );

-- name: GetRackSlots :many
SELECT dcm.device_identifier, rs.row, rs.col
FROM rack_slot rs
JOIN device_collection_membership dcm ON rs.collection_id = dcm.collection_id AND rs.device_id = dcm.device_id
WHERE rs.collection_id = $1
ORDER BY rs.row, rs.col;
