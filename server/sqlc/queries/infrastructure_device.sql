-- name: CreateInfrastructureDevice :one
-- Name is unique per (site_id, name) among live rows; the partial
-- unique index surfaces collisions to the store layer as
-- AlreadyExists.
INSERT INTO infrastructure_device (
    org_id,
    site_id,
    building_name,
    rack_name,
    name,
    device_kind,
    fan_count,
    enabled,
    driver_type,
    driver_config
) VALUES (
    sqlc.arg('org_id'),
    sqlc.arg('site_id'),
    sqlc.arg('building_name'),
    sqlc.arg('rack_name'),
    sqlc.arg('name'),
    sqlc.arg('device_kind'),
    sqlc.arg('fan_count'),
    sqlc.arg('enabled'),
    sqlc.arg('driver_type'),
    sqlc.arg('driver_config')
)
RETURNING *;

-- name: GetInfrastructureDevice :one
SELECT
    d.*,
    COALESCE(s.name, '') AS site_label
FROM infrastructure_device d
LEFT JOIN site s
  ON s.id = d.site_id
 AND s.org_id = d.org_id
 AND s.deleted_at IS NULL
WHERE d.id = sqlc.arg('id')
  AND d.org_id = sqlc.arg('org_id')
  AND d.deleted_at IS NULL;

-- name: LockInfrastructureDeviceForWrite :one
-- Canonical serialization point for device moves/deletes and response-profile
-- references. Callers lock parent sites first, then device rows by ID.
SELECT id
FROM infrastructure_device
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND site_id = sqlc.arg('expected_site_id')
  AND deleted_at IS NULL
FOR UPDATE;

-- name: LockInfrastructureDevicesForResponseProfile :many
-- Locks the exact live fan rows selected by a response profile so concurrent
-- moves/deletes cannot invalidate validation before the profile write commits.
SELECT id, site_id
FROM infrastructure_device
WHERE org_id = sqlc.arg('org_id')
  AND id = ANY(sqlc.arg('infrastructure_device_ids')::bigint[])
  AND deleted_at IS NULL
ORDER BY id
FOR UPDATE;

-- name: ListInfrastructureDevicesByOrg :many
-- Lists every live infrastructure device in the org. site_ids is an
-- optional OR filter (empty array = no filter); excluded_site_ids
-- removes sites from the result regardless of site_ids — the handler
-- uses it to push the caller's narrowed-away sites into the query so
-- unreadable rows are never fetched.
SELECT
    d.*,
    COALESCE(s.name, '') AS site_label
FROM infrastructure_device d
LEFT JOIN site s
  ON s.id = d.site_id
 AND s.org_id = d.org_id
 AND s.deleted_at IS NULL
WHERE d.org_id = sqlc.arg('org_id')
  AND d.deleted_at IS NULL
  AND (
       cardinality(sqlc.arg('site_ids')::bigint[]) = 0
    OR d.site_id = ANY(sqlc.arg('site_ids')::bigint[])
  )
  AND (
       cardinality(sqlc.arg('excluded_site_ids')::bigint[]) = 0
    OR d.site_id != ALL(sqlc.arg('excluded_site_ids')::bigint[])
  )
ORDER BY d.name, d.id;

-- name: LockInfrastructureRackForPlacement :one
-- Validate and lock the live rack catalog entry before persisting its
-- denormalized label on an infrastructure device. Locking both catalog rows
-- serializes this write with rack rename/delete and placement changes; those
-- operations lock rack rows before cascading to infrastructure devices, so
-- callers must invoke this before locking an infrastructure-device row.
SELECT ds.id
FROM device_set_rack dsr
JOIN device_set ds
  ON ds.id = dsr.device_set_id
 AND ds.org_id = dsr.org_id
JOIN building b
  ON b.id = dsr.building_id
 AND b.org_id = ds.org_id
 AND b.deleted_at IS NULL
WHERE ds.org_id = sqlc.arg('org_id')
  AND ds.type = 'rack'
  AND ds.label = sqlc.arg('rack_name')
  AND ds.deleted_at IS NULL
  AND dsr.site_id = sqlc.arg('site_id')
  AND b.site_id = sqlc.arg('site_id')
  AND b.name = sqlc.arg('building_name')
FOR UPDATE OF dsr, ds;

-- name: UpdateInfrastructureDevice :execrows
-- expected_site_id and expected_rack_name predicate the write on the
-- placement the caller was authorized against, so a concurrent placement
-- change between the authorization read and this write invalidates the
-- mutation (0 rows). expected_rack_name NULL is reserved for trusted domain
-- callers that did not perform a handler authorization read. enabled and
-- rack_name are nullable inputs: NULL preserves the row's current value
-- atomically in the UPDATE itself.
UPDATE infrastructure_device
SET site_id       = sqlc.arg('site_id'),
    building_name = sqlc.arg('building_name'),
    rack_name     = COALESCE(sqlc.narg('rack_name')::text, rack_name),
    name          = sqlc.arg('name'),
    device_kind   = sqlc.arg('device_kind'),
    fan_count     = sqlc.arg('fan_count'),
    enabled       = COALESCE(sqlc.narg('enabled')::bool, enabled),
    driver_type   = sqlc.arg('driver_type'),
    driver_config = sqlc.arg('driver_config'),
    updated_at    = CURRENT_TIMESTAMP
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND site_id = sqlc.arg('expected_site_id')
  AND (
    sqlc.narg('expected_rack_name')::text IS NULL
    OR rack_name = sqlc.narg('expected_rack_name')::text
  )
  AND deleted_at IS NULL;

-- name: SoftDeleteInfrastructureDevice :one
-- expected_site_id: same stale-authorization guard as
-- UpdateInfrastructureDevice above. RETURNING the deleted row lets the
-- caller stamp the delete audit event with the device actually deleted,
-- race-free (mirrors SoftDeleteBuilding). sql.ErrNoRows when no live
-- row matched.
UPDATE infrastructure_device
SET deleted_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND site_id = sqlc.arg('expected_site_id')
  AND deleted_at IS NULL
RETURNING *;

-- name: CountResponseProfilesByInfrastructureDevice :one
SELECT COUNT(*)
FROM curtailment_response_profile
WHERE org_id = sqlc.arg('org_id')
  AND facility_fan_device_ids @> sqlc.arg('infrastructure_device_ids')::bigint[];

-- name: CountActiveCurtailmentEventsByInfrastructureDevices :one
SELECT COUNT(*)
FROM curtailment_event
WHERE org_id = sqlc.arg('org_id')
  AND (
    state IN ('pending', 'active', 'restoring')
    OR fan_last_error IS NOT NULL
  )
  AND facility_fan_device_ids && sqlc.arg('infrastructure_device_ids')::BIGINT[];

-- name: CountNonTerminalCurtailmentEventsByInfrastructureDevices :one
SELECT COUNT(*)
FROM curtailment_event
WHERE org_id = sqlc.arg('org_id')
  AND state IN ('pending', 'active', 'restoring')
  AND facility_fan_device_ids && sqlc.arg('infrastructure_device_ids')::BIGINT[];
