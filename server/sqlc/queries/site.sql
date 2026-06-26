-- name: CreateSite :one
-- Org-scoped insert. The unique partial index on (org_id, name) where
-- deleted_at IS NULL surfaces name collisions as a unique-violation; the
-- service layer maps that to AlreadyExists.
INSERT INTO site (
    org_id,
    name,
    slug,
    location_city,
    location_state,
    timezone,
    power_capacity_mw,
    network_config,
    address,
    postal_code,
    country,
    notes
) VALUES (
    sqlc.arg('org_id'),
    sqlc.arg('name'),
    sqlc.arg('slug'),
    sqlc.narg('location_city'),
    sqlc.narg('location_state'),
    sqlc.narg('timezone'),
    sqlc.narg('power_capacity_mw'),
    sqlc.narg('network_config'),
    sqlc.narg('address'),
    sqlc.narg('postal_code'),
    COALESCE(sqlc.narg('country')::text, 'US'),
    sqlc.narg('notes')
)
RETURNING *;

-- name: ListSiteSlugs :many
SELECT slug
FROM site
WHERE org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL
ORDER BY slug;

-- name: GetSite :one
SELECT *
FROM site
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL;

-- name: GetSiteBySlug :one
SELECT *
FROM site
WHERE slug = sqlc.arg('slug')
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL;

-- name: SitesByIDs :many
-- Returns the subset of requested IDs that correspond to live sites
-- in the org. Caller diffs against the requested set to detect
-- cross-org or missing IDs. Mirrors BuildingsByIDs; used to
-- bulk-validate rack-list site_ids filter references in one round trip.
SELECT id
FROM site
WHERE org_id = $1
  AND deleted_at IS NULL
  AND id = ANY(@ids::bigint[]);

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

-- name: CountRacksBySite :one
SELECT COUNT(*)::bigint
FROM device_set_rack dsr
JOIN device_set ds ON ds.id = dsr.device_set_id
WHERE dsr.org_id = sqlc.arg('org_id')
  AND dsr.site_id = sqlc.arg('site_id')
  AND ds.deleted_at IS NULL;

-- name: CountBuildingsBySite :one
SELECT COUNT(*)::bigint
FROM building
WHERE org_id = sqlc.arg('org_id')
  AND site_id = sqlc.arg('site_id')
  AND deleted_at IS NULL;

-- name: UpdateSite :exec
-- The slug is not user-editable but tracks the name: the service regenerates
-- it on a rename and re-sends the unchanged slug otherwise. A slug
-- unique-violation (uk_site_org_slug) maps to a collision sentinel so the
-- service can retry with the next suffix, mirroring CreateSite.
UPDATE site
SET name              = sqlc.arg('name'),
    slug              = sqlc.arg('slug'),
    location_city     = sqlc.narg('location_city'),
    location_state    = sqlc.narg('location_state'),
    timezone          = sqlc.narg('timezone'),
    power_capacity_mw = sqlc.narg('power_capacity_mw'),
    network_config    = sqlc.narg('network_config'),
    address           = sqlc.narg('address'),
    postal_code       = sqlc.narg('postal_code'),
    country           = COALESCE(sqlc.narg('country')::text, country),
    notes             = sqlc.narg('notes'),
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

-- name: DeleteCurtailmentResponseProfilesBySite :one
-- Deletes reusable response profiles tied to a site as part of the
-- site delete cascade so they cannot outlive a soft-deleted site.
WITH scoped_profiles AS (
  SELECT profile.id
  FROM curtailment_response_profile profile
  WHERE profile.org_id = sqlc.arg('org_id')
    AND (
      profile.site_id = sqlc.arg('site_id')
      OR (
        profile.scope_json ? 'site_id'
        AND (profile.scope_json->>'site_id')::BIGINT = sqlc.arg('site_id')
      )
      OR EXISTS (
        SELECT 1
        FROM jsonb_array_elements_text(
          CASE
            WHEN jsonb_typeof(profile.scope_json->'site_ids') = 'array' THEN profile.scope_json->'site_ids'
            ELSE '[]'::jsonb
          END
        ) AS scope_site(site_id)
        WHERE scope_site.site_id::BIGINT = sqlc.arg('site_id')
      )
    )
),
blocking_rules AS (
  SELECT rule.id
  FROM curtailment_automation_rule rule
  JOIN scoped_profiles profile
    ON profile.id = rule.response_profile_id
  WHERE rule.org_id = sqlc.arg('org_id')
),
deleted_profiles AS (
  DELETE FROM curtailment_response_profile profile
  WHERE profile.org_id = sqlc.arg('org_id')
    AND profile.id IN (SELECT id FROM scoped_profiles)
    AND NOT EXISTS (SELECT 1 FROM blocking_rules)
  RETURNING 1
)
SELECT
  (SELECT COUNT(*) FROM deleted_profiles)::BIGINT AS deleted_count,
  (SELECT COUNT(*) FROM blocking_rules)::BIGINT AS blocking_rule_count;

-- name: SoftDeleteBuildingsBySite :execrows
-- Soft-deletes every live building under the given site. Caller wraps
-- this in the same tx as the SoftDeleteSite + cascade.
UPDATE building
SET deleted_at = CURRENT_TIMESTAMP
WHERE org_id = sqlc.arg('org_id')
  AND site_id = sqlc.arg('site_id')
  AND deleted_at IS NULL;

-- name: UnassignRacksFromSite :execrows
-- Sets device_set_rack.site_id = NULL for every live rack pointing at
-- the given site (org-guarded by the denormalized rack.org_id; the
-- EXISTS subquery skips racks whose parent device_set is soft-deleted
-- so the count returned to the UI matches ListSites.rack_count).
UPDATE device_set_rack dsr
SET site_id = NULL
WHERE dsr.org_id = sqlc.arg('org_id')
  AND dsr.site_id = sqlc.arg('site_id')
  AND EXISTS (
      SELECT 1 FROM device_set ds
      WHERE ds.id = dsr.device_set_id
        AND ds.deleted_at IS NULL
  );

-- name: UnassignRacksFromBuildingsBySite :execrows
-- Clears rack→building linkage (and the zone + grid placement) for
-- every live rack under any building of the given site. Run BEFORE
-- buildings are soft-deleted so the JOIN against building still
-- resolves. The EXISTS subquery on device_set skips soft-deleted
-- rack collections, matching ListBuildings.rack_count's filter.
-- aisle_index/position_in_aisle MUST be cleared in the same update —
-- the ck_device_set_rack_position_requires_building CHECK rejects
-- rows where building_id IS NULL but a position is set, so a
-- separate two-statement cascade would violate the constraint.
UPDATE device_set_rack dsr
SET building_id = NULL,
    zone = NULL,
    aisle_index = NULL,
    position_in_aisle = NULL
WHERE dsr.org_id = sqlc.arg('org_id')
  AND dsr.building_id IN (
      SELECT b.id FROM building b
      WHERE b.org_id = sqlc.arg('org_id')
        AND b.site_id = sqlc.arg('site_id')
  )
  AND EXISTS (
      SELECT 1 FROM device_set ds
      WHERE ds.id = dsr.device_set_id
        AND ds.deleted_at IS NULL
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

-- name: LockBuildingForWrite :one
-- Row-locks a specific building so concurrent mutations (DeleteSite,
-- AssignBuildingsToSite, DeleteBuilding) serialize. Returns the building
-- id when alive; sql.ErrNoRows when soft-deleted or missing.
SELECT id FROM building
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL
FOR UPDATE;

-- name: LockBuildingsBySiteForWrite :many
-- Row-locks every live building under the given site so DeleteSite's
-- cascade can rewrite their racks without a concurrent
-- AssignBuildingsToSite slipping a building out from under it. Returns
-- the locked ids (result is informational; the FOR UPDATE side-effect
-- is what matters).
SELECT id FROM building
WHERE org_id = sqlc.arg('org_id')
  AND site_id = sqlc.arg('site_id')
  AND deleted_at IS NULL
ORDER BY id ASC
FOR UPDATE;

-- name: AssignDevicesToSite :execrows
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
-- For every requested device, returns the site_id of its live rack
-- (NULL when the rack has no site or the device has no rack). The
-- JOIN on device_set with deleted_at IS NULL skips memberships that
-- point at soft-deleted rack collections so a stale rack can't trigger
-- a false conflict rejection (rack/building list queries already
-- filter the same way).
-- Service layer compares against the target site to surface
-- per-device conflicts.
SELECT d.device_identifier, dsr.site_id::bigint AS conflicting_site_id
FROM device d
JOIN device_set_membership dsm
    ON dsm.device_id = d.id
   AND dsm.org_id = d.org_id
   AND dsm.device_set_type = 'rack'
JOIN device_set ds
    ON ds.id = dsm.device_set_id
   AND ds.deleted_at IS NULL
JOIN device_set_rack dsr
    ON dsr.device_set_id = dsm.device_set_id
   AND dsr.org_id = d.org_id
WHERE d.org_id = sqlc.arg('org_id')
  AND d.device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND d.deleted_at IS NULL
  AND dsr.site_id IS NOT NULL;

-- name: FindDevicesInSiteLessRacks :many
-- Returns device identifiers sitting in a live rack that has NO site (a
-- fully-unassigned rack — building implies a site, so a NULL site means
-- no building either). The site peer of FindDeviceSiteConflicts, which
-- only returns racks WITH a site. Used by AssignDevicesToSite (and the
-- building flow, which cascades site): a device can't take a direct site
-- while remaining in a site-less rack without breaking device/rack site
-- lockstep, so the service flags these as a clearable conflict and the
-- force-clear path drops the rack membership before the move.
SELECT d.device_identifier
FROM device d
JOIN device_set_membership dsm
    ON dsm.device_id = d.id
   AND dsm.org_id = d.org_id
   AND dsm.device_set_type = 'rack'
JOIN device_set ds
    ON ds.id = dsm.device_set_id
   AND ds.deleted_at IS NULL
JOIN device_set_rack dsr
    ON dsr.device_set_id = dsm.device_set_id
   AND dsr.org_id = d.org_id
WHERE d.org_id = sqlc.arg('org_id')
  AND d.device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND d.deleted_at IS NULL
  AND dsr.site_id IS NULL;

-- name: ListExistingDeviceIdentifiers :many
-- Filters the requested identifier list down to those that actually
-- exist as live devices in the org. Used to surface "device_not_found"
-- conflicts in AssignDevicesToSite without an N+1 lookup.
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
-- Sets rack.site_id = $target for every live rack pointing at the
-- given building. Caller wraps this in the same tx as the building
-- UPDATE so the building/rack/device site_ids stay in lockstep. The
-- EXISTS subquery on device_set skips soft-deleted rack collections,
-- matching the list/cascade filters elsewhere.
UPDATE device_set_rack dsr
SET site_id = sqlc.narg('target_site_id')
WHERE dsr.org_id = sqlc.arg('org_id')
  AND dsr.building_id = sqlc.arg('building_id')
  AND EXISTS (
      SELECT 1 FROM device_set ds
      WHERE ds.id = dsr.device_set_id
        AND ds.deleted_at IS NULL
  );

-- name: ReassignRacksUnderBuildingsBulk :execrows
-- Bulk variant of ReassignRacksUnderBuilding. Sets rack.site_id =
-- $target for every live rack pointing at any of @building_ids in one
-- statement. Caller wraps in the same tx as the building UPDATE so
-- the building/rack/device site_ids stay in lockstep.
UPDATE device_set_rack dsr
SET site_id = sqlc.narg('target_site_id')
WHERE dsr.org_id = sqlc.arg('org_id')
  AND dsr.building_id = ANY(sqlc.arg('building_ids')::bigint[])
  AND EXISTS (
      SELECT 1 FROM device_set ds
      WHERE ds.id = dsr.device_set_id
        AND ds.deleted_at IS NULL
  );

-- name: ReassignDevicesUnderBuildingsBulk :execrows
-- Bulk variant of ReassignDevicesUnderBuilding. Sets device.site_id =
-- $target for every device in any live rack of any building in
-- @building_ids. Caller wraps in the same tx as the building UPDATE.
UPDATE device d
SET site_id = sqlc.narg('target_site_id'),
    updated_at = CURRENT_TIMESTAMP
FROM device_set_membership dsm
JOIN device_set ds
    ON ds.id = dsm.device_set_id
   AND ds.deleted_at IS NULL
JOIN device_set_rack dsr
    ON dsr.device_set_id = dsm.device_set_id
   AND dsr.org_id = dsm.org_id
WHERE d.id = dsm.device_id
  AND d.org_id = dsm.org_id
  AND dsm.device_set_type = 'rack'
  AND d.org_id = sqlc.arg('org_id')
  AND dsr.building_id = ANY(sqlc.arg('building_ids')::bigint[])
  AND d.deleted_at IS NULL;

-- name: ReassignDevicesUnderBuilding :execrows
-- Sets device.site_id = $target for every device in any live rack of
-- the given building. Caller wraps this in the same tx as the building
-- UPDATE. The JOIN on device_set with deleted_at IS NULL skips
-- soft-deleted rack collections so a stale membership can't rewrite
-- live devices through a rack users no longer see.
UPDATE device d
SET site_id = sqlc.narg('target_site_id'),
    updated_at = CURRENT_TIMESTAMP
FROM device_set_membership dsm
JOIN device_set ds
    ON ds.id = dsm.device_set_id
   AND ds.deleted_at IS NULL
JOIN device_set_rack dsr
    ON dsr.device_set_id = dsm.device_set_id
   AND dsr.org_id = dsm.org_id
WHERE d.id = dsm.device_id
  AND d.org_id = dsm.org_id
  AND dsm.device_set_type = 'rack'
  AND d.org_id = sqlc.arg('org_id')
  AND dsr.building_id = sqlc.arg('building_id')
  AND d.deleted_at IS NULL;
