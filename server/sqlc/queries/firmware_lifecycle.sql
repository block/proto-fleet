-- name: CheckFirmwareArtifactInUse :one
-- Uploaded artifacts are global. Assignments protect continuous enforcement,
-- active rollouts protect in-progress targets, and queued commands protect
-- dispatches that may outlive a rollout or use a legacy file ID.
SELECT (
    EXISTS (SELECT 1 FROM release_channel_firmware assignment
            WHERE assignment.firmware_checksum = sqlc.arg('firmware_checksum')::text
              AND assignment.firmware_checksum <> '')
    OR EXISTS (SELECT 1 FROM firmware_rollout rollout
               WHERE rollout.status = 'active'
                 AND rollout.firmware_checksum = sqlc.arg('firmware_checksum')::text)
    OR EXISTS (SELECT 1 FROM queue_message message
               WHERE message.command_type = 'FirmwareUpdate'
                 AND message.status IN ('PENDING', 'PROCESSING')
                 AND (message.payload->>'firmware_checksum' = sqlc.arg('firmware_checksum')::text
                      OR message.payload->>'firmware_file_id' = sqlc.arg('file_id')::text))
)::boolean AS in_use;

-- name: ListManagedFirmwareUpdateDevices :many
-- A direct firmware command must not compete with release-channel enforcement,
-- including a command that happens to report the assigned version. Firmware
-- can rename a target's reported hardware; active enrollment remains owned by
-- its current assignment until that paired device settles or leaves scope.
SELECT DISTINCT d.device_identifier, channel.name AS channel_name
FROM device d
JOIN discovered_device discovered ON discovered.id = d.discovered_device_id AND discovered.deleted_at IS NULL
JOIN release_channel_member member ON member.device_id = d.id AND member.org_id = d.org_id
JOIN release_channel channel ON channel.id = member.channel_id AND channel.org_id = d.org_id
JOIN release_channel_firmware assignment ON assignment.channel_id = channel.id
    AND assignment.firmware_checksum <> ''
    AND (
        (release_channel_pair_key(assignment.manufacturer) = release_channel_pair_key(discovered.manufacturer)
         AND release_channel_pair_key(assignment.model) = release_channel_pair_key(discovered.model))
        OR EXISTS (
            SELECT 1 FROM firmware_rollout_device target
            JOIN firmware_rollout rollout ON rollout.id = target.rollout_id
            WHERE target.device_id = d.id AND target.excluded_at IS NULL
              AND rollout.channel_id = channel.id AND rollout.status = 'active'
              AND rollout.assignment_generation = assignment.assignment_generation
              AND rollout.firmware_checksum = assignment.firmware_checksum
              AND release_channel_pair_key(rollout.manufacturer) = release_channel_pair_key(assignment.manufacturer)
              AND release_channel_pair_key(rollout.model) = release_channel_pair_key(assignment.model)
        )
    )
WHERE d.org_id = sqlc.arg('org_id') AND d.deleted_at IS NULL
  AND d.device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
ORDER BY d.device_identifier;
