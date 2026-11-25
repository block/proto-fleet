-- A release channel's target controller software version for a container
-- model. The target starts no update: it marks which containers are behind and
-- holds the channel's firmware updates to them until they match.

-- name: UpsertReleaseChannelControllerSoftware :one
-- Sets a pair's target. The stored pair keeps the spelling it was first
-- written with.
INSERT INTO release_channel_controller_software (channel_id, manufacturer, model, version, assigned_by)
VALUES (sqlc.arg('channel_id'), sqlc.arg('manufacturer'), sqlc.arg('model'), sqlc.arg('version'), sqlc.arg('assigned_by'))
ON CONFLICT (channel_id, release_channel_pair_key(manufacturer), release_channel_pair_key(model)) DO UPDATE
SET version = EXCLUDED.version,
    assigned_by = EXCLUDED.assigned_by,
    updated_at = now()
RETURNING *;

-- name: DeleteReleaseChannelControllerSoftware :execrows
DELETE FROM release_channel_controller_software
WHERE channel_id = sqlc.arg('channel_id')
  AND release_channel_pair_key(manufacturer) = release_channel_pair_key(sqlc.arg('manufacturer')::text)
  AND release_channel_pair_key(model) = release_channel_pair_key(sqlc.arg('model')::text);

-- name: ControllerSoftwareVersionUploaded :one
-- Whether the organization has an uploaded controller software package of this
-- version, for any architecture.
SELECT EXISTS (
    SELECT 1 FROM frigate_update_package p
    WHERE p.org_id = sqlc.arg('org_id')
      AND p.deleted_at IS NULL
      AND release_channel_controller_software_key(p.version) = release_channel_controller_software_key(sqlc.arg('version')::text)
)::boolean AS uploaded;

-- name: ListReleaseChannelControllerSoftwareSummary :many
-- Count members in SQL. Keep observed spellings separate, and include targets
-- without members under any spelling of their pair.
WITH members AS (
    SELECT COALESCE(dd.manufacturer, '')::text AS manufacturer,
           COALESCE(dd.model, '')::text AS model,
           COALESCE(t.version, '')::text AS target_version,
           count(*)::integer AS container_count,
           count(*) FILTER (WHERE t.version IS NOT NULL
               AND release_channel_controller_software_key(dd.firmware_version) = release_channel_controller_software_key(t.version))::integer AS on_target_count
    FROM release_channel_member m
    JOIN device d ON d.id = m.device_id
    JOIN discovered_device dd ON dd.id = d.discovered_device_id
    LEFT JOIN release_channel_controller_software t
      ON t.channel_id = m.channel_id
     AND release_channel_pair_key(t.manufacturer) = release_channel_pair_key(dd.manufacturer)
     AND release_channel_pair_key(t.model) = release_channel_pair_key(dd.model)
    WHERE m.org_id = sqlc.arg('org_id')
      AND m.channel_id = sqlc.arg('channel_id')
      AND dd.device_class = 2
    GROUP BY COALESCE(dd.manufacturer, ''), COALESCE(dd.model, ''), t.version
)
SELECT manufacturer, model, target_version, container_count, on_target_count FROM members
UNION ALL
SELECT t.manufacturer, t.model, t.version AS target_version, 0::integer AS container_count, 0::integer AS on_target_count
FROM release_channel_controller_software t
WHERE t.channel_id = sqlc.arg('channel_id')
  AND NOT EXISTS (
      SELECT 1 FROM members m
      WHERE release_channel_pair_key(m.manufacturer) = release_channel_pair_key(t.manufacturer)
        AND release_channel_pair_key(m.model) = release_channel_pair_key(t.model)
  )
ORDER BY manufacturer, model;

-- name: ListReleaseChannelContainersPage :many
-- One page for an exact observed pair, including empty observed identities.
-- The cursor is the last (device_identifier, device_id) in the previous page.
SELECT d.id AS device_id,
       d.device_identifier,
       COALESCE(dd.firmware_version, '')::text AS reported_version,
       (t.version IS NOT NULL
        AND release_channel_controller_software_key(dd.firmware_version) = release_channel_controller_software_key(t.version))::boolean AS on_target
FROM release_channel_member m
JOIN device d ON d.id = m.device_id
JOIN discovered_device dd ON dd.id = d.discovered_device_id
LEFT JOIN release_channel_controller_software t
  ON t.channel_id = m.channel_id
 AND release_channel_pair_key(t.manufacturer) = release_channel_pair_key(dd.manufacturer)
 AND release_channel_pair_key(t.model) = release_channel_pair_key(dd.model)
WHERE m.org_id = sqlc.arg('org_id')
  AND m.channel_id = sqlc.arg('channel_id')
  AND dd.device_class = 2
  AND COALESCE(dd.manufacturer, '') = sqlc.arg('manufacturer')::text
  AND COALESCE(dd.model, '') = sqlc.arg('model')::text
  AND (sqlc.narg('after_identifier')::text IS NULL
       OR (d.device_identifier, d.id) > (sqlc.narg('after_identifier')::text, sqlc.narg('after_device_id')::bigint))
ORDER BY d.device_identifier, d.id
LIMIT sqlc.arg('page_limit');

-- name: ListDevicesBehindControllerSoftware :many
-- Of the given devices, the containers whose channel sets a controller software
-- target for their pair that they do not report. The rollout engine holds
-- firmware for these until they match.
SELECT d.id AS device_id,
       t.version AS target_version
FROM device d
JOIN discovered_device dd ON dd.id = d.discovered_device_id
JOIN release_channel_controller_software t
  ON t.channel_id = sqlc.arg('channel_id')
 AND release_channel_pair_key(t.manufacturer) = release_channel_pair_key(dd.manufacturer)
 AND release_channel_pair_key(t.model) = release_channel_pair_key(dd.model)
WHERE d.id = ANY(sqlc.arg('device_ids')::bigint[])
  AND dd.device_class = 2
  AND release_channel_controller_software_key(dd.firmware_version) <> release_channel_controller_software_key(t.version)
ORDER BY d.id;
