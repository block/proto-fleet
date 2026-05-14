-- name: CreateDeviceSet :one
INSERT INTO device_set (org_id, type, label, description)
VALUES ($1, $2, $3, $4)
RETURNING id, org_id, type, label, description, created_at, updated_at;

-- name: CreateRackExtension :exec
-- org_id is denormalized onto device_set_rack so the building FK can be
-- composite-keyed; inherit it from device_set so the caller's org_id
-- must match. site_id / building_id are NULL for unassigned racks.
INSERT INTO device_set_rack (device_set_id, org_id, zone, rows, columns, order_index, cooling_type, site_id, building_id)
SELECT ds.id, ds.org_id, sqlc.arg('zone'), sqlc.arg('rows'), sqlc.arg('columns'), sqlc.arg('order_index'), sqlc.arg('cooling_type'), sqlc.narg('site_id')::bigint, sqlc.narg('building_id')::bigint
FROM device_set ds
WHERE ds.id = sqlc.arg('device_set_id') AND ds.org_id = sqlc.arg('org_id') AND ds.deleted_at IS NULL;

-- name: GetDeviceSet :one
SELECT ds.id, ds.type, ds.label, ds.description, ds.created_at, ds.updated_at,
       COUNT(dsm.id)::int AS device_count
FROM device_set ds
LEFT JOIN device_set_membership dsm ON ds.id = dsm.device_set_id
WHERE ds.id = $1 AND ds.org_id = $2 AND ds.deleted_at IS NULL
GROUP BY ds.id;

-- name: GetRackInfo :one
SELECT dsr.zone, dsr.rows, dsr.columns, dsr.order_index, dsr.cooling_type, dsr.site_id, dsr.building_id
FROM device_set_rack dsr
JOIN device_set ds ON dsr.device_set_id = ds.id
WHERE dsr.device_set_id = $1 AND ds.org_id = $2 AND ds.deleted_at IS NULL;

-- name: LockRackPlacementForWrite :one
-- Locks device_set + rack rows FOR UPDATE and returns current placement.
-- Must run after the site/building locks (canonical lock order).
SELECT dsr.site_id, dsr.building_id, dsr.zone
FROM device_set_rack dsr
JOIN device_set ds ON dsr.device_set_id = ds.id
WHERE dsr.device_set_id = $1 AND ds.org_id = $2 AND ds.deleted_at IS NULL
FOR UPDATE;

-- name: GetBuildingSite :one
-- Returns the building's parent site_id, excluding soft-deleted rows.
SELECT site_id
FROM building
WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL;

-- name: UnassignDeviceSitesByRack :execrows
-- Nulls device.site_id for paired rack members whose site_id matches
-- the rack's stamped site. No-op when the rack has no site; preserves
-- direct assignments where device.site_id has diverged from the rack.
UPDATE device d
SET site_id = NULL,
    updated_at = CURRENT_TIMESTAMP
FROM device_set_membership dsm
JOIN device_set_rack dsr ON dsr.device_set_id = dsm.device_set_id AND dsr.org_id = dsm.org_id
WHERE dsm.device_set_id = $1
  AND dsm.org_id = $2
  AND dsm.device_set_type = 'rack'
  AND dsm.device_id = d.id
  AND d.deleted_at IS NULL
  AND dsr.site_id IS NOT NULL
  AND d.site_id IS NOT DISTINCT FROM dsr.site_id;

-- name: CascadeRackDeviceSites :execrows
-- Rewrites device.site_id to target_site_id for every paired member of
-- the rack. NULL target unassigns. IS DISTINCT FROM skips no-op rows.
UPDATE device d
SET site_id = sqlc.narg('target_site_id')::bigint,
    updated_at = CURRENT_TIMESTAMP
FROM device_set_membership dsm
WHERE dsm.device_set_id = $1
  AND dsm.org_id = $2
  AND dsm.device_set_type = 'rack'
  AND dsm.device_id = d.id
  AND d.deleted_at IS NULL
  AND d.site_id IS DISTINCT FROM sqlc.narg('target_site_id')::bigint;

-- name: GetDeviceSiteIDsByMembership :many
-- Returns device_identifier + current site_id for every rack member;
-- used to capture prior sites in the cascade activity-log metadata.
SELECT d.device_identifier, d.site_id
FROM device_set_membership dsm
JOIN device d ON dsm.device_id = d.id
WHERE dsm.device_set_id = $1
  AND dsm.org_id = $2
  AND dsm.device_set_type = 'rack'
  AND d.deleted_at IS NULL;

-- name: UpdateDeviceSetLabel :exec
UPDATE device_set
SET label = $1, updated_at = CURRENT_TIMESTAMP
WHERE id = $2 AND org_id = $3 AND deleted_at IS NULL;

-- name: UpdateDeviceSetDescription :exec
UPDATE device_set
SET description = $1, updated_at = CURRENT_TIMESTAMP
WHERE id = $2 AND org_id = $3 AND deleted_at IS NULL;

-- name: UpdateDeviceSetLabelAndDescription :exec
UPDATE device_set
SET label = $1, description = $2, updated_at = CURRENT_TIMESTAMP
WHERE id = $3 AND org_id = $4 AND deleted_at IS NULL;

-- name: UpdateRackInfo :exec
UPDATE device_set_rack
SET zone = $1, rows = $2, columns = $3, order_index = $4, cooling_type = $5
WHERE device_set_id = $6
  AND EXISTS (SELECT 1 FROM device_set ds WHERE ds.id = $6 AND ds.org_id = $7 AND ds.deleted_at IS NULL);

-- name: UpdateRackPlacement :exec
-- Sets the rack's site_id, building_id, and zone atomically. NULL
-- values unassign placement; caller clears zone via empty string.
UPDATE device_set_rack
SET site_id = sqlc.narg('site_id')::bigint,
    building_id = sqlc.narg('building_id')::bigint,
    zone = sqlc.arg('zone')
WHERE device_set_id = sqlc.arg('device_set_id')
  AND EXISTS (
    SELECT 1 FROM device_set ds
    WHERE ds.id = sqlc.arg('device_set_id')
      AND ds.org_id = sqlc.arg('org_id')
      AND ds.deleted_at IS NULL
  );

-- name: SoftDeleteDeviceSet :execrows
UPDATE device_set
SET deleted_at = CURRENT_TIMESTAMP
WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL;

-- name: DeviceSetBelongsToOrg :one
SELECT EXISTS(
    SELECT 1 FROM device_set
    WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL
) AS belongs;

-- name: GetDeviceSetType :one
SELECT type FROM device_set
WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL;

-- name: AddDevicesToDeviceSet :execrows
INSERT INTO device_set_membership (org_id, device_set_id, device_set_type, device_id, device_identifier)
SELECT $1, $2, ds.type, d.id, d.device_identifier
FROM device d
CROSS JOIN device_set ds
WHERE d.device_identifier = ANY(@device_identifiers::text[])
  AND d.org_id = $1
  AND d.deleted_at IS NULL
  AND ds.id = $2
  AND ds.deleted_at IS NULL
ON CONFLICT (device_set_id, device_id) DO NOTHING;

-- name: GetAddedDeviceSiteConflicts :many
-- Returns prior + target site_id for devices being added to a rack
-- where they differ. Empty for racks without a stamped site.
SELECT d.device_identifier, d.site_id AS prior_site_id, dsr.site_id AS target_site_id
FROM device d
JOIN device_set ds ON ds.id = $2 AND ds.org_id = $1 AND ds.deleted_at IS NULL
JOIN device_set_rack dsr ON dsr.device_set_id = ds.id AND dsr.org_id = $1
WHERE d.device_identifier = ANY(@device_identifiers::text[])
  AND d.org_id = $1
  AND d.deleted_at IS NULL
  AND ds.type = 'rack'
  AND dsr.site_id IS NOT NULL
  AND d.site_id IS DISTINCT FROM dsr.site_id;

-- name: CascadeAddedDeviceSites :execrows
-- Rewrites device.site_id to rack.site_id for added rack members
-- whose current site differs. No-op for groups or site-less racks.
UPDATE device d
SET site_id = dsr.site_id,
    updated_at = CURRENT_TIMESTAMP
FROM device_set ds
JOIN device_set_rack dsr ON dsr.device_set_id = ds.id AND dsr.org_id = ds.org_id
WHERE d.device_identifier = ANY(@device_identifiers::text[])
  AND d.org_id = $1
  AND d.deleted_at IS NULL
  AND ds.id = $2
  AND ds.org_id = $1
  AND ds.deleted_at IS NULL
  AND ds.type = 'rack'
  AND dsr.site_id IS NOT NULL
  AND d.site_id IS DISTINCT FROM dsr.site_id;

-- name: RemoveAllDevicesFromDeviceSet :execrows
DELETE FROM device_set_membership
WHERE device_set_id = $1
  AND org_id = $2;

-- name: RemoveDevicesFromDeviceSet :execrows
DELETE FROM device_set_membership
WHERE device_set_id = $1
  AND org_id = $2
  AND device_identifier = ANY(@device_identifiers::text[]);

-- name: ListDeviceSetMembersPaginated :many
SELECT dsm.id, dsm.device_identifier, dsm.created_at,
       rs.row AS slot_row, rs.col AS slot_col
FROM device_set_membership dsm
LEFT JOIN rack_slot rs ON dsm.device_set_id = rs.device_set_id AND dsm.device_id = rs.device_id
WHERE dsm.device_set_id = $1 AND dsm.org_id = $2
ORDER BY dsm.created_at DESC, dsm.id DESC
LIMIT $3;

-- name: ListDeviceSetMembersPaginatedAfter :many
SELECT dsm.id, dsm.device_identifier, dsm.created_at,
       rs.row AS slot_row, rs.col AS slot_col
FROM device_set_membership dsm
LEFT JOIN rack_slot rs ON dsm.device_set_id = rs.device_set_id AND dsm.device_id = rs.device_id
WHERE dsm.device_set_id = $1 AND dsm.org_id = $2
  AND (dsm.created_at < @cursor_created_at::timestamptz OR (dsm.created_at = @cursor_created_at::timestamptz AND dsm.id < @cursor_id::bigint))
ORDER BY dsm.created_at DESC, dsm.id DESC
LIMIT $3;

-- name: GetDeviceDeviceSets :many
SELECT ds.id, ds.type, ds.label, ds.description, ds.created_at, ds.updated_at,
       (SELECT COUNT(*) FROM device_set_membership WHERE device_set_id = ds.id)::int AS device_count
FROM device_set ds
JOIN device_set_membership dsm ON ds.id = dsm.device_set_id
WHERE dsm.device_identifier = $1
  AND dsm.org_id = $2
  AND ds.deleted_at IS NULL
ORDER BY ds.label ASC;

-- name: GetDeviceDeviceSetsByType :many
SELECT ds.id, ds.type, ds.label, ds.description, ds.created_at, ds.updated_at,
       (SELECT COUNT(*) FROM device_set_membership WHERE device_set_id = ds.id)::int AS device_count
FROM device_set ds
JOIN device_set_membership dsm ON ds.id = dsm.device_set_id
WHERE dsm.device_identifier = $1
  AND dsm.org_id = $2
  AND ds.type = $3
  AND ds.deleted_at IS NULL
ORDER BY ds.label ASC;

-- name: GetGroupLabelsForDevices :many
-- Batch query to get group labels for multiple devices at once (for miner list)
SELECT dsm.device_identifier, ds.label
FROM device_set_membership dsm
JOIN device_set ds ON dsm.device_set_id = ds.id
WHERE dsm.device_identifier = ANY(@device_identifiers::text[])
  AND dsm.org_id = $1
  AND ds.type = 'group'
  AND ds.deleted_at IS NULL
ORDER BY dsm.device_identifier, ds.label;

-- name: GetRackDetailsForDevices :many
-- Batch query to get rack label and formatted slot position for multiple devices at once.
-- Returns at most one rack per device due to partial unique index.
SELECT
  dsm.device_identifier,
  ds.label,
  CASE
    WHEN rs.row IS NULL OR rs.col IS NULL OR dsr.order_index NOT IN (1, 2, 3, 4) THEN ''
    ELSE (
      CASE
        WHEN (
          CASE dsr.order_index
            WHEN 1 THEN (dsr.rows - 1 - rs.row) * dsr.columns + rs.col + 1
            WHEN 2 THEN rs.row * dsr.columns + rs.col + 1
            WHEN 3 THEN (dsr.rows - 1 - rs.row) * dsr.columns + (dsr.columns - 1 - rs.col) + 1
            ELSE rs.row * dsr.columns + (dsr.columns - 1 - rs.col) + 1
          END
        ) < 10 THEN LPAD((
          CASE dsr.order_index
            WHEN 1 THEN (dsr.rows - 1 - rs.row) * dsr.columns + rs.col + 1
            WHEN 2 THEN rs.row * dsr.columns + rs.col + 1
            WHEN 3 THEN (dsr.rows - 1 - rs.row) * dsr.columns + (dsr.columns - 1 - rs.col) + 1
            ELSE rs.row * dsr.columns + (dsr.columns - 1 - rs.col) + 1
          END
        )::text, 2, '0')
        ELSE (
          CASE dsr.order_index
            WHEN 1 THEN (dsr.rows - 1 - rs.row) * dsr.columns + rs.col + 1
            WHEN 2 THEN rs.row * dsr.columns + rs.col + 1
            WHEN 3 THEN (dsr.rows - 1 - rs.row) * dsr.columns + (dsr.columns - 1 - rs.col) + 1
            ELSE rs.row * dsr.columns + (dsr.columns - 1 - rs.col) + 1
          END
        )::text
      END
    )
  END::text AS position
FROM device_set_membership dsm
JOIN device_set ds ON dsm.device_set_id = ds.id
LEFT JOIN device_set_rack dsr ON dsm.device_set_id = dsr.device_set_id
LEFT JOIN rack_slot rs ON dsm.device_set_id = rs.device_set_id AND dsm.device_id = rs.device_id
WHERE dsm.device_identifier = ANY(@device_identifiers::text[])
  AND dsm.org_id = $1
  AND ds.type = 'rack'
  AND ds.deleted_at IS NULL
ORDER BY dsm.device_identifier;

-- name: SetRackSlotPosition :exec
INSERT INTO rack_slot (device_set_id, device_id, row, col)
SELECT dsm.device_set_id, dsm.device_id, @row::int, @col::int
FROM device_set_membership dsm
JOIN device_set ds ON dsm.device_set_id = ds.id
WHERE dsm.device_set_id = $1
  AND dsm.device_identifier = $2
  AND ds.org_id = $3
  AND ds.deleted_at IS NULL
ON CONFLICT (device_set_id, device_id) DO UPDATE
SET row = EXCLUDED.row, col = EXCLUDED.col;

-- name: ClearRackSlotPosition :exec
DELETE FROM rack_slot rs
WHERE rs.device_set_id = $1
  AND rs.device_id = (
    SELECT dsm.device_id FROM device_set_membership dsm
    JOIN device_set ds ON dsm.device_set_id = ds.id
    WHERE dsm.device_set_id = $1 AND dsm.device_identifier = $2
      AND ds.org_id = $3 AND ds.deleted_at IS NULL
  );

-- name: GetRackSlots :many
SELECT dsm.device_identifier, rs.row, rs.col
FROM rack_slot rs
JOIN device_set_membership dsm ON rs.device_set_id = dsm.device_set_id AND rs.device_id = dsm.device_id
JOIN device_set ds ON rs.device_set_id = ds.id
WHERE rs.device_set_id = $1 AND ds.org_id = $2 AND ds.deleted_at IS NULL
ORDER BY rs.row, rs.col;

-- name: GetRackInfoBatch :many
SELECT dsr.device_set_id, dsr.zone, dsr.rows, dsr.columns, dsr.order_index, dsr.cooling_type, dsr.site_id, dsr.building_id
FROM device_set_rack dsr
JOIN device_set ds ON dsr.device_set_id = ds.id
WHERE dsr.device_set_id = ANY(@device_set_ids::bigint[]) AND ds.org_id = $1 AND ds.deleted_at IS NULL;

-- name: GetDeviceSetTypesBatch :many
SELECT id, type FROM device_set
WHERE org_id = $1 AND deleted_at IS NULL AND id = ANY(@device_set_ids::bigint[]);

-- name: ListRackZones :many
SELECT DISTINCT dsr.zone
FROM device_set_rack dsr
JOIN device_set ds ON dsr.device_set_id = ds.id
WHERE ds.org_id = $1
  AND ds.deleted_at IS NULL
  AND dsr.zone IS NOT NULL
  AND dsr.zone != ''
ORDER BY dsr.zone;

-- name: ListRackTypes :many
SELECT dsr.rows, dsr.columns, COUNT(*)::int AS rack_count
FROM device_set_rack dsr
JOIN device_set ds ON dsr.device_set_id = ds.id
WHERE ds.org_id = $1 AND ds.deleted_at IS NULL
GROUP BY dsr.rows, dsr.columns
ORDER BY MAX(ds.created_at) DESC;

-- name: GetDeviceIdentifiersByDeviceSetID :many
SELECT dsm.device_identifier
FROM device_set_membership dsm
JOIN device_set ds ON dsm.device_set_id = ds.id
WHERE dsm.device_set_id = $1
  AND dsm.org_id = $2
  AND ds.org_id = $2
  AND ds.deleted_at IS NULL;
