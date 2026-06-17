-- name: CreateInfraDevice :one
INSERT INTO infra_device (
    org_id, name, device_type, subtype,
    site_id, building_id, ip_address,
    status, control_mode, protocol
) VALUES (
    sqlc.arg('org_id'),
    sqlc.arg('name'),
    sqlc.arg('device_type'),
    sqlc.narg('subtype'),
    sqlc.narg('site_id'),
    sqlc.narg('building_id'),
    sqlc.narg('ip_address'),
    1, -- ONLINE
    sqlc.arg('control_mode'),
    sqlc.narg('protocol')
)
RETURNING *;

-- name: GetInfraDevice :one
SELECT *
FROM infra_device
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL;

-- name: ListInfraDevices :many
SELECT d.*
FROM infra_device d
WHERE d.org_id = sqlc.arg('org_id')
  AND d.deleted_at IS NULL
  AND (sqlc.narg('filter_site_ids')::bigint[] IS NULL
       OR d.site_id = ANY(sqlc.narg('filter_site_ids')::bigint[]))
  AND (sqlc.narg('filter_building_ids')::bigint[] IS NULL
       OR d.building_id = ANY(sqlc.narg('filter_building_ids')::bigint[]))
  AND (sqlc.narg('filter_statuses')::smallint[] IS NULL
       OR d.status = ANY(sqlc.narg('filter_statuses')::smallint[]))
  AND (sqlc.narg('filter_types')::smallint[] IS NULL
       OR d.device_type = ANY(sqlc.narg('filter_types')::smallint[]))
  AND (sqlc.narg('cursor_id')::bigint IS NULL
       OR d.id < sqlc.narg('cursor_id')::bigint)
ORDER BY d.name, d.id
LIMIT sqlc.arg('limit_n')::int;

-- name: CountInfraDevices :one
SELECT COUNT(*)::int AS total_count
FROM infra_device d
WHERE d.org_id = sqlc.arg('org_id')
  AND d.deleted_at IS NULL
  AND (sqlc.narg('filter_site_ids')::bigint[] IS NULL
       OR d.site_id = ANY(sqlc.narg('filter_site_ids')::bigint[]))
  AND (sqlc.narg('filter_building_ids')::bigint[] IS NULL
       OR d.building_id = ANY(sqlc.narg('filter_building_ids')::bigint[]))
  AND (sqlc.narg('filter_statuses')::smallint[] IS NULL
       OR d.status = ANY(sqlc.narg('filter_statuses')::smallint[]))
  AND (sqlc.narg('filter_types')::smallint[] IS NULL
       OR d.device_type = ANY(sqlc.narg('filter_types')::smallint[]));

-- name: UpdateInfraDevice :one
UPDATE infra_device
SET name         = COALESCE(sqlc.narg('name'), name),
    ip_address   = COALESCE(sqlc.narg('ip_address'), ip_address),
    control_mode = COALESCE(sqlc.narg('control_mode'), control_mode),
    updated_at   = CURRENT_TIMESTAMP
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteInfraDevice :execrows
UPDATE infra_device
SET deleted_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL;

-- name: BulkUpdateControlMode :execrows
UPDATE infra_device
SET control_mode = sqlc.arg('control_mode'),
    updated_at = CURRENT_TIMESTAMP
WHERE id = ANY(sqlc.arg('device_ids')::bigint[])
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL;

-- name: BulkSoftDeleteInfraDevices :execrows
UPDATE infra_device
SET deleted_at = CURRENT_TIMESTAMP
WHERE id = ANY(sqlc.arg('device_ids')::bigint[])
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL;

-- name: GetInfraDeviceStats :one
-- Aggregate device counts for dashboard summaries.
SELECT
    COUNT(*)::int AS total_count,
    COUNT(*) FILTER (WHERE status = 1)::int AS online_count,
    COUNT(*) FILTER (WHERE status = 2)::int AS degraded_count,
    COUNT(*) FILTER (WHERE status = 3)::int AS offline_count,
    COUNT(DISTINCT building_id)::int AS buildings_count
FROM infra_device
WHERE org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL
  AND (sqlc.narg('filter_site_id')::bigint IS NULL
       OR site_id = sqlc.narg('filter_site_id')::bigint)
  AND (sqlc.narg('filter_building_id')::bigint IS NULL
       OR building_id = sqlc.narg('filter_building_id')::bigint);

-- name: ListInfraDevicesByBuilding :many
-- Building detail section: devices in a specific building.
SELECT *
FROM infra_device
WHERE org_id = sqlc.arg('org_id')
  AND building_id = sqlc.arg('building_id')
  AND deleted_at IS NULL
ORDER BY name;
