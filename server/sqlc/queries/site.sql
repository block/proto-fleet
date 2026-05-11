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
-- can show "N miners, M buildings, K racks" without an extra round trip.
SELECT
    s.*,
    COALESCE(d.device_count, 0)::bigint AS device_count,
    COALESCE(b.building_count, 0)::bigint AS building_count,
    COALESCE(r.rack_count, 0)::bigint AS rack_count
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
LEFT JOIN (
    SELECT dsr.site_id, COUNT(*) AS rack_count
    FROM device_set_rack dsr
    JOIN device_set ds ON ds.id = dsr.device_set_id
    WHERE dsr.org_id = sqlc.arg('org_id')
      AND dsr.site_id IS NOT NULL
      AND ds.deleted_at IS NULL
    GROUP BY dsr.site_id
) r ON r.site_id = s.id
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
-- Caller is expected to also cascade-unassign attached devices/racks and
-- soft-delete buildings in the same transaction (cascade — see plan J3).
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

-- name: SoftDeleteBuildingsBySite :execrows
-- Soft-deletes every live building under the given site. Caller wraps
-- this in the same tx as the SoftDeleteSite + cascade.
UPDATE building
SET deleted_at = CURRENT_TIMESTAMP
WHERE org_id = sqlc.arg('org_id')
  AND site_id = sqlc.arg('site_id')
  AND deleted_at IS NULL;

-- name: UnassignRacksFromSite :execrows
-- Sets device_set_rack.site_id = NULL for every rack pointing at the
-- given site (org-guarded by the denormalized rack.org_id).
UPDATE device_set_rack
SET site_id = NULL
WHERE org_id = sqlc.arg('org_id')
  AND site_id = sqlc.arg('site_id');

-- name: UnassignRacksFromBuildingsBySite :execrows
-- Clears rack→building linkage (and the zone label) for every rack
-- under any building of the given site. Run BEFORE buildings are
-- soft-deleted so the JOIN against building still resolves.
UPDATE device_set_rack dsr
SET building_id = NULL,
    zone = NULL
WHERE dsr.org_id = sqlc.arg('org_id')
  AND dsr.building_id IN (
      SELECT b.id FROM building b
      WHERE b.org_id = sqlc.arg('org_id')
        AND b.site_id = sqlc.arg('site_id')
  );

-- name: SiteBelongsToOrg :one
SELECT EXISTS(
    SELECT 1 FROM site
    WHERE id = sqlc.arg('id')
      AND org_id = sqlc.arg('org_id')
      AND deleted_at IS NULL
) AS belongs;

-- name: LockSiteForWrite :one
-- Row-locks the site so concurrent DeleteSite can't soft-delete it
-- between the existence check and the cascade write. Returns the
-- site id when alive; sql.ErrNoRows when soft-deleted or missing.
SELECT id FROM site
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL
FOR UPDATE;

-- name: ReassignDevicesToSite :execrows
-- Bulk update of device.site_id for the given identifiers within the
-- org. Caller is expected to have already validated that no device is
-- in a rack at a different site (see FindDeviceSiteConflicts).
-- target_site_id NULL = move to Unassigned.
UPDATE device
SET site_id = sqlc.narg('target_site_id'),
    updated_at = CURRENT_TIMESTAMP
WHERE org_id = sqlc.arg('org_id')
  AND device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND deleted_at IS NULL;

-- name: LockDevicesForReassign :many
-- Takes a row lock on each device row for the duration of the
-- surrounding transaction so the conflict check and the UPDATE are
-- atomic against a concurrent reassign. Empty result means none of the
-- identifiers exist; the caller still wants the lock side-effect.
SELECT id FROM device
WHERE org_id = sqlc.arg('org_id')
  AND device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND deleted_at IS NULL
FOR UPDATE;

-- name: FindDeviceSiteConflicts :many
-- For every requested device, returns the site_id of its rack
-- (NULL when the rack has no site or the device has no rack).
-- Service layer compares against the target site to surface
-- per-device conflicts.
SELECT d.device_identifier, dsr.site_id::bigint AS conflicting_site_id
FROM device d
JOIN device_set_membership dsm
    ON dsm.device_id = d.id
   AND dsm.org_id = d.org_id
   AND dsm.device_set_type = 'rack'
JOIN device_set_rack dsr
    ON dsr.device_set_id = dsm.device_set_id
   AND dsr.org_id = d.org_id
WHERE d.org_id = sqlc.arg('org_id')
  AND d.device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND d.deleted_at IS NULL
  AND dsr.site_id IS NOT NULL;

-- name: ListExistingDeviceIdentifiers :many
-- Filters the requested identifier list down to those that actually
-- exist as live devices in the org. Used to surface "device_not_found"
-- conflicts in ReassignDevicesToSite without an N+1 lookup.
SELECT device_identifier
FROM device
WHERE org_id = sqlc.arg('org_id')
  AND device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND deleted_at IS NULL;

-- name: ListSiteNetworkConfigsForOverlap :many
-- Returns the (id, name, network_config) tuple for every live site in
-- the org excluding the given id (pass 0 for "no exclusion" when
-- creating). Used by the service layer to compute non-blocking
-- cross-site overlap warnings on save.
SELECT id, name, network_config
FROM site
WHERE org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL
  AND id != sqlc.arg('exclude_id');

-- name: ReassignRacksUnderBuilding :execrows
-- Sets rack.site_id = $target for every rack pointing at the given
-- building. Caller wraps this in the same tx as the building UPDATE so
-- the building/rack/device site_ids stay in lockstep.
UPDATE device_set_rack
SET site_id = sqlc.narg('target_site_id')
WHERE org_id = sqlc.arg('org_id')
  AND building_id = sqlc.arg('building_id');

-- name: ReassignDevicesUnderBuilding :execrows
-- Sets device.site_id = $target for every device in any rack of the
-- given building. Caller wraps this in the same tx as the building
-- UPDATE.
UPDATE device d
SET site_id = sqlc.narg('target_site_id'),
    updated_at = CURRENT_TIMESTAMP
FROM device_set_membership dsm
JOIN device_set_rack dsr
    ON dsr.device_set_id = dsm.device_set_id
   AND dsr.org_id = dsm.org_id
WHERE d.id = dsm.device_id
  AND d.org_id = dsm.org_id
  AND dsm.device_set_type = 'rack'
  AND d.org_id = sqlc.arg('org_id')
  AND dsr.building_id = sqlc.arg('building_id')
  AND d.deleted_at IS NULL;
