-- name: CreateFirmwareRolloutEvent :exec
INSERT INTO firmware_rollout_event
    (org_id, rollout_id, channel_id, type, actor_type, actor_id, actor_name,
     rollout_revision, note, device_identifiers)
SELECT r.org_id, r.id, r.channel_id, sqlc.arg('type'), sqlc.arg('actor_type'),
       sqlc.arg('actor_id'), sqlc.arg('actor_name'), r.revision,
       sqlc.arg('note'), sqlc.arg('device_identifiers')::text[]
FROM firmware_rollout r WHERE r.id = sqlc.arg('rollout_id');

-- name: ListFirmwareRolloutEvents :many
SELECT * FROM firmware_rollout_event
WHERE org_id = sqlc.arg('org_id')
  AND (sqlc.arg('rollout_id')::bigint = 0 OR rollout_id = sqlc.arg('rollout_id'))
  AND (sqlc.arg('channel_id')::bigint = 0 OR channel_id = sqlc.arg('channel_id'))
  AND id > sqlc.arg('after_id')
ORDER BY id LIMIT sqlc.arg('page_size');

-- name: SetFirmwareRolloutControllerWait :exec
UPDATE firmware_rollout SET controller_waiting_since = sqlc.narg('waiting_since')::timestamptz
WHERE id = sqlc.arg('rollout_id')
  AND controller_waiting_since IS DISTINCT FROM sqlc.narg('waiting_since')::timestamptz;

-- name: SkipFirmwareRolloutDevices :exec
UPDATE firmware_rollout_device
SET halted_at = clock_timestamp(), halt_reason = 'skipped', skip_note = sqlc.arg('note')
WHERE rollout_id = sqlc.arg('rollout_id') AND device_id = ANY(sqlc.arg('device_ids')::bigint[])
  AND attempts = 0 AND halted_at IS NULL AND verified_at IS NULL AND excluded_at IS NULL;

-- name: ListFirmwareRolloutDispatchCompletions :many
-- When each target's latest dispatched FirmwareUpdate reached a terminal
-- status. Targets whose command is still queued or unrecorded are omitted.
SELECT rd.device_id, max(qm.updated_at)::timestamptz AS finished_at
FROM firmware_rollout_device rd
JOIN queue_message qm ON qm.device_id = rd.device_id
    AND qm.command_batch_log_uuid = rd.last_dispatched_batch_uuid
WHERE rd.rollout_id = sqlc.arg('rollout_id')
  AND qm.command_type = 'FirmwareUpdate'
  AND qm.status IN ('SUCCESS', 'FAILED')
GROUP BY rd.device_id;

-- name: CompleteDelegatedFirmwareRollout :exec
UPDATE firmware_rollout
SET status = sqlc.arg('status'),
    finished_at = GREATEST(created_at, stage_changed_at, paused_at, clock_timestamp()),
    paused_at = NULL, controller_waiting_since = NULL,
    last_action_by_type = sqlc.arg('actor_type'), last_action_by_id = sqlc.arg('actor_id'),
    last_action_by_name = sqlc.arg('actor_name')
WHERE id = sqlc.arg('rollout_id') AND status = 'active';
