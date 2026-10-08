-- name: GetMinerFirmwareHistoryDeviceID :one
SELECT id FROM device
WHERE org_id = sqlc.arg('org_id')
  AND device_identifier = sqlc.arg('device_identifier')
  AND deleted_at IS NULL;

-- name: ListMinerFirmwareHistory :many
-- Read only this device record's persisted targets. History is independent
-- of its current channel membership, reported hardware, and live telemetry.
SELECT r.id AS rollout_id, r.channel_id, c.name AS channel_name,
       r.manufacturer, r.model, r.firmware_version, r.firmware_checksum,
       r.status AS rollout_status, r.cancel_reason, r.paused_at,
       r.created_at, r.finished_at,
       rd.attempts, rd.last_error, rd.skip_note, rd.last_sent_at,
       rd.verified_at, rd.excluded_at, rd.halted_at, rd.halt_reason
FROM firmware_rollout_device rd
JOIN firmware_rollout r ON r.id = rd.rollout_id
JOIN release_channel c ON c.id = r.channel_id AND c.org_id = r.org_id
JOIN device d ON d.id = rd.device_id AND d.org_id = r.org_id
WHERE rd.device_id = sqlc.arg('device_id')
  AND r.org_id = sqlc.arg('org_id')
  AND d.deleted_at IS NULL
  AND (
    sqlc.narg('before_created_at')::timestamptz IS NULL
    OR (r.created_at, r.id) < (sqlc.narg('before_created_at')::timestamptz, sqlc.narg('before_id')::bigint)
  )
ORDER BY r.created_at DESC, r.id DESC
LIMIT sqlc.arg('page_limit');
