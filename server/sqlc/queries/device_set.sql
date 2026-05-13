-- name: CreateDeviceSet :one
INSERT INTO device_set (org_id, type, label, description)
VALUES ($1, $2, $3, $4)
RETURNING id, org_id, type, label, description, created_at, updated_at;

-- name: CreateRackExtension :exec
-- org_id is denormalized onto device_set_rack (see migration 000046) so
-- the building FK can be composite-keyed. The SELECT pulls it from
-- device_set so the rack inherits the parent's org_id; caller's $7 must
-- match (otherwise the WHERE filters the row out and INSERT inserts 0
-- rows). Aliases qualify column refs since both tables now have org_id.
-- site_id / building_id are nullable: caller passes the resolved
-- placement (with building.site_id == rack.site_id when building_id is
-- set), or both NULL for unassigned racks.
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
-- Acquires a row-level write lock on the device_set + rack rows for a
-- rack-edit transaction and returns the rack's current placement +
-- zone. Lock order matches building/site -> device_set -> device_set_rack
-- to keep the cascade tx deadlock-free.
SELECT dsr.site_id, dsr.building_id, dsr.zone
FROM device_set_rack dsr
JOIN device_set ds ON dsr.device_set_id = ds.id
WHERE dsr.device_set_id = $1 AND ds.org_id = $2 AND ds.deleted_at IS NULL
FOR UPDATE;

-- name: GetBuildingSite :one
-- Returns the building's parent site_id so callers can derive a rack's
-- effective site when only building_id is supplied. Soft-deleted
-- buildings are excluded — they cannot accept new rack assignments.
SELECT site_id
FROM building
WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL;

-- name: UnassignDeviceSitesByRack :execrows
-- Clears device.site_id (sets to NULL) for every paired member of a
-- rack. Called by DeleteCollection in the same transaction as the rack
-- soft-delete so devices don't keep pointing at a site they entered via
-- the deleted rack. Mirrors AssignBuildingToSite(target=NULL) cascade
-- semantics: the device lands in the Unassigned bucket, the operator
-- can explicitly reassign later.
UPDATE device d
SET site_id = NULL,
    updated_at = CURRENT_TIMESTAMP
FROM device_set_membership dsm
WHERE dsm.device_set_id = $1
  AND dsm.org_id = $2
  AND dsm.device_set_type = 'rack'
  AND dsm.device_id = d.id
  AND d.deleted_at IS NULL
  AND d.site_id IS NOT NULL;

-- name: CascadeRackDeviceSites :execrows
-- Rewrites device.site_id to the rack's new site for every paired
-- device that is currently a member of the rack. Used by the rack
-- edit/move cascade so descendant devices follow their rack across
-- building / site boundaries. NULL is a valid target_site_id (rack
-- moves to fully unassigned).
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
-- Returns the device_identifier + current site_id for every device that
-- belongs to the rack. Used by the cascade flow to capture each
-- device's prior site in the activity-log metadata before we rewrite.
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
-- Sets the rack's site_id, building_id, and zone together so the rack
-- edit/move cascade can update placement atomically with the descendant
-- device site rewrite. NULL values are accepted for site_id and
-- building_id (fully-unassigned racks); zone is cleared by the caller
-- via an empty string when the rack crosses a building boundary.
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
-- Returns prior site_id for devices being added to a rack whose current
-- site_id differs from the target rack's site_id. Issued before the
-- cascade UPDATE so callers can stamp the prior site on the activity-log
-- row. Returns the empty set for group targets (rack.site_id IS NULL)
-- or when no devices need rewriting.
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
-- Rewrites device.site_id to the rack's site for every paired device
-- in the supplied identifier list whose current site differs from the
-- rack's site_id. Executes as a no-op when the rack has no site or
-- when the target is a group set (ds.type != 'rack'). Mirrors the
-- cascade semantics of AssignBuildingToSite for the add-to-rack path.
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
