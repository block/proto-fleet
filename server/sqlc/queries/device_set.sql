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
       COUNT(dsm.id)::int AS device_count,
       dsr.site_id,
       COALESCE(s.name, '') AS site_label,
       dsr.building_id,
       COALESCE(b.name, '') AS building_label
FROM device_set ds
LEFT JOIN device_set_membership dsm ON ds.id = dsm.device_set_id
LEFT JOIN device_set_rack dsr ON dsr.device_set_id = ds.id
LEFT JOIN site s
  ON s.id = dsr.site_id
 AND s.org_id = ds.org_id
 AND s.deleted_at IS NULL
LEFT JOIN building b
  ON b.id = dsr.building_id
 AND b.org_id = ds.org_id
 AND b.deleted_at IS NULL
WHERE ds.id = $1 AND ds.org_id = $2 AND ds.deleted_at IS NULL
GROUP BY ds.id, dsr.site_id, s.name, dsr.building_id, b.name;

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

-- name: UnassignDeviceBuildingsByRack :execrows
-- Building peer of UnassignDeviceSitesByRack. Nulls device.building_id
-- for paired rack members whose building_id matches the rack's stamped
-- building. No-op when the rack has no building; preserves direct
-- "Add miners to building" assignments that diverged from the rack.
UPDATE device d
SET building_id = NULL,
    updated_at = CURRENT_TIMESTAMP
FROM device_set_membership dsm
JOIN device_set_rack dsr ON dsr.device_set_id = dsm.device_set_id AND dsr.org_id = dsm.org_id
WHERE dsm.device_set_id = $1
  AND dsm.org_id = $2
  AND dsm.device_set_type = 'rack'
  AND dsm.device_id = d.id
  AND d.deleted_at IS NULL
  AND dsr.building_id IS NOT NULL
  AND d.building_id IS NOT DISTINCT FROM dsr.building_id;

-- name: CascadeRackDeviceBuildings :execrows
-- Building peer of CascadeRackDeviceSites. Rewrites device.building_id
-- to target_building_id for every paired member of the rack. NULL
-- target unassigns. IS DISTINCT FROM skips no-op rows.
UPDATE device d
SET building_id = sqlc.narg('target_building_id')::bigint,
    updated_at = CURRENT_TIMESTAMP
FROM device_set_membership dsm
WHERE dsm.device_set_id = $1
  AND dsm.org_id = $2
  AND dsm.device_set_type = 'rack'
  AND dsm.device_id = d.id
  AND d.deleted_at IS NULL
  AND d.building_id IS DISTINCT FROM sqlc.narg('target_building_id')::bigint;

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
-- Clears aisle_index / position_in_aisle only when building_id
-- changes (transitions to a different non-null value, or to NULL).
-- A no-op building_id update preserves the existing grid position so
-- SaveRack callers that don't touch building placement (rack rename,
-- cooling change, etc.) don't accidentally nuke the operator's
-- ManageBuildingModal layout work.
UPDATE device_set_rack
SET site_id = sqlc.narg('site_id')::bigint,
    building_id = sqlc.narg('building_id')::bigint,
    zone = sqlc.arg('zone'),
    aisle_index = CASE
        WHEN sqlc.narg('building_id')::bigint IS DISTINCT FROM building_id THEN NULL
        ELSE aisle_index
    END,
    position_in_aisle = CASE
        WHEN sqlc.narg('building_id')::bigint IS DISTINCT FROM building_id THEN NULL
        ELSE position_in_aisle
    END
WHERE device_set_id = sqlc.arg('device_set_id')
  AND EXISTS (
    SELECT 1 FROM device_set ds
    WHERE ds.id = sqlc.arg('device_set_id')
      AND ds.org_id = sqlc.arg('org_id')
      AND ds.deleted_at IS NULL
  );

-- name: UpdateRackPlacementBulkForBuilding :execrows
-- Bulk variant of UpdateRackPlacement scoped to AssignRacksToBuilding.
-- Sets site_id, building_id, and zone for every rack in @rack_ids in a
-- single update.
--
-- Semantics match the per-row UpdateRackPlacement + service-layer
-- zone/site rules:
--
--   * When @target_building_id IS NULL (unassign branch), each rack
--     keeps its current site_id (no cascade fires later). Otherwise
--     every rack is stamped with @target_site_id.
--   * Zone clears to NULL for any rack that had a building and is
--     transitioning to a different (or NULL) building. Racks staying in
--     the same building preserve their zone. NULL (not '') is
--     load-bearing: collection_sort.go orders by zone NULLS LAST, so an
--     empty-string zone would sort as a real zone instead of falling
--     into the NULLS LAST bucket like the per-row path produced.
--   * aisle_index / position_in_aisle clear when building_id changes,
--     matching the single-row CASE.
--
-- Returns the number of affected rows so the service layer can verify
-- every requested rack id resolved to an actual row (defense-in-depth
-- against cross-org or stale ids slipping past the per-rack lock
-- pre-pass).
UPDATE device_set_rack dsr
SET site_id = CASE
        WHEN sqlc.narg('target_building_id')::bigint IS NULL THEN dsr.site_id
        ELSE sqlc.narg('target_site_id')::bigint
    END,
    building_id = sqlc.narg('target_building_id')::bigint,
    zone = CASE
        WHEN dsr.building_id IS NOT NULL
             AND dsr.building_id IS DISTINCT FROM sqlc.narg('target_building_id')::bigint
        THEN NULL
        ELSE dsr.zone
    END,
    aisle_index = CASE
        WHEN sqlc.narg('target_building_id')::bigint IS DISTINCT FROM dsr.building_id THEN NULL
        ELSE dsr.aisle_index
    END,
    position_in_aisle = CASE
        WHEN sqlc.narg('target_building_id')::bigint IS DISTINCT FROM dsr.building_id THEN NULL
        ELSE dsr.position_in_aisle
    END
WHERE dsr.device_set_id = ANY(sqlc.arg('rack_ids')::bigint[])
  AND dsr.org_id = sqlc.arg('org_id')
  AND EXISTS (
    SELECT 1 FROM device_set ds
    WHERE ds.id = dsr.device_set_id
      AND ds.org_id = sqlc.arg('org_id')
      AND ds.deleted_at IS NULL
  );

-- name: UpdateRackPlacementBulkForSite :exec
-- Bulk variant used by AssignRacksToSite. Stamps every rack in
-- @rack_ids with the target site, clears building_id (because a
-- building belongs to one site, the rack's building membership is
-- invalidated by any site transition), and clears grid placement as a
-- downstream effect of building_id changing. Caller is expected to
-- only pass racks whose current site differs from the target so the
-- response counts stay accurate.
--
-- Zone is cleared (to NULL) only when the rack was actually in a
-- building before — racks with building_id IS NULL keep their zone,
-- matching the old per-rack path. ListRackZoneRefs surfaces
-- building-less zone refs as building_id=0, and silently wiping them
-- would lose user-curated metadata. NULL (not '') preserves the
-- collection_sort.go "zone NULLS LAST" semantics.
UPDATE device_set_rack dsr
SET site_id           = sqlc.narg('target_site_id')::bigint,
    building_id       = NULL,
    zone              = CASE
        WHEN dsr.building_id IS NOT NULL THEN NULL
        ELSE dsr.zone
    END,
    aisle_index       = NULL,
    position_in_aisle = NULL
WHERE dsr.device_set_id = ANY(sqlc.arg('rack_ids')::bigint[])
  AND dsr.org_id = sqlc.arg('org_id')
  AND EXISTS (
    SELECT 1 FROM device_set ds
    WHERE ds.id = dsr.device_set_id
      AND ds.org_id = sqlc.arg('org_id')
      AND ds.deleted_at IS NULL
  );

-- name: CascadeRackDeviceSitesBulk :execrows
-- Bulk variant of CascadeRackDeviceSites. Rewrites device.site_id to
-- target_site_id for every paired member of every rack in @rack_ids.
-- NULL target unassigns. IS DISTINCT FROM skips no-op rows. Returns
-- the total affected row count across all racks.
UPDATE device d
SET site_id = sqlc.narg('target_site_id')::bigint,
    updated_at = CURRENT_TIMESTAMP
FROM device_set_membership dsm
WHERE dsm.device_set_id = ANY(sqlc.arg('rack_ids')::bigint[])
  AND dsm.org_id = sqlc.arg('org_id')
  AND dsm.device_set_type = 'rack'
  AND dsm.device_id = d.id
  AND d.deleted_at IS NULL
  AND d.site_id IS DISTINCT FROM sqlc.narg('target_site_id')::bigint;

-- name: CascadeRackDeviceBuildingsBulk :execrows
-- Building peer of CascadeRackDeviceSitesBulk. Rewrites
-- device.building_id to target_building_id for every paired member of
-- every rack in @rack_ids. NULL target unassigns. IS DISTINCT FROM
-- skips no-op rows.
UPDATE device d
SET building_id = sqlc.narg('target_building_id')::bigint,
    updated_at = CURRENT_TIMESTAMP
FROM device_set_membership dsm
WHERE dsm.device_set_id = ANY(sqlc.arg('rack_ids')::bigint[])
  AND dsm.org_id = sqlc.arg('org_id')
  AND dsm.device_set_type = 'rack'
  AND dsm.device_id = d.id
  AND d.deleted_at IS NULL
  AND d.building_id IS DISTINCT FROM sqlc.narg('target_building_id')::bigint;

-- name: SoftDeleteDeviceSet :execrows
UPDATE device_set
SET deleted_at = CURRENT_TIMESTAMP
WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL;

-- name: ClearRackPlacementForSoftDelete :exec
-- Companion to SoftDeleteDeviceSet for rack-typed collections. Clears
-- the device_set_rack placement so a soft-deleted rack doesn't leave
-- an orphan (building_id, aisle_index, position_in_aisle) tuple that
-- the partial unique index uk_device_set_rack_building_position still
-- treats as occupied. Callers wrap this and SoftDeleteDeviceSet in the
-- same transaction; non-rack collection types simply match 0 rows.
UPDATE device_set_rack
SET aisle_index = NULL,
    position_in_aisle = NULL,
    building_id = NULL,
    zone = ''
WHERE device_set_id = $1 AND org_id = $2;

-- name: DeviceSetBelongsToOrg :one
SELECT EXISTS(
    SELECT 1 FROM device_set
    WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL
) AS belongs;

-- name: GetDeviceSetType :one
SELECT type FROM device_set
WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL;

-- name: AddDevicesToDeviceSet :many
-- RETURNING yields one row per device whose membership was actually inserted
-- (ON CONFLICT DO NOTHING skips already-members), so callers can both count
-- the change (len of result) and resolve the changed set for activity site
-- scope (#538). Equivalent affected-row count to the prior :execrows shape.
INSERT INTO device_set_membership (org_id, device_set_id, device_set_type, device_id, device_identifier)
SELECT $1, $2, ds.type, d.id, d.device_identifier
FROM device d
CROSS JOIN device_set ds
WHERE d.device_identifier = ANY(@device_identifiers::text[])
  AND d.org_id = $1
  AND d.deleted_at IS NULL
  AND ds.id = $2
  AND ds.deleted_at IS NULL
ON CONFLICT (device_set_id, device_id) DO NOTHING
RETURNING device_identifier;

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
-- Rewrites device.site_id to rack.site_id for added rack members whose
-- current site differs. Fires when the rack has a site OR a building:
-- a rack in a building inherits that building's site, which is NULL for
-- an unassigned building — in that case device.site_id is set to NULL
-- so it can't disagree with the building_id stamped by
-- CascadeAddedDeviceBuildings. No-op for groups and for fully-unassigned
-- racks (no site, no building), where setting NULL would clobber direct
-- device.site_id assignments.
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
  AND (dsr.site_id IS NOT NULL OR dsr.building_id IS NOT NULL)
  AND d.site_id IS DISTINCT FROM dsr.site_id;

-- name: CascadeAddedDeviceBuildings :execrows
-- Building peer of CascadeAddedDeviceSites. Rewrites device.building_id
-- to rack.building_id for added rack members whose current building
-- differs. Fires when the rack has a placement (a site OR a building):
-- a site-level rack (site set, building NULL) is a real placement that
-- dictates building = NULL, so members added to it get device.building_id
-- cleared rather than keeping a stale direct assignment. No-op for
-- groups and fully-unassigned racks (no site, no building), where the
-- rack dictates nothing and clearing would clobber direct assignments.
UPDATE device d
SET building_id = dsr.building_id,
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
  AND (dsr.building_id IS NOT NULL OR dsr.site_id IS NOT NULL)
  AND d.building_id IS DISTINCT FROM dsr.building_id;

-- name: RemoveAllDevicesFromDeviceSet :execrows
DELETE FROM device_set_membership
WHERE device_set_id = $1
  AND org_id = $2;

-- name: LockRacksForReparent :many
-- Locks every rack involved in a reparent (sources + target) FOR UPDATE
-- in ascending id order. Used by AssignDevicesToRack as the FIRST tx
-- operation. Sorting source and target together in a single lock
-- acquisition is what prevents the deadlock two concurrent
-- AssignDevicesToRack calls moving devices in opposite directions
-- between the same pair of racks would otherwise hit: each tx locks
-- {sourceA, sourceB, ..., target} in id order, so any two txs touching
-- the same rack pair always agree on the global lock order regardless
-- of which side is source vs target. Pass 0 for @target_rack_id on the
-- unassign path (clear-rack -- no target to include). The membership
-- DELETE in RemoveDevicesFromAnyRack still excludes the target via its
-- own predicate; this query is purely about lock order. Distinct ids
-- are derived in a subquery so the outer locking SELECT can use
-- FOR UPDATE (Postgres rejects DISTINCT + FOR UPDATE at runtime).
--
-- Joining device_set_rack with FOR UPDATE OF dsr, ds extends the lock
-- to the rack's placement row as well. LockRackPlacementForWrite (used
-- by SaveRack, DeleteCollection, etc.) starts FROM device_set_rack dsr
-- JOIN device_set ds and locks both via FOR UPDATE — mirroring that
-- table order here (dsr first in FROM, dsr first in FOR UPDATE OF) so
-- the planner walks both joined rows in the same per-rack order across
-- the two code paths. Without that parity, a concurrent SaveRack and
-- AssignDevicesToRack against the same rack could hold device_set
-- while waiting on device_set_rack (or vice versa) and deadlock.
-- INNER JOIN (not LEFT) is intentional: Postgres rejects FOR UPDATE
-- on the nullable side of an outer join, and every live rack has a
-- device_set_rack row by lifecycle invariant.
SELECT ds.id AS device_set_id
FROM device_set_rack dsr
JOIN device_set ds
  ON ds.id = dsr.device_set_id AND ds.org_id = dsr.org_id
WHERE ds.id IN (
    SELECT dsm.device_set_id
    FROM device_set_membership dsm
    WHERE dsm.org_id = @org_id
      AND dsm.device_set_type = 'rack'
      AND dsm.device_identifier = ANY(@device_identifiers::text[])
  UNION
    SELECT @target_rack_id::bigint
    WHERE @target_rack_id::bigint > 0
  )
  AND ds.org_id = @org_id
  AND ds.type = 'rack'
  AND ds.deleted_at IS NULL
ORDER BY ds.id ASC
FOR UPDATE OF dsr, ds;

-- name: RemoveDevicesFromAnyRack :execrows
-- Removes the given devices from whatever rack they're currently in,
-- EXCEPT the target rack (@target_rack_id). AssignDevicesToRack uses
-- this to clear prior rack membership inside the same transaction as
-- the new-rack insert, closing the orphan window the client-side
-- remove + add orchestration had. Skipping the target rack preserves
-- the existing membership row (and its rack_slot child) when a device
-- is reassigned to the same rack it's already in -- otherwise the
-- DELETE would cascade rack_slot rows that we'd silently lose. Pass
-- 0 for an unconditional clear (caller intends to unassign).
DELETE FROM device_set_membership
WHERE org_id = $1
  AND device_identifier = ANY(@device_identifiers::text[])
  AND device_set_type = 'rack'
  AND device_set_id != @target_rack_id::bigint;

-- name: RemoveDevicesFromDeviceSet :many
-- RETURNING yields one row per membership actually deleted (identifiers that
-- were not members match nothing), so callers can count the change and resolve
-- the changed set for activity site scope (#538). Equivalent affected-row
-- count to the prior :execrows shape.
DELETE FROM device_set_membership
WHERE device_set_id = $1
  AND org_id = $2
  AND device_identifier = ANY(@device_identifiers::text[])
RETURNING device_identifier;

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

-- name: ListDeviceSetMembersPaginatedFiltered :many
SELECT dsm.id, dsm.device_identifier, dsm.created_at,
       rs.row AS slot_row, rs.col AS slot_col
FROM device_set_membership dsm
JOIN device d ON dsm.device_id = d.id AND d.deleted_at IS NULL
LEFT JOIN rack_slot rs ON dsm.device_set_id = rs.device_set_id AND dsm.device_id = rs.device_id
WHERE dsm.device_set_id = @device_set_id::bigint AND dsm.org_id = @org_id::bigint
  AND (
    d.site_id = ANY(@site_ids::bigint[])
    OR (@include_unassigned::boolean AND d.site_id IS NULL)
  )
ORDER BY dsm.created_at DESC, dsm.id DESC
LIMIT @limit_count::int;

-- name: ListDeviceSetMembersPaginatedFilteredAfter :many
SELECT dsm.id, dsm.device_identifier, dsm.created_at,
       rs.row AS slot_row, rs.col AS slot_col
FROM device_set_membership dsm
JOIN device d ON dsm.device_id = d.id AND d.deleted_at IS NULL
LEFT JOIN rack_slot rs ON dsm.device_set_id = rs.device_set_id AND dsm.device_id = rs.device_id
WHERE dsm.device_set_id = @device_set_id::bigint AND dsm.org_id = @org_id::bigint
  AND (
    d.site_id = ANY(@site_ids::bigint[])
    OR (@include_unassigned::boolean AND d.site_id IS NULL)
  )
  AND (dsm.created_at < @cursor_created_at::timestamptz OR (dsm.created_at = @cursor_created_at::timestamptz AND dsm.id < @cursor_id::bigint))
ORDER BY dsm.created_at DESC, dsm.id DESC
LIMIT @limit_count::int;

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

-- name: GetGroupRefsForDevices :many
-- Batch query to get group refs for multiple devices at once (for miner list)
SELECT dsm.device_identifier, ds.id, ds.label
FROM device_set_membership dsm
JOIN device_set ds ON dsm.device_set_id = ds.id AND ds.org_id = dsm.org_id
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
  ds.id AS rack_id,
  ds.label,
  b.id AS building_id,
  COALESCE(b.name, '') AS building_label,
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
JOIN device_set ds ON dsm.device_set_id = ds.id AND ds.org_id = dsm.org_id
LEFT JOIN device_set_rack dsr ON dsm.device_set_id = dsr.device_set_id AND dsr.org_id = dsm.org_id
LEFT JOIN building b ON b.id = dsr.building_id
  AND b.org_id = dsm.org_id
  AND b.deleted_at IS NULL
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

-- name: ListRackZoneRefs :many
-- Returns all distinct (building_id, zone) pairs across the org's racks
-- with denormalized building and site names for dropdown rendering.
-- Racks with building_id IS NULL surface with building_id = 0 and
-- empty building / site labels.
SELECT DISTINCT
    COALESCE(dsr.building_id, 0)::bigint AS building_id,
    COALESCE(b.name, '')                 AS building_label,
    COALESCE(b.site_id, 0)::bigint       AS site_id,
    COALESCE(s.name, '')                 AS site_label,
    dsr.zone
FROM device_set_rack dsr
JOIN device_set ds ON dsr.device_set_id = ds.id
LEFT JOIN building b ON b.id = dsr.building_id AND b.org_id = $1 AND b.deleted_at IS NULL
LEFT JOIN site s     ON s.id = b.site_id      AND s.org_id = $1 AND s.deleted_at IS NULL
WHERE ds.org_id = $1
  AND ds.deleted_at IS NULL
  AND dsr.zone IS NOT NULL
  AND dsr.zone != ''
ORDER BY site_label, building_label, dsr.zone;

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

-- name: FindDevicesWithSiteOrBuilding :many
-- Returns the requested device identifiers that currently have a
-- non-NULL site_id OR building_id. Used by AssignDevicesToRack to detect
-- miners that would lose a placement by joining a site-less
-- (fully-unassigned) rack — the force path clears BOTH columns, so a
-- miner with only a direct building (site NULL, building set, e.g. one
-- assigned to a site-less building) must trip the confirm too.
SELECT device_identifier
FROM device
WHERE org_id = sqlc.arg('org_id')
  AND device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND deleted_at IS NULL
  AND (site_id IS NOT NULL OR building_id IS NOT NULL);

-- name: ClearDeviceSitesAndBuildings :execrows
-- Nulls device.site_id AND device.building_id for the given identifiers.
-- Used by AssignDevicesToRack's force path when adding miners to a
-- site-less rack: the rack dictates "no placement", so member devices
-- can't keep a direct site/building. IS DISTINCT FROM guard skips rows
-- already fully cleared. Returns the count actually stripped.
UPDATE device
SET site_id     = NULL,
    building_id = NULL,
    updated_at  = CURRENT_TIMESTAMP
WHERE org_id = sqlc.arg('org_id')
  AND device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND deleted_at IS NULL
  AND (site_id IS NOT NULL OR building_id IS NOT NULL);
