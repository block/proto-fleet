-- name: CreateFirmwareReleaseSet :one
INSERT INTO firmware_release_set (org_id)
VALUES ($1)
RETURNING id, org_id, created_at;

-- name: CreateFirmwareReleaseTarget :one
INSERT INTO firmware_release_target (
    release_set_id,
    org_id,
    firmware_file_id,
    target_manufacturer,
    target_model,
    firmware_version,
    sha256
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, release_set_id, org_id, firmware_file_id,
          target_manufacturer, target_model, firmware_version, sha256, created_at;

-- name: GetFirmwareReleaseSet :one
SELECT id, org_id, created_at
FROM firmware_release_set
WHERE id = $1 AND org_id = $2;

-- name: ListFirmwareReleaseTargets :many
SELECT firmware_file_id, target_manufacturer, target_model, firmware_version, sha256
FROM firmware_release_target
WHERE release_set_id = $1 AND org_id = $2
ORDER BY lower(target_manufacturer), lower(target_model), id;

-- name: FirmwareReleaseSetBelongsToOrg :one
SELECT EXISTS (
    SELECT 1
    FROM firmware_release_set
    WHERE id = $1 AND org_id = $2
) AS belongs;

-- name: CreateChannelExtension :execrows
INSERT INTO device_set_channel (device_set_id, org_id, release_set_id)
SELECT ds.id, ds.org_id, rs.id
FROM device_set ds
JOIN firmware_release_set rs
  ON rs.id = sqlc.arg('release_set_id')
 AND rs.org_id = ds.org_id
WHERE ds.id = sqlc.arg('device_set_id')
  AND ds.org_id = sqlc.arg('org_id')
  AND ds.type = 'channel'
  AND ds.deleted_at IS NULL;

-- name: GetChannelInfo :one
SELECT dsc.release_set_id
FROM device_set_channel dsc
JOIN device_set ds
  ON ds.id = dsc.device_set_id
 AND ds.org_id = dsc.org_id
WHERE dsc.device_set_id = $1
  AND dsc.org_id = $2
  AND ds.type = 'channel'
  AND ds.deleted_at IS NULL;

-- name: LockChannelForWrite :one
SELECT dsc.release_set_id
FROM device_set_channel dsc
JOIN device_set ds
  ON ds.id = dsc.device_set_id
 AND ds.org_id = dsc.org_id
WHERE dsc.device_set_id = $1
  AND dsc.org_id = $2
  AND ds.type = 'channel'
  AND ds.deleted_at IS NULL
FOR UPDATE OF dsc, ds;

-- name: GetChannelInfoBatch :many
SELECT dsc.device_set_id, dsc.release_set_id
FROM device_set_channel dsc
JOIN device_set ds
  ON ds.id = dsc.device_set_id
 AND ds.org_id = dsc.org_id
WHERE dsc.device_set_id = ANY(sqlc.arg('device_set_ids')::bigint[])
  AND dsc.org_id = sqlc.arg('org_id')
  AND ds.type = 'channel'
  AND ds.deleted_at IS NULL
ORDER BY dsc.device_set_id;

-- name: ListFirmwareReleaseTargetsBySetIDs :many
SELECT release_set_id, firmware_file_id, target_manufacturer,
       target_model, firmware_version, sha256
FROM firmware_release_target
WHERE release_set_id = ANY(sqlc.arg('release_set_ids')::bigint[])
  AND org_id = sqlc.arg('org_id')
ORDER BY release_set_id, lower(target_manufacturer), lower(target_model), id;

-- name: UpdateChannelReleaseSet :execrows
UPDATE device_set_channel dsc
SET release_set_id = rs.id
FROM firmware_release_set rs, device_set ds
WHERE dsc.device_set_id = sqlc.arg('device_set_id')
  AND dsc.org_id = sqlc.arg('org_id')
  AND ds.id = dsc.device_set_id
  AND ds.org_id = dsc.org_id
  AND ds.type = 'channel'
  AND ds.deleted_at IS NULL
  AND rs.id = sqlc.arg('release_set_id')
  AND rs.org_id = dsc.org_id;

-- name: LockChannelsForReparent :many
SELECT ds.id AS device_set_id
FROM device_set_channel dsc
JOIN device_set ds
  ON ds.id = dsc.device_set_id
 AND ds.org_id = dsc.org_id
WHERE ds.id IN (
    SELECT dsm.device_set_id
    FROM device_set_membership dsm
    WHERE dsm.org_id = sqlc.arg('org_id')
      AND dsm.device_set_type = 'channel'
      AND dsm.device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  UNION
    SELECT sqlc.arg('target_channel_id')::bigint
    WHERE sqlc.arg('target_channel_id')::bigint > 0
  )
  AND ds.org_id = sqlc.arg('org_id')
  AND ds.type = 'channel'
  AND ds.deleted_at IS NULL
ORDER BY ds.id
FOR UPDATE OF dsc, ds;

-- name: LockDevicesForChannelAssignment :many
SELECT device_identifier
FROM device
WHERE org_id = sqlc.arg('org_id')
  AND device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND deleted_at IS NULL
ORDER BY device_identifier
FOR UPDATE;

-- name: RemoveDevicesFromAnyChannel :execrows
DELETE FROM device_set_membership
WHERE org_id = sqlc.arg('org_id')
  AND device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND device_set_type = 'channel'
  AND device_set_id != sqlc.arg('target_channel_id')::bigint;

-- name: FirmwareArtifactReferenced :one
SELECT EXISTS (
    SELECT 1
    FROM firmware_release_target
    WHERE firmware_file_id = $1
) AS referenced;

-- name: AnyFirmwareArtifactReferenced :one
SELECT EXISTS (
    SELECT 1
    FROM firmware_release_target
) AS referenced;
