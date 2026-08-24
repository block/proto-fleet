-- name: CreateRolloutLane :one
INSERT INTO rollout_lane (
    id,
    org_id,
    label,
    description,
    current_channel_id,
    idempotency_key,
    create_fingerprint,
    created_by_user_id
)
VALUES (
    sqlc.arg('lane_id'),
    sqlc.arg('org_id'),
    sqlc.arg('label'),
    sqlc.arg('description'),
    sqlc.arg('current_channel_id'),
    sqlc.arg('idempotency_key'),
    sqlc.arg('create_fingerprint'),
    sqlc.arg('created_by_user_id')
)
RETURNING *;

-- name: GetRolloutLaneByIdempotencyKey :one
SELECT *
FROM rollout_lane
WHERE org_id = sqlc.arg('org_id')
  AND idempotency_key = sqlc.arg('idempotency_key');

-- name: GetRolloutLane :one
SELECT *
FROM rollout_lane
WHERE id = sqlc.arg('lane_id')
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL;

-- name: LockRolloutLane :one
SELECT *
FROM rollout_lane
WHERE id = sqlc.arg('lane_id')
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL
FOR UPDATE;

-- name: LockRolloutLanes :many
SELECT *
FROM rollout_lane
WHERE org_id = sqlc.arg('org_id')
  AND id = ANY(sqlc.arg('lane_ids')::uuid[])
  AND deleted_at IS NULL
ORDER BY id
FOR UPDATE;

-- name: ListRolloutLanes :many
SELECT *
FROM rollout_lane
WHERE org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL
ORDER BY label, id;

-- name: ListActiveFirmwareConvergenceRolloutLanes :many
WITH active_lane_ids AS (
    SELECT DISTINCT lane.id AS lane_id
    FROM rollout_lane lane
    JOIN rollout_lane_channel attachment
      ON attachment.lane_id = lane.id
     AND attachment.org_id = lane.org_id
    JOIN device_set_membership membership
      ON membership.device_set_id = attachment.channel_id
     AND membership.org_id = attachment.org_id
     AND membership.device_set_type = 'channel'
    JOIN channel_firmware_enforcement enforcement
      ON enforcement.device_id = membership.device_id
     AND enforcement.org_id = membership.org_id
    JOIN channel_firmware_authority authority
      ON authority.id = enforcement.authority_id
     AND authority.org_id = enforcement.org_id
    LEFT JOIN rollout_lane_membership_change membership_change
      ON membership_change.authority_id = authority.id
     AND membership_change.org_id = authority.org_id
    WHERE lane.org_id = sqlc.arg('org_id')
      AND (
          authority.authority_type = 'rollout_lane_initial'
              AND authority.authority_reference = lane.id::text
          OR authority.authority_type = 'rollout_lane_membership'
              AND membership_change.target_lane_id = lane.id
      )
      AND enforcement.state IN (
          'pending',
          'held',
          'dispatching',
          'dispatched',
          'verifying'
      )
)
SELECT lane.*
FROM rollout_lane lane
JOIN active_lane_ids active
  ON active.lane_id = lane.id
WHERE lane.org_id = sqlc.arg('org_id')
  AND lane.deleted_at IS NULL
ORDER BY lane.label, lane.id;

-- name: LockRolloutLaneForArchive :one
SELECT *
FROM rollout_lane
WHERE id = sqlc.arg('lane_id')
  AND org_id = sqlc.arg('org_id')
FOR UPDATE;

-- name: ListRolloutLaneMemberDeviceIDs :many
SELECT DISTINCT device.id
FROM rollout_lane_channel attachment
JOIN device_set_membership membership
  ON membership.device_set_id = attachment.channel_id
 AND membership.org_id = attachment.org_id
 AND membership.device_set_type = 'channel'
JOIN device
  ON device.id = membership.device_id
 AND device.org_id = membership.org_id
WHERE attachment.lane_id = sqlc.arg('lane_id')
  AND attachment.org_id = sqlc.arg('org_id')
ORDER BY device.id;

-- name: CountRolloutLaneMembers :one
SELECT COUNT(DISTINCT membership.device_id)::bigint
FROM rollout_lane_channel attachment
JOIN device_set_membership membership
  ON membership.device_set_id = attachment.channel_id
 AND membership.org_id = attachment.org_id
 AND membership.device_set_type = 'channel'
JOIN device
  ON device.id = membership.device_id
 AND device.org_id = membership.org_id
 AND device.deleted_at IS NULL
WHERE attachment.lane_id = sqlc.arg('lane_id')
  AND attachment.org_id = sqlc.arg('org_id')
  AND (
      sqlc.narg('lane_model_id')::uuid IS NULL
      OR EXISTS (
          SELECT 1
          FROM rollout_lane_model_binding binding
          WHERE binding.lane_id = attachment.lane_id
            AND binding.lane_model_id = sqlc.narg('lane_model_id')
            AND binding.org_id = attachment.org_id
            AND binding.device_id = membership.device_id
            AND binding.channel_id = attachment.channel_id
            AND binding.ended_at IS NULL
      )
  );

-- name: CountRolloutLaneMembersByLaneIDs :many
SELECT attachment.lane_id,
       COUNT(DISTINCT device.id)::bigint AS member_count
FROM rollout_lane_channel attachment
LEFT JOIN device_set_membership membership
  ON membership.device_set_id = attachment.channel_id
 AND membership.org_id = attachment.org_id
 AND membership.device_set_type = 'channel'
LEFT JOIN device
  ON device.id = membership.device_id
 AND device.org_id = membership.org_id
 AND device.deleted_at IS NULL
WHERE attachment.org_id = sqlc.arg('org_id')
  AND attachment.lane_id = ANY(sqlc.arg('lane_ids')::uuid[])
GROUP BY attachment.lane_id
ORDER BY attachment.lane_id;

-- name: ListRolloutLaneMembers :many
SELECT device.id AS device_id,
       device.device_identifier,
       COALESCE(discovered.manufacturer, '') AS manufacturer,
       COALESCE(discovered.model, '') AS model,
       btrim(COALESCE(discovered.firmware_version, '')) AS observed_firmware_version,
       attachment.channel_id,
       attachment.position AS channel_position,
       attachment.channel_id = COALESCE(selected_model.current_channel_id, lane.current_channel_id) AS on_current_channel,
       COALESCE(target.firmware_version, '') AS pinned_release_version,
       COALESCE(latest_enforcement.last_observed_firmware_version, '') AS enforcement_observed_firmware_version,
       COALESCE(latest_enforcement.desired_firmware_version, '') AS enforcement_target_firmware_version,
       COALESCE(latest_enforcement.state, '') AS enforcement_state,
       COALESCE(latest_enforcement.last_error, '') AS enforcement_last_error,
       COALESCE(latest_enforcement.updated_at, 'epoch'::timestamptz) AS enforcement_updated_at
FROM rollout_lane lane
JOIN rollout_lane_channel attachment
  ON attachment.lane_id = lane.id
 AND attachment.org_id = lane.org_id
JOIN device_set_membership membership
  ON membership.device_set_id = attachment.channel_id
 AND membership.org_id = attachment.org_id
 AND membership.device_set_type = 'channel'
JOIN device
  ON device.id = membership.device_id
 AND device.org_id = membership.org_id
 AND device.deleted_at IS NULL
LEFT JOIN discovered_device discovered
  ON discovered.id = device.discovered_device_id
 AND discovered.org_id = device.org_id
 AND discovered.deleted_at IS NULL
LEFT JOIN rollout_lane_model selected_model
  ON selected_model.id = sqlc.narg('lane_model_id')
 AND selected_model.lane_id = lane.id
 AND selected_model.org_id = lane.org_id
JOIN device_set_channel physical_channel
  ON physical_channel.device_set_id = attachment.channel_id
 AND physical_channel.org_id = attachment.org_id
LEFT JOIN firmware_release_target target
  ON target.release_set_id = physical_channel.release_set_id
 AND target.org_id = physical_channel.org_id
 AND lower(btrim(target.target_manufacturer)) =
     lower(btrim(COALESCE(discovered.manufacturer, '')))
 AND lower(btrim(target.target_model)) =
     lower(btrim(COALESCE(discovered.model, '')))
LEFT JOIN LATERAL (
    SELECT enforcement.last_observed_firmware_version,
           enforcement.desired_firmware_version,
           enforcement.state,
           enforcement.last_error,
           enforcement.updated_at
    FROM channel_firmware_enforcement enforcement
    JOIN channel_firmware_authority authority
      ON authority.id = enforcement.authority_id
     AND authority.org_id = enforcement.org_id
    LEFT JOIN rollout_lane_membership_change membership_change
      ON membership_change.authority_id = authority.id
     AND membership_change.org_id = authority.org_id
    WHERE enforcement.org_id = lane.org_id
      AND enforcement.device_id = device.id
      AND (
          authority.authority_type = 'rollout_lane_initial'
              AND authority.authority_reference = lane.id::text
          OR authority.authority_type = 'rollout_lane_membership'
              AND membership_change.target_lane_id = lane.id
      )
    ORDER BY enforcement.desired_at DESC, enforcement.id DESC
    LIMIT 1
) latest_enforcement ON true
WHERE lane.id = sqlc.arg('lane_id')
  AND lane.org_id = sqlc.arg('org_id')
  AND lane.deleted_at IS NULL
  AND device.device_identifier > sqlc.arg('after_identifier')
  AND (
      sqlc.narg('lane_model_id')::uuid IS NULL
      OR EXISTS (
          SELECT 1
          FROM rollout_lane_model_binding binding
          WHERE binding.lane_id = lane.id
            AND binding.lane_model_id = sqlc.narg('lane_model_id')
            AND binding.org_id = lane.org_id
            AND binding.device_id = device.id
            AND binding.channel_id = attachment.channel_id
            AND binding.ended_at IS NULL
      )
  )
ORDER BY device.device_identifier
LIMIT sqlc.arg('member_limit');

-- name: ListRolloutLaneMembersByIdentifiers :many
SELECT device.id AS device_id,
       device.device_identifier,
       COALESCE(discovered.manufacturer, '') AS manufacturer,
       COALESCE(discovered.model, '') AS model,
       btrim(COALESCE(discovered.firmware_version, '')) AS observed_firmware_version,
       attachment.channel_id,
       attachment.position AS channel_position,
       attachment.channel_id = lane.current_channel_id AS on_current_channel,
       COALESCE(target.firmware_version, '') AS pinned_release_version,
       COALESCE(latest_enforcement.last_observed_firmware_version, '') AS enforcement_observed_firmware_version,
       COALESCE(latest_enforcement.desired_firmware_version, '') AS enforcement_target_firmware_version,
       COALESCE(latest_enforcement.state, '') AS enforcement_state,
       COALESCE(latest_enforcement.last_error, '') AS enforcement_last_error,
       COALESCE(latest_enforcement.updated_at, 'epoch'::timestamptz) AS enforcement_updated_at
FROM rollout_lane lane
JOIN rollout_lane_channel attachment
  ON attachment.lane_id = lane.id
 AND attachment.org_id = lane.org_id
JOIN device_set_membership membership
  ON membership.device_set_id = attachment.channel_id
 AND membership.org_id = attachment.org_id
 AND membership.device_set_type = 'channel'
JOIN device
  ON device.id = membership.device_id
 AND device.org_id = membership.org_id
 AND device.deleted_at IS NULL
LEFT JOIN discovered_device discovered
  ON discovered.id = device.discovered_device_id
 AND discovered.org_id = device.org_id
 AND discovered.deleted_at IS NULL
JOIN device_set_channel physical_channel
  ON physical_channel.device_set_id = attachment.channel_id
 AND physical_channel.org_id = attachment.org_id
LEFT JOIN firmware_release_target target
  ON target.release_set_id = physical_channel.release_set_id
 AND target.org_id = physical_channel.org_id
 AND lower(btrim(target.target_manufacturer)) =
     lower(btrim(COALESCE(discovered.manufacturer, '')))
 AND lower(btrim(target.target_model)) =
     lower(btrim(COALESCE(discovered.model, '')))
LEFT JOIN LATERAL (
    SELECT enforcement.last_observed_firmware_version,
           enforcement.desired_firmware_version,
           enforcement.state,
           enforcement.last_error,
           enforcement.updated_at
    FROM channel_firmware_enforcement enforcement
    JOIN channel_firmware_authority authority
      ON authority.id = enforcement.authority_id
     AND authority.org_id = enforcement.org_id
    LEFT JOIN rollout_lane_membership_change membership_change
      ON membership_change.authority_id = authority.id
     AND membership_change.org_id = authority.org_id
    WHERE enforcement.org_id = lane.org_id
      AND enforcement.device_id = device.id
      AND (
          authority.authority_type = 'rollout_lane_initial'
              AND authority.authority_reference = lane.id::text
          OR authority.authority_type = 'rollout_lane_membership'
              AND membership_change.target_lane_id = lane.id
      )
    ORDER BY enforcement.desired_at DESC, enforcement.id DESC
    LIMIT 1
) latest_enforcement ON true
WHERE lane.id = sqlc.arg('lane_id')
  AND lane.org_id = sqlc.arg('org_id')
  AND lane.deleted_at IS NULL
  AND device.device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
ORDER BY device.device_identifier;

-- name: GetRolloutLaneAssignments :many
SELECT membership.device_identifier,
       lane.id AS lane_id,
       lane.label AS lane_label
FROM device_set_membership membership
JOIN rollout_lane_channel attachment
  ON attachment.channel_id = membership.device_set_id
 AND attachment.org_id = membership.org_id
JOIN rollout_lane lane
  ON lane.id = attachment.lane_id
 AND lane.org_id = attachment.org_id
 AND lane.deleted_at IS NULL
WHERE membership.org_id = sqlc.arg('org_id')
  AND membership.device_set_type = 'channel'
  AND membership.device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
ORDER BY membership.device_identifier;

-- name: ListRolloutLaneMembershipCandidates :many
SELECT device.id AS device_id,
       device.device_identifier,
       COALESCE(discovered.manufacturer, '') AS manufacturer,
       COALESCE(discovered.model, '') AS model,
       COALESCE(discovered.firmware_version, '') AS observed_firmware_version,
       membership.device_set_id AS channel_id,
       source_lane.id AS source_lane_id,
       source_lane.label AS source_lane_label,
       source_lane.revision AS source_lane_revision,
       source_attachment.position AS source_channel_position,
       COALESCE(source_target.firmware_version, '') AS source_release_version
FROM device
JOIN discovered_device discovered
  ON discovered.id = device.discovered_device_id
 AND discovered.org_id = device.org_id
 AND discovered.deleted_at IS NULL
LEFT JOIN device_set_membership membership
  ON membership.device_id = device.id
 AND membership.org_id = device.org_id
 AND membership.device_set_type = 'channel'
LEFT JOIN rollout_lane_channel source_attachment
  ON source_attachment.channel_id = membership.device_set_id
 AND source_attachment.org_id = membership.org_id
LEFT JOIN rollout_lane source_lane
  ON source_lane.id = source_attachment.lane_id
 AND source_lane.org_id = source_attachment.org_id
 AND source_lane.deleted_at IS NULL
LEFT JOIN device_set_channel source_channel
  ON source_channel.device_set_id = membership.device_set_id
 AND source_channel.org_id = membership.org_id
LEFT JOIN firmware_release_target source_target
  ON source_target.release_set_id = source_channel.release_set_id
 AND source_target.org_id = source_channel.org_id
 AND lower(btrim(source_target.target_manufacturer)) =
     lower(btrim(COALESCE(discovered.manufacturer, '')))
 AND lower(btrim(source_target.target_model)) =
     lower(btrim(COALESCE(discovered.model, '')))
WHERE device.org_id = sqlc.arg('org_id')
  AND device.device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND device.deleted_at IS NULL
ORDER BY device.device_identifier;

-- name: ListRolloutLaneCurrentReleaseTargets :many
SELECT target.id,
       target.release_set_id,
       target.firmware_file_id,
       target.target_manufacturer,
       target.target_model,
       target.firmware_version,
       target.sha256
FROM rollout_lane lane
JOIN device_set_channel channel
  ON channel.device_set_id = lane.current_channel_id
 AND channel.org_id = lane.org_id
JOIN firmware_release_target target
  ON target.release_set_id = channel.release_set_id
 AND target.org_id = channel.org_id
WHERE lane.id = sqlc.arg('lane_id')
  AND lane.org_id = sqlc.arg('org_id')
  AND lane.deleted_at IS NULL
ORDER BY lower(target.target_manufacturer), lower(target.target_model), target.id;

-- name: LockRolloutLaneInitialAuthorities :many
SELECT authority.id,
       authority.revision,
       authority.halted_at
FROM channel_firmware_authority authority
LEFT JOIN rollout_lane_membership_change membership_change
  ON membership_change.authority_id = authority.id
 AND membership_change.org_id = authority.org_id
WHERE authority.org_id = sqlc.arg('org_id')
  AND (
      authority.authority_type = 'rollout_lane_initial'
          AND authority.authority_reference = sqlc.arg('lane_id')::uuid::text
      OR authority.authority_type = 'rollout_lane_membership'
          AND membership_change.target_lane_id = sqlc.arg('lane_id')
  )
ORDER BY authority.id
FOR UPDATE OF authority;

-- name: LockRolloutLaneManagementAuthorities :many
SELECT authority.id,
       authority.revision,
       authority.halted_at
FROM channel_firmware_authority authority
LEFT JOIN rollout_lane_membership_change membership_change
  ON membership_change.authority_id = authority.id
 AND membership_change.org_id = authority.org_id
WHERE authority.org_id = sqlc.arg('org_id')
  AND (
      authority.authority_type = 'rollout_lane_initial'
          AND authority.authority_reference::uuid = ANY(sqlc.arg('lane_ids')::uuid[])
      OR authority.authority_type = 'rollout_lane_membership'
          AND membership_change.target_lane_id = ANY(sqlc.arg('lane_ids')::uuid[])
  )
ORDER BY authority.id
FOR UPDATE OF authority;

-- name: LockRolloutLaneOwnedRolloutMembers :many
SELECT member.id
FROM rollout_lane_channel attachment
JOIN firmware_rollout rollout
  ON rollout.id = attachment.rollout_id
 AND rollout.org_id = attachment.org_id
JOIN firmware_rollout_member member
  ON member.rollout_id = rollout.id
 AND member.org_id = rollout.org_id
WHERE attachment.org_id = sqlc.arg('org_id')
  AND attachment.lane_id = ANY(sqlc.arg('lane_ids')::uuid[])
  AND member.owner_released_at IS NULL
ORDER BY member.id
FOR UPDATE OF rollout, member;

-- name: HasActiveRolloutLaneManagementWork :one
SELECT EXISTS (
    SELECT 1
    FROM channel_firmware_authority authority
    JOIN channel_firmware_enforcement enforcement
      ON enforcement.authority_id = authority.id
     AND enforcement.org_id = authority.org_id
    LEFT JOIN rollout_lane_membership_change membership_change
      ON membership_change.authority_id = authority.id
     AND membership_change.org_id = authority.org_id
    WHERE authority.org_id = sqlc.arg('org_id')
      AND (
          authority.authority_type = 'rollout_lane_initial'
              AND authority.authority_reference::uuid = ANY(sqlc.arg('lane_ids')::uuid[])
          OR authority.authority_type = 'rollout_lane_membership'
              AND membership_change.target_lane_id = ANY(sqlc.arg('lane_ids')::uuid[])
      )
      AND enforcement.state IN (
          'pending',
          'held',
          'dispatching',
          'dispatched',
          'verifying'
      )
)
OR EXISTS (
    SELECT 1
    FROM rollout_lane_channel attachment
    JOIN firmware_rollout rollout
      ON rollout.id = attachment.rollout_id
     AND rollout.org_id = attachment.org_id
    WHERE attachment.org_id = sqlc.arg('org_id')
      AND attachment.lane_id = ANY(sqlc.arg('lane_ids')::uuid[])
      AND (
          rollout.state NOT IN (
              'completed',
              'completed_with_failures',
              'aborted',
              'reverted'
          )
          OR EXISTS (
              SELECT 1
              FROM firmware_rollout_member member
              WHERE member.rollout_id = rollout.id
                AND member.org_id = rollout.org_id
                AND member.owner_released_at IS NULL
          )
          OR EXISTS (
              SELECT 1
              FROM firmware_rollout_control control
              WHERE control.rollout_id = rollout.id
                AND control.org_id = rollout.org_id
                AND control.status = 'started'
          )
          OR EXISTS (
              SELECT 1
              FROM channel_firmware_enforcement enforcement
              WHERE enforcement.org_id = rollout.org_id
                AND enforcement.authority_id IN (
                    rollout.forward_authority_id,
                    rollout.revert_authority_id
                )
                AND enforcement.state IN (
                    'pending',
                    'held',
                    'dispatching',
                    'dispatched',
                    'verifying'
                )
          )
      )
);

-- name: HasActiveRolloutLaneInitialWork :one
SELECT EXISTS (
    SELECT 1
    FROM channel_firmware_authority authority
    JOIN channel_firmware_enforcement enforcement
      ON enforcement.authority_id = authority.id
     AND enforcement.org_id = authority.org_id
    LEFT JOIN rollout_lane_membership_change membership_change
      ON membership_change.authority_id = authority.id
     AND membership_change.org_id = authority.org_id
    WHERE authority.org_id = sqlc.arg('org_id')
      AND (
          authority.authority_type = 'rollout_lane_initial'
              AND authority.authority_reference = sqlc.arg('lane_id')::uuid::text
          OR authority.authority_type = 'rollout_lane_membership'
              AND membership_change.target_lane_id = sqlc.arg('lane_id')
      )
      AND enforcement.state IN (
          'pending',
          'held',
          'dispatching',
          'dispatched',
          'verifying'
      )
);

-- name: HasActiveRolloutLaneLinkedWork :one
SELECT EXISTS (
    SELECT 1
    FROM rollout_lane_channel attachment
    JOIN firmware_rollout rollout
      ON rollout.id = attachment.rollout_id
     AND rollout.org_id = attachment.org_id
    WHERE attachment.lane_id = sqlc.arg('lane_id')
      AND attachment.org_id = sqlc.arg('org_id')
      AND (
          rollout.state NOT IN (
              'completed',
              'completed_with_failures',
              'aborted',
              'reverted'
          )
          OR EXISTS (
              SELECT 1
              FROM firmware_rollout_member member
              WHERE member.rollout_id = rollout.id
                AND member.org_id = rollout.org_id
                AND member.owner_released_at IS NULL
          )
          OR EXISTS (
              SELECT 1
              FROM firmware_rollout_control control
              WHERE control.rollout_id = rollout.id
                AND control.org_id = rollout.org_id
                AND control.status = 'started'
          )
          OR EXISTS (
              SELECT 1
              FROM channel_firmware_authority authority
              WHERE authority.org_id = rollout.org_id
                AND authority.id IN (
                    rollout.forward_authority_id,
                    rollout.revert_authority_id
                )
                AND authority.halted_at IS NULL
          )
          OR EXISTS (
              SELECT 1
              FROM channel_firmware_enforcement enforcement
              WHERE enforcement.org_id = rollout.org_id
                AND enforcement.authority_id IN (
                    rollout.forward_authority_id,
                    rollout.revert_authority_id
                )
                AND enforcement.state IN (
                    'pending',
                    'held',
                    'dispatching',
                    'dispatched',
                    'verifying'
                )
          )
      )
);

-- name: RemoveRolloutLaneMemberships :many
DELETE FROM device_set_membership membership
USING rollout_lane_channel attachment
WHERE attachment.lane_id = sqlc.arg('lane_id')
  AND attachment.org_id = sqlc.arg('org_id')
  AND membership.device_set_id = attachment.channel_id
  AND membership.org_id = attachment.org_id
  AND membership.device_set_type = 'channel'
RETURNING membership.device_id;

-- name: EndRolloutLaneModelBindingsForArchive :execrows
UPDATE rollout_lane_model_binding
SET ended_at = CURRENT_TIMESTAMP,
    revision = revision + 1
WHERE lane_id = sqlc.arg('lane_id')
  AND org_id = sqlc.arg('org_id')
  AND ended_at IS NULL;

-- name: ArchiveRolloutLane :one
UPDATE rollout_lane
SET deleted_at = CURRENT_TIMESTAMP,
    deleted_by_user_id = sqlc.arg('deleted_by_user_id'),
    deleted_actor_type = sqlc.arg('deleted_actor_type'),
    deleted_actor_credential_id = sqlc.narg('deleted_actor_credential_id'),
    delete_reason = sqlc.arg('delete_reason'),
    delete_idempotency_key = sqlc.arg('delete_idempotency_key'),
    delete_fingerprint = sqlc.arg('delete_fingerprint'),
    revision = revision + 1
WHERE id = sqlc.arg('lane_id')
  AND org_id = sqlc.arg('org_id')
  AND revision = sqlc.arg('expected_revision')
  AND deleted_at IS NULL
RETURNING *;

-- name: CreateRolloutLaneChannel :one
INSERT INTO rollout_lane_channel (
    lane_id,
    org_id,
    channel_id,
    position,
    rollout_id,
    start_idempotency_key,
    start_fingerprint
)
VALUES (
    sqlc.arg('lane_id'),
    sqlc.arg('org_id'),
    sqlc.arg('channel_id'),
    sqlc.arg('position'),
    sqlc.narg('rollout_id'),
    sqlc.narg('start_idempotency_key'),
    sqlc.narg('start_fingerprint')
)
RETURNING *;

-- name: GetRolloutLaneChannelByStartKey :one
SELECT *
FROM rollout_lane_channel
WHERE lane_id = sqlc.arg('lane_id')
  AND org_id = sqlc.arg('org_id')
  AND start_idempotency_key = sqlc.arg('start_idempotency_key');

-- name: ListRolloutLaneChannels :many
SELECT attachment.lane_id,
       attachment.org_id,
       attachment.channel_id,
       channel.release_set_id,
       attachment.position,
       attachment.rollout_id,
       attachment.created_at
FROM rollout_lane_channel attachment
JOIN device_set_channel channel
  ON channel.device_set_id = attachment.channel_id
 AND channel.org_id = attachment.org_id
WHERE attachment.lane_id = sqlc.arg('lane_id')
  AND attachment.org_id = sqlc.arg('org_id')
ORDER BY attachment.position, attachment.channel_id;

-- name: ListRolloutLaneChannelsByLaneIDs :many
SELECT attachment.lane_id,
       attachment.channel_id
FROM rollout_lane_channel attachment
WHERE attachment.lane_id = ANY(sqlc.arg('lane_ids')::uuid[])
  AND attachment.org_id = sqlc.arg('org_id')
ORDER BY attachment.lane_id, attachment.position, attachment.channel_id;

-- name: ListRolloutLaneChannelDetailsByLaneIDs :many
SELECT attachment.lane_id,
       attachment.org_id,
       attachment.channel_id,
       channel.release_set_id,
       attachment.position,
       attachment.rollout_id,
       attachment.created_at
FROM rollout_lane_channel attachment
JOIN device_set_channel channel
  ON channel.device_set_id = attachment.channel_id
 AND channel.org_id = attachment.org_id
WHERE attachment.lane_id = ANY(sqlc.arg('lane_ids')::uuid[])
  AND attachment.org_id = sqlc.arg('org_id')
ORDER BY attachment.lane_id, attachment.position, attachment.channel_id;

-- name: ListRolloutLaneModels :many
SELECT model.id,
       model.lane_id,
       model.org_id,
       model.model_identity_key,
       model.normalization_version,
       model.manufacturer,
       model.model,
       model.current_channel_id,
       model.current_release_set_id,
       model.current_release_target_id,
       model.revision,
       model.created_at,
       model.updated_at,
       target.firmware_file_id,
       target.firmware_version,
       target.sha256,
       COUNT(binding.id) FILTER (WHERE binding.ended_at IS NULL)::bigint AS active_binding_count,
       COUNT(binding.id) FILTER (WHERE binding.ended_at IS NOT NULL)::bigint AS historical_binding_count
FROM rollout_lane_model model
JOIN firmware_release_target target
  ON target.id = model.current_release_target_id
 AND target.release_set_id = model.current_release_set_id
 AND target.org_id = model.org_id
LEFT JOIN rollout_lane_model_binding binding
  ON binding.lane_model_id = model.id
 AND binding.lane_id = model.lane_id
 AND binding.org_id = model.org_id
WHERE model.lane_id = sqlc.arg('lane_id')
  AND model.org_id = sqlc.arg('org_id')
GROUP BY model.id,
         target.id,
         target.release_set_id,
         target.org_id
ORDER BY lower(model.manufacturer), lower(model.model), model.id;

-- name: ListRolloutLaneModelsByLaneIDs :many
SELECT model.id,
       model.lane_id,
       model.org_id,
       model.model_identity_key,
       model.normalization_version,
       model.manufacturer,
       model.model,
       model.current_channel_id,
       model.current_release_set_id,
       model.current_release_target_id,
       model.revision,
       model.created_at,
       model.updated_at,
       target.firmware_file_id,
       target.firmware_version,
       target.sha256,
       COUNT(binding.id) FILTER (WHERE binding.ended_at IS NULL)::bigint AS active_binding_count,
       COUNT(binding.id) FILTER (WHERE binding.ended_at IS NOT NULL)::bigint AS historical_binding_count
FROM rollout_lane_model model
JOIN firmware_release_target target
  ON target.id = model.current_release_target_id
 AND target.release_set_id = model.current_release_set_id
 AND target.org_id = model.org_id
LEFT JOIN rollout_lane_model_binding binding
  ON binding.lane_model_id = model.id
 AND binding.lane_id = model.lane_id
 AND binding.org_id = model.org_id
WHERE model.lane_id = ANY(sqlc.arg('lane_ids')::uuid[])
  AND model.org_id = sqlc.arg('org_id')
GROUP BY model.id,
         target.id,
         target.release_set_id,
         target.org_id
ORDER BY model.lane_id, lower(model.manufacturer), lower(model.model), model.id;

-- name: GetRolloutLaneModelCurrentTarget :one
SELECT target.firmware_file_id,
       model.manufacturer,
       model.model,
       target.firmware_version,
       target.sha256
FROM rollout_lane_model model
JOIN firmware_release_target target
  ON target.id = model.current_release_target_id
 AND target.release_set_id = model.current_release_set_id
 AND target.org_id = model.org_id
WHERE model.id = sqlc.arg('lane_model_id')
  AND model.lane_id = sqlc.arg('lane_id')
  AND model.org_id = sqlc.arg('org_id');

-- name: ListRolloutLaneModelChannels :many
SELECT history.lane_model_id,
       history.channel_id,
       history.position,
       history.created_at,
       history.release_set_id,
       history.release_target_id,
       target.firmware_file_id,
       target.firmware_version,
       target.sha256
FROM rollout_lane_model_channel history
JOIN firmware_release_target target
  ON target.id = history.release_target_id
 AND target.release_set_id = history.release_set_id
 AND target.org_id = history.org_id
WHERE history.lane_id = sqlc.arg('lane_id')
  AND history.org_id = sqlc.arg('org_id')
ORDER BY history.lane_model_id, history.position, history.channel_id;

-- name: ListRolloutLaneModelChannelsByLaneIDs :many
SELECT history.lane_id,
       history.lane_model_id,
       history.channel_id,
       history.position,
       history.created_at,
       history.release_set_id,
       history.release_target_id,
       target.firmware_file_id,
       target.firmware_version,
       target.sha256
FROM rollout_lane_model_channel history
JOIN firmware_release_target target
  ON target.id = history.release_target_id
 AND target.release_set_id = history.release_set_id
 AND target.org_id = history.org_id
WHERE history.lane_id = ANY(sqlc.arg('lane_ids')::uuid[])
  AND history.org_id = sqlc.arg('org_id')
ORDER BY history.lane_id, history.lane_model_id, history.position, history.channel_id;

-- name: ListRolloutLaneModelFirmwareConvergenceStatuses :many
WITH active_bindings AS (
    SELECT binding.lane_model_id,
           binding.device_id
    FROM rollout_lane_model_binding binding
    WHERE binding.lane_id = sqlc.arg('lane_id')
      AND binding.org_id = sqlc.arg('org_id')
      AND binding.ended_at IS NULL
),
latest AS (
    SELECT active.lane_model_id,
           active.device_id,
           latest_enforcement.state
    FROM active_bindings active
    LEFT JOIN LATERAL (
        SELECT enforcement.state
        FROM channel_firmware_enforcement enforcement
        JOIN channel_firmware_authority authority
          ON authority.id = enforcement.authority_id
         AND authority.org_id = enforcement.org_id
        LEFT JOIN rollout_lane_membership_change membership_change
          ON membership_change.authority_id = authority.id
         AND membership_change.org_id = authority.org_id
        WHERE enforcement.org_id = sqlc.arg('org_id')
          AND enforcement.device_id = active.device_id
          AND (
              authority.authority_type = 'rollout_lane_initial'
                  AND authority.authority_reference = sqlc.arg('lane_id')::uuid::text
              OR authority.authority_type = 'rollout_lane_membership'
                  AND membership_change.target_lane_id = sqlc.arg('lane_id')
          )
        ORDER BY enforcement.desired_at DESC, enforcement.id DESC
        LIMIT 1
    ) latest_enforcement ON true
)
SELECT model.id AS lane_model_id,
       COUNT(latest.device_id)::bigint AS total_count,
       COUNT(latest.device_id) FILTER (
           WHERE latest.state IS NULL OR latest.state IN ('pending', 'held')
       )::bigint AS pending_count,
       COUNT(latest.device_id) FILTER (
           WHERE latest.state IN ('dispatching', 'dispatched')
       )::bigint AS updating_count,
       COUNT(latest.device_id) FILTER (
           WHERE latest.state = 'verifying'
       )::bigint AS verifying_count,
       COUNT(latest.device_id) FILTER (
           WHERE latest.state = 'confirmed'
       )::bigint AS confirmed_count,
       COUNT(latest.device_id) FILTER (
           WHERE latest.state IN ('attention_required', 'cancelled')
       )::bigint AS attention_count
FROM rollout_lane_model model
LEFT JOIN latest
  ON latest.lane_model_id = model.id
WHERE model.lane_id = sqlc.arg('lane_id')
  AND model.org_id = sqlc.arg('org_id')
GROUP BY model.id
ORDER BY model.id;

-- name: ListRolloutLaneModelFirmwareConvergenceStatusesByLaneIDs :many
WITH active_bindings AS (
    SELECT binding.lane_id,
           binding.lane_model_id,
           binding.device_id
    FROM rollout_lane_model_binding binding
    WHERE binding.lane_id = ANY(sqlc.arg('lane_ids')::uuid[])
      AND binding.org_id = sqlc.arg('org_id')
      AND binding.ended_at IS NULL
),
latest AS (
    SELECT active.lane_id,
           active.lane_model_id,
           active.device_id,
           latest_enforcement.state
    FROM active_bindings active
    LEFT JOIN LATERAL (
        SELECT enforcement.state
        FROM channel_firmware_enforcement enforcement
        JOIN channel_firmware_authority authority
          ON authority.id = enforcement.authority_id
         AND authority.org_id = enforcement.org_id
        LEFT JOIN rollout_lane_membership_change membership_change
          ON membership_change.authority_id = authority.id
         AND membership_change.org_id = authority.org_id
        WHERE enforcement.org_id = sqlc.arg('org_id')
          AND enforcement.device_id = active.device_id
          AND (
              authority.authority_type = 'rollout_lane_initial'
                  AND authority.authority_reference = active.lane_id::text
              OR authority.authority_type = 'rollout_lane_membership'
                  AND membership_change.target_lane_id = active.lane_id
          )
        ORDER BY enforcement.desired_at DESC, enforcement.id DESC
        LIMIT 1
    ) latest_enforcement ON true
)
SELECT model.lane_id,
       model.id AS lane_model_id,
       COUNT(latest.device_id)::bigint AS total_count,
       COUNT(latest.device_id) FILTER (
           WHERE latest.state IS NULL OR latest.state IN ('pending', 'held')
       )::bigint AS pending_count,
       COUNT(latest.device_id) FILTER (
           WHERE latest.state IN ('dispatching', 'dispatched')
       )::bigint AS updating_count,
       COUNT(latest.device_id) FILTER (
           WHERE latest.state = 'verifying'
       )::bigint AS verifying_count,
       COUNT(latest.device_id) FILTER (
           WHERE latest.state = 'confirmed'
       )::bigint AS confirmed_count,
       COUNT(latest.device_id) FILTER (
           WHERE latest.state IN ('attention_required', 'cancelled')
       )::bigint AS attention_count
FROM rollout_lane_model model
LEFT JOIN latest
  ON latest.lane_id = model.lane_id
 AND latest.lane_model_id = model.id
WHERE model.lane_id = ANY(sqlc.arg('lane_ids')::uuid[])
  AND model.org_id = sqlc.arg('org_id')
GROUP BY model.lane_id, model.id
ORDER BY model.lane_id, model.id;

-- name: CountRolloutLaneNonCurrentMembers :one
SELECT COUNT(*)::bigint
FROM rollout_lane lane
JOIN rollout_lane_channel attachment
  ON attachment.lane_id = lane.id
 AND attachment.org_id = lane.org_id
JOIN device_set_membership membership
  ON membership.device_set_id = attachment.channel_id
 AND membership.org_id = attachment.org_id
 AND membership.device_set_type = 'channel'
WHERE lane.id = sqlc.arg('lane_id')
  AND lane.org_id = sqlc.arg('org_id')
  AND attachment.channel_id <> lane.current_channel_id;

-- name: GetNextRolloutLaneChannelPosition :one
SELECT COALESCE(MAX(position) + 1, 0)::int
FROM rollout_lane_channel
WHERE lane_id = sqlc.arg('lane_id')
  AND org_id = sqlc.arg('org_id');

-- name: GetRolloutLaneForRollout :one
SELECT lane.*
FROM rollout_lane lane
WHERE lane.org_id = sqlc.arg('org_id')
  AND (
      EXISTS (
          SELECT 1
          FROM rollout_lane_channel attachment
          WHERE attachment.lane_id = lane.id
            AND attachment.org_id = lane.org_id
            AND attachment.rollout_id = sqlc.arg('rollout_id')
      )
      OR EXISTS (
          SELECT 1
          FROM firmware_rollout child
          WHERE child.id = sqlc.arg('rollout_id')
            AND child.org_id = lane.org_id
            AND child.lane_id = lane.id
      )
      OR EXISTS (
          SELECT 1
          FROM firmware_rollout_group parent
          WHERE parent.id = sqlc.arg('rollout_id')
            AND parent.org_id = lane.org_id
            AND parent.lane_id = lane.id
      )
  );

-- name: LockBetweenChannelChannels :many
SELECT channel.device_set_id
FROM device_set_channel channel
JOIN device_set parent
  ON parent.id = channel.device_set_id
 AND parent.org_id = channel.org_id
WHERE channel.org_id = sqlc.arg('org_id')
  AND channel.device_set_id = ANY(sqlc.arg('channel_ids')::bigint[])
  AND parent.type = 'channel'
  AND parent.deleted_at IS NULL
ORDER BY channel.device_set_id
FOR UPDATE OF channel, parent;

-- name: LockBetweenChannelDevices :many
SELECT device.id
FROM device
JOIN discovered_device discovered
  ON discovered.id = device.discovered_device_id
 AND discovered.org_id = device.org_id
 AND discovered.deleted_at IS NULL
WHERE device.org_id = sqlc.arg('org_id')
  AND device.id = ANY(sqlc.arg('device_ids')::bigint[])
  AND device.deleted_at IS NULL
ORDER BY device.device_identifier
FOR UPDATE OF device, discovered;

-- name: LockRolloutLaneDevicesForArchive :many
-- Archive must lock every persisted membership, including devices that were
-- soft-deleted without removing their historical channel membership.
SELECT id
FROM device
WHERE org_id = sqlc.arg('org_id')
  AND id = ANY(sqlc.arg('device_ids')::bigint[])
ORDER BY device_identifier
FOR UPDATE;

-- name: ListBetweenChannelDeviceModels :many
SELECT device.id AS device_id,
       device.device_identifier,
       COALESCE(discovered.manufacturer, '') AS manufacturer,
       COALESCE(discovered.model, '') AS model,
       COALESCE(discovered.firmware_version, '') AS current_firmware_version
FROM device
JOIN discovered_device discovered
  ON discovered.id = device.discovered_device_id
 AND discovered.org_id = device.org_id
 AND discovered.deleted_at IS NULL
WHERE device.org_id = sqlc.arg('org_id')
  AND device.device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND device.deleted_at IS NULL
ORDER BY device.device_identifier;

-- name: LockBetweenChannelInitialDevices :many
SELECT device.id AS device_id,
       device.device_identifier,
       COALESCE(discovered.manufacturer, '') AS manufacturer,
       COALESCE(discovered.model, '') AS model,
       COALESCE(discovered.firmware_version, '') AS current_firmware_version
FROM device
JOIN discovered_device discovered
  ON discovered.id = device.discovered_device_id
 AND discovered.org_id = device.org_id
 AND discovered.deleted_at IS NULL
WHERE device.org_id = sqlc.arg('org_id')
  AND device.device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND device.deleted_at IS NULL
ORDER BY device.device_identifier
FOR UPDATE OF device, discovered;

-- name: CreateInitialRolloutLaneEnforcements :execrows
INSERT INTO channel_firmware_enforcement (
    org_id,
    device_id,
    desired_release_set_id,
    desired_release_target_id,
    desired_firmware_file_id,
    desired_firmware_version,
    cause_type,
    cause_reference,
    authority_id,
    authority_revision,
    state,
    last_observed_firmware_version,
    firmware_observed_at,
    confirmed_at
)
SELECT device.org_id,
       device.id,
       target.release_set_id,
       target.id,
       target.firmware_file_id,
       target.firmware_version,
       'rollout_lane_initial',
       sqlc.arg('lane_id')::uuid::text,
       authority.id,
       authority.revision,
       CASE
           WHEN btrim(COALESCE(discovered.firmware_version, '')) = target.firmware_version
               THEN 'confirmed'
           ELSE 'pending'
       END,
       NULLIF(btrim(COALESCE(discovered.firmware_version, '')), ''),
       CASE
           WHEN btrim(COALESCE(discovered.firmware_version, '')) <> ''
               THEN discovered.last_seen
           ELSE NULL
       END,
       CASE
           WHEN btrim(COALESCE(discovered.firmware_version, '')) = target.firmware_version
               THEN CURRENT_TIMESTAMP
           ELSE NULL
       END
FROM device
JOIN discovered_device discovered
  ON discovered.id = device.discovered_device_id
 AND discovered.org_id = device.org_id
 AND discovered.deleted_at IS NULL
JOIN firmware_release_target target
  ON target.release_set_id = sqlc.arg('release_set_id')
 AND target.org_id = device.org_id
 AND lower(btrim(target.target_manufacturer)) =
     lower(btrim(COALESCE(discovered.manufacturer, '')))
 AND lower(btrim(target.target_model)) =
     lower(btrim(COALESCE(discovered.model, '')))
JOIN channel_firmware_authority authority
  ON authority.id = sqlc.arg('authority_id')
 AND authority.org_id = device.org_id
 AND authority.revision = sqlc.arg('authority_revision')
 AND authority.halted_at IS NULL
WHERE device.org_id = sqlc.arg('org_id')
  AND device.device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND device.deleted_at IS NULL
ON CONFLICT (authority_id, device_id) DO NOTHING;

-- name: GetRolloutLaneFirmwareConvergenceStatus :one
WITH latest AS (
    SELECT DISTINCT ON (membership.device_id)
           enforcement.id,
           enforcement.state
    FROM rollout_lane_channel attachment
    JOIN device_set_membership membership
      ON membership.device_set_id = attachment.channel_id
     AND membership.org_id = attachment.org_id
     AND membership.device_set_type = 'channel'
    JOIN channel_firmware_enforcement enforcement
      ON enforcement.device_id = membership.device_id
     AND enforcement.org_id = membership.org_id
    JOIN channel_firmware_authority authority
      ON authority.id = enforcement.authority_id
     AND authority.org_id = enforcement.org_id
    LEFT JOIN rollout_lane_membership_change membership_change
      ON membership_change.authority_id = authority.id
     AND membership_change.org_id = authority.org_id
    WHERE attachment.lane_id = sqlc.arg('lane_id')
      AND attachment.org_id = sqlc.arg('org_id')
      AND (
          authority.authority_type = 'rollout_lane_initial'
              AND authority.authority_reference = attachment.lane_id::text
          OR authority.authority_type = 'rollout_lane_membership'
              AND membership_change.target_lane_id = attachment.lane_id
      )
    ORDER BY membership.device_id, enforcement.desired_at DESC, enforcement.id DESC
)
SELECT COUNT(latest.id)::bigint AS total_count,
       COUNT(latest.id) FILTER (
           WHERE latest.state IN ('pending', 'held')
       )::bigint AS pending_count,
       COUNT(latest.id) FILTER (
           WHERE latest.state IN ('dispatching', 'dispatched')
       )::bigint AS updating_count,
       COUNT(latest.id) FILTER (
           WHERE latest.state = 'verifying'
       )::bigint AS verifying_count,
       COUNT(latest.id) FILTER (
           WHERE latest.state = 'confirmed'
       )::bigint AS confirmed_count,
       COUNT(latest.id) FILTER (
           WHERE latest.state IN ('attention_required', 'cancelled')
       )::bigint AS attention_count
FROM latest;

-- name: ListRolloutLaneFirmwareConvergenceStatuses :many
WITH ranked AS (
    SELECT attachment.lane_id,
           enforcement.id,
           enforcement.state,
           ROW_NUMBER() OVER (
               PARTITION BY attachment.lane_id, membership.device_id
               ORDER BY enforcement.desired_at DESC, enforcement.id DESC
           ) AS rank
    FROM rollout_lane_channel attachment
    JOIN device_set_membership membership
      ON membership.device_set_id = attachment.channel_id
     AND membership.org_id = attachment.org_id
     AND membership.device_set_type = 'channel'
    JOIN channel_firmware_enforcement enforcement
      ON enforcement.device_id = membership.device_id
     AND enforcement.org_id = membership.org_id
    JOIN channel_firmware_authority authority
      ON authority.id = enforcement.authority_id
     AND authority.org_id = enforcement.org_id
    LEFT JOIN rollout_lane_membership_change membership_change
      ON membership_change.authority_id = authority.id
     AND membership_change.org_id = authority.org_id
    WHERE attachment.org_id = sqlc.arg('org_id')
      AND (
          authority.authority_type = 'rollout_lane_initial'
              AND authority.authority_reference = attachment.lane_id::text
          OR authority.authority_type = 'rollout_lane_membership'
              AND membership_change.target_lane_id = attachment.lane_id
      )
)
SELECT lane.id AS lane_id,
       COUNT(ranked.id)::bigint AS total_count,
       COUNT(ranked.id) FILTER (
           WHERE ranked.state IN ('pending', 'held')
       )::bigint AS pending_count,
       COUNT(ranked.id) FILTER (
           WHERE ranked.state IN ('dispatching', 'dispatched')
       )::bigint AS updating_count,
       COUNT(ranked.id) FILTER (
           WHERE ranked.state = 'verifying'
       )::bigint AS verifying_count,
       COUNT(ranked.id) FILTER (
           WHERE ranked.state = 'confirmed'
       )::bigint AS confirmed_count,
       COUNT(ranked.id) FILTER (
           WHERE ranked.state IN ('attention_required', 'cancelled')
       )::bigint AS attention_count
FROM rollout_lane lane
LEFT JOIN ranked
  ON ranked.lane_id = lane.id
 AND ranked.rank = 1
WHERE lane.org_id = sqlc.arg('org_id')
  AND lane.deleted_at IS NULL
GROUP BY lane.id;

-- name: ListActiveRolloutLaneFirmwareConvergenceStatuses :many
WITH ranked AS (
    SELECT attachment.lane_id,
           enforcement.id,
           enforcement.state,
           ROW_NUMBER() OVER (
               PARTITION BY attachment.lane_id, membership.device_id
               ORDER BY enforcement.desired_at DESC, enforcement.id DESC
           ) AS rank
    FROM rollout_lane_channel attachment
    JOIN device_set_membership membership
      ON membership.device_set_id = attachment.channel_id
     AND membership.org_id = attachment.org_id
     AND membership.device_set_type = 'channel'
    JOIN channel_firmware_enforcement enforcement
      ON enforcement.device_id = membership.device_id
     AND enforcement.org_id = membership.org_id
    JOIN channel_firmware_authority authority
      ON authority.id = enforcement.authority_id
     AND authority.org_id = enforcement.org_id
    LEFT JOIN rollout_lane_membership_change membership_change
      ON membership_change.authority_id = authority.id
     AND membership_change.org_id = authority.org_id
    WHERE attachment.org_id = sqlc.arg('org_id')
      AND (
          authority.authority_type = 'rollout_lane_initial'
              AND authority.authority_reference = attachment.lane_id::text
          OR authority.authority_type = 'rollout_lane_membership'
              AND membership_change.target_lane_id = attachment.lane_id
      )
),
active_lane_ids AS (
    SELECT DISTINCT lane_id
    FROM ranked
    WHERE rank = 1
      AND state IN ('pending', 'held', 'dispatching', 'dispatched', 'verifying')
)
SELECT lane.id AS lane_id,
       COUNT(ranked.id)::bigint AS total_count,
       COUNT(ranked.id) FILTER (
           WHERE ranked.state IN ('pending', 'held')
       )::bigint AS pending_count,
       COUNT(ranked.id) FILTER (
           WHERE ranked.state IN ('dispatching', 'dispatched')
       )::bigint AS updating_count,
       COUNT(ranked.id) FILTER (
           WHERE ranked.state = 'verifying'
       )::bigint AS verifying_count,
       COUNT(ranked.id) FILTER (
           WHERE ranked.state = 'confirmed'
       )::bigint AS confirmed_count,
       COUNT(ranked.id) FILTER (
           WHERE ranked.state IN ('attention_required', 'cancelled')
       )::bigint AS attention_count
FROM active_lane_ids active
JOIN rollout_lane lane
  ON lane.id = active.lane_id
 AND lane.org_id = sqlc.arg('org_id')
LEFT JOIN ranked
  ON ranked.lane_id = lane.id
 AND ranked.rank = 1
WHERE lane.deleted_at IS NULL
GROUP BY lane.id;

-- name: ListRolloutLaneFirmwareConvergenceMembers :many
WITH latest AS (
    SELECT DISTINCT ON (membership.device_id)
           membership.device_id,
           enforcement.last_observed_firmware_version,
           enforcement.desired_firmware_version,
           enforcement.state,
           enforcement.last_error,
           enforcement.updated_at
    FROM rollout_lane_channel attachment
    JOIN device_set_membership membership
      ON membership.device_set_id = attachment.channel_id
     AND membership.org_id = attachment.org_id
     AND membership.device_set_type = 'channel'
    JOIN channel_firmware_enforcement enforcement
      ON enforcement.device_id = membership.device_id
     AND enforcement.org_id = membership.org_id
    JOIN channel_firmware_authority authority
      ON authority.id = enforcement.authority_id
     AND authority.org_id = enforcement.org_id
    LEFT JOIN rollout_lane_membership_change membership_change
      ON membership_change.authority_id = authority.id
     AND membership_change.org_id = authority.org_id
    WHERE attachment.lane_id = sqlc.arg('lane_id')
      AND attachment.org_id = sqlc.arg('org_id')
      AND (
          authority.authority_type = 'rollout_lane_initial'
              AND authority.authority_reference = attachment.lane_id::text
          OR authority.authority_type = 'rollout_lane_membership'
              AND membership_change.target_lane_id = attachment.lane_id
      )
    ORDER BY membership.device_id, enforcement.desired_at DESC, enforcement.id DESC
)
SELECT device.device_identifier,
       COALESCE(discovered.manufacturer, '') AS manufacturer,
       COALESCE(discovered.model, '') AS model,
       latest.last_observed_firmware_version,
       latest.desired_firmware_version AS target_firmware_version,
       latest.state,
       latest.last_error,
       latest.updated_at
FROM latest
JOIN device
  ON device.id = latest.device_id
 AND device.org_id = sqlc.arg('org_id')
LEFT JOIN discovered_device discovered
  ON discovered.id = device.discovered_device_id
 AND discovered.org_id = device.org_id
 AND discovered.deleted_at IS NULL
WHERE (
      sqlc.narg('members_updated_after')::timestamptz IS NULL
      OR latest.updated_at > sqlc.narg('members_updated_after')::timestamptz
  )
ORDER BY device.device_identifier;

-- name: ListRolloutLaneChannelTransitions :many
SELECT device.id AS device_id,
       device.device_identifier,
       COALESCE(discovered.manufacturer, '') AS manufacturer,
       COALESCE(discovered.model, '') AS model,
       source_target.id AS source_release_target_id,
       source_target.firmware_file_id AS source_firmware_file_id,
       source_target.firmware_version AS source_firmware_version,
       source_target.sha256 AS source_sha256
FROM rollout_lane lane
JOIN device_set_membership membership
  ON membership.device_set_id = lane.current_channel_id
 AND membership.org_id = lane.org_id
 AND membership.device_set_type = 'channel'
JOIN device
  ON device.id = membership.device_id
 AND device.org_id = membership.org_id
 AND device.deleted_at IS NULL
JOIN discovered_device discovered
  ON discovered.id = device.discovered_device_id
 AND discovered.org_id = device.org_id
 AND discovered.deleted_at IS NULL
JOIN device_set_channel source_channel
  ON source_channel.device_set_id = lane.current_channel_id
 AND source_channel.org_id = lane.org_id
LEFT JOIN firmware_release_target source_target
  ON source_target.release_set_id = source_channel.release_set_id
 AND source_target.org_id = source_channel.org_id
 AND lower(source_target.target_manufacturer) = lower(COALESCE(discovered.manufacturer, ''))
 AND lower(source_target.target_model) = lower(COALESCE(discovered.model, ''))
WHERE lane.id = sqlc.arg('lane_id')
  AND lane.org_id = sqlc.arg('org_id')
ORDER BY device.device_identifier;

-- name: ListRolloutLaneModelTransitions :many
SELECT device.id AS device_id,
       device.device_identifier,
       COALESCE(discovered.manufacturer, '') AS manufacturer,
       COALESCE(discovered.model, '') AS model,
       source_target.id AS source_release_target_id,
       source_target.firmware_file_id AS source_firmware_file_id,
       source_target.firmware_version AS source_firmware_version,
       source_target.sha256 AS source_sha256,
       declaration.model_identity_key,
       discovered.model_identity_observed_at
FROM rollout_lane_model declaration
JOIN rollout_lane_model_binding binding
  ON binding.lane_model_id = declaration.id
 AND binding.lane_id = declaration.lane_id
 AND binding.org_id = declaration.org_id
 AND binding.channel_id = declaration.current_channel_id
 AND binding.ended_at IS NULL
JOIN device_set_membership membership
  ON membership.device_set_id = binding.channel_id
 AND membership.org_id = binding.org_id
 AND membership.device_id = binding.device_id
 AND membership.device_set_type = 'channel'
JOIN device
  ON device.id = binding.device_id
 AND device.org_id = binding.org_id
 AND device.deleted_at IS NULL
JOIN discovered_device discovered
  ON discovered.id = device.discovered_device_id
 AND discovered.org_id = device.org_id
 AND discovered.deleted_at IS NULL
JOIN firmware_release_target source_target
  ON source_target.id = declaration.current_release_target_id
 AND source_target.release_set_id = declaration.current_release_set_id
 AND source_target.org_id = declaration.org_id
WHERE declaration.id = sqlc.arg('lane_model_id')
  AND declaration.lane_id = sqlc.arg('lane_id')
  AND declaration.org_id = sqlc.arg('org_id')
ORDER BY device.device_identifier;

-- name: ListBetweenChannelAdmissionMembers :many
SELECT member.id,
       member.device_id,
       member.device_identifier,
       member.revision
FROM (
    SELECT rollout_member.id,
           rollout_member.device_id,
           device.device_identifier,
           rollout_member.revision,
           rollout_member.position
    FROM firmware_rollout_member rollout_member
    JOIN device
      ON device.id = rollout_member.device_id
     AND device.org_id = rollout_member.org_id
    JOIN device_set_membership membership
      ON membership.device_id = rollout_member.device_id
     AND membership.org_id = rollout_member.org_id
     AND membership.device_set_type = 'channel'
     AND membership.device_set_id = sqlc.arg('source_channel_id')
    WHERE rollout_member.rollout_id = sqlc.arg('rollout_id')
      AND rollout_member.batch_id = sqlc.arg('batch_id')
      AND rollout_member.org_id = sqlc.arg('org_id')
      AND rollout_member.state = 'admitted'
      AND rollout_member.owner_released_at IS NULL
    ORDER BY device.device_identifier
    FOR UPDATE OF rollout_member
) AS member
ORDER BY member.device_identifier;

-- name: CountBetweenChannelAdmittedBatchMembers :one
SELECT COUNT(*)::bigint
FROM firmware_rollout_member
WHERE rollout_id = sqlc.arg('rollout_id')
  AND batch_id = sqlc.arg('batch_id')
  AND org_id = sqlc.arg('org_id')
  AND state = 'admitted'
  AND owner_released_at IS NULL;

-- name: FreezeBetweenChannelMemberReleaseTargets :execrows
UPDATE firmware_rollout_member member
SET source_release_set_id = rollout.source_release_set_id,
    source_release_target_id = source_target.id,
    target_release_set_id = rollout.target_release_set_id,
    target_release_target_id = target_target.id,
    revision = member.revision + 1
FROM firmware_rollout rollout,
     device,
     discovered_device discovered,
     firmware_release_target source_target,
     firmware_release_target target_target
WHERE member.rollout_id = sqlc.arg('rollout_id')
  AND member.org_id = sqlc.arg('org_id')
  AND rollout.id = member.rollout_id
  AND rollout.org_id = member.org_id
  AND rollout.strategy_key = 'between_channel'
  AND device.id = member.device_id
  AND device.org_id = member.org_id
  AND discovered.id = device.discovered_device_id
  AND discovered.org_id = device.org_id
  AND source_target.release_set_id = rollout.source_release_set_id
  AND source_target.org_id = member.org_id
  AND lower(source_target.target_manufacturer) =
      lower(COALESCE(discovered.manufacturer, ''))
  AND lower(source_target.target_model) =
      lower(COALESCE(discovered.model, ''))
  AND target_target.release_set_id = rollout.target_release_set_id
  AND target_target.org_id = member.org_id
  AND lower(target_target.target_manufacturer) =
      lower(COALESCE(discovered.manufacturer, ''))
  AND lower(target_target.target_model) =
      lower(COALESCE(discovered.model, ''))
  AND member.source_release_target_id IS NULL
  AND member.target_release_target_id IS NULL;

-- name: CreateBetweenChannelAdmissionEnforcements :execrows
INSERT INTO channel_firmware_enforcement (
    org_id,
    device_id,
    desired_release_set_id,
    desired_release_target_id,
    desired_firmware_file_id,
    desired_firmware_version,
    cause_type,
    cause_reference,
    authority_id,
    authority_revision,
    expected_model_identity_key,
    model_identity_validated_at
)
SELECT rollout_member.org_id,
       rollout_member.device_id,
       target.release_set_id,
       target.id,
       target.firmware_file_id,
       target.firmware_version,
       'between_channel_forward',
       rollout_member.id::text,
       authority.id,
       authority.revision,
       rollout_member.model_identity_key,
       rollout_member.model_identity_validated_at
FROM firmware_rollout_member rollout_member
JOIN firmware_release_target target
  ON target.id = rollout_member.target_release_target_id
 AND target.release_set_id = rollout_member.target_release_set_id
 AND target.org_id = rollout_member.org_id
JOIN channel_firmware_authority authority
  ON authority.id = sqlc.arg('authority_id')
 AND authority.org_id = rollout_member.org_id
 AND authority.revision = sqlc.arg('authority_revision')
 AND authority.halted_at IS NULL
WHERE rollout_member.rollout_id = sqlc.arg('rollout_id')
  AND rollout_member.batch_id = sqlc.arg('batch_id')
  AND rollout_member.org_id = sqlc.arg('org_id')
  AND rollout_member.state = 'admitted'
  AND rollout_member.owner_released_at IS NULL
ON CONFLICT (authority_id, device_id) DO NOTHING;

-- name: AttachBetweenChannelAdmissionEnforcements :execrows
UPDATE firmware_rollout_member member
SET enforcement_id = enforcement.id,
    last_error = NULL,
    revision = member.revision + 1
FROM channel_firmware_enforcement enforcement
WHERE member.rollout_id = sqlc.arg('rollout_id')
  AND member.batch_id = sqlc.arg('batch_id')
  AND member.org_id = sqlc.arg('org_id')
  AND member.state = 'admitted'
  AND member.owner_released_at IS NULL
  AND enforcement.authority_id = sqlc.arg('authority_id')
  AND enforcement.device_id = member.device_id
  AND enforcement.cause_type = 'between_channel_forward'
  AND enforcement.cause_reference = member.id::text
  AND member.enforcement_id IS DISTINCT FROM enforcement.id;

-- name: CountBetweenChannelAttachedAdmissionMembers :one
SELECT COUNT(*)::bigint
FROM firmware_rollout_member member
JOIN channel_firmware_enforcement enforcement
  ON enforcement.id = member.enforcement_id
 AND enforcement.org_id = member.org_id
 AND enforcement.device_id = member.device_id
JOIN channel_firmware_authority authority
  ON authority.id = enforcement.authority_id
 AND authority.org_id = enforcement.org_id
WHERE member.rollout_id = sqlc.arg('rollout_id')
  AND member.batch_id = sqlc.arg('batch_id')
  AND member.org_id = sqlc.arg('org_id')
  AND member.state = 'admitted'
  AND member.owner_released_at IS NULL
  AND enforcement.authority_id = sqlc.arg('authority_id')
  AND enforcement.cause_type = 'between_channel_forward';

-- name: CaptureBetweenChannelBatchBaseline :execrows
INSERT INTO firmware_rollout_evidence (
    rollout_id,
    member_id,
    org_id,
    phase,
    window_start,
    window_end,
    observed_at,
    avg_hashrate_hs,
    avg_power_w,
    avg_temperature_c,
    error_count,
    sample_count
)
SELECT member.rollout_id,
       member.id,
       member.org_id,
       'baseline',
       sqlc.arg('window_start'),
       sqlc.arg('window_end'),
       CASE WHEN MAX(metrics.time) >= sqlc.arg('fresh_after') THEN MAX(metrics.time) ELSE NULL END,
       CASE WHEN MAX(metrics.time) >= sqlc.arg('fresh_after') THEN AVG(metrics.hash_rate_hs)::float8 ELSE NULL END,
       CASE WHEN MAX(metrics.time) >= sqlc.arg('fresh_after') THEN AVG(metrics.power_w)::float8 ELSE NULL END,
       CASE WHEN MAX(metrics.time) >= sqlc.arg('fresh_after') THEN AVG(metrics.temp_c)::float8 ELSE NULL END,
       CASE
           WHEN MAX(metrics.time) >= sqlc.arg('fresh_after') THEN (
               SELECT COUNT(*)::bigint
               FROM errors error_row
               WHERE error_row.device_id = member.device_id
                 AND error_row.org_id = member.org_id
                 AND error_row.last_seen_at >= sqlc.arg('window_start')
                 AND error_row.first_seen_at <= sqlc.arg('window_end')
           )
           ELSE NULL
       END,
       CASE WHEN MAX(metrics.time) >= sqlc.arg('fresh_after') THEN COUNT(metrics.time)::bigint ELSE NULL END
FROM firmware_rollout_member member
JOIN device
  ON device.id = member.device_id
 AND device.org_id = member.org_id
LEFT JOIN device_metrics metrics
  ON metrics.device_identifier = device.device_identifier
 AND metrics.time >= sqlc.arg('window_start')
 AND metrics.time <= sqlc.arg('window_end')
WHERE member.rollout_id = sqlc.arg('rollout_id')
  AND member.batch_id = sqlc.arg('batch_id')
  AND member.org_id = sqlc.arg('org_id')
GROUP BY member.rollout_id,
         member.id,
         member.org_id,
         member.device_id,
         member.position
ON CONFLICT (member_id, phase) DO NOTHING;

-- name: MarkBetweenChannelRevertMembershipConflicts :execrows
UPDATE firmware_rollout_member member
SET state = 'attention_required',
    last_error = 'device no longer belongs to the rollout target channel',
    settled_at = CURRENT_TIMESTAMP,
    owner_released_at = CURRENT_TIMESTAMP,
    revision = member.revision + 1
WHERE member.rollout_id = sqlc.arg('rollout_id')
  AND member.org_id = sqlc.arg('org_id')
  AND member.state = 'reverting'
  AND member.owner_released_at IS NULL
  AND NOT EXISTS (
      SELECT 1
      FROM device_set_membership membership
      WHERE membership.org_id = member.org_id
        AND membership.device_id = member.device_id
        AND membership.device_set_type = 'channel'
        AND membership.device_set_id = sqlc.arg('target_channel_id')
  );

-- name: CreateBetweenChannelRevertEnforcements :execrows
INSERT INTO channel_firmware_enforcement (
    org_id,
    device_id,
    desired_release_set_id,
    desired_release_target_id,
    desired_firmware_file_id,
    desired_firmware_version,
    cause_type,
    cause_reference,
    authority_id,
    authority_revision
)
SELECT member.org_id,
       member.device_id,
       source_target.release_set_id,
       source_target.id,
       source_target.firmware_file_id,
       source_target.firmware_version,
       'between_channel_revert',
       member.id::text,
       authority.id,
       authority.revision
FROM firmware_rollout_member member
JOIN firmware_release_target source_target
  ON source_target.id = member.source_release_target_id
 AND source_target.release_set_id = member.source_release_set_id
 AND source_target.org_id = member.org_id
JOIN device_set_membership membership
  ON membership.org_id = member.org_id
 AND membership.device_id = member.device_id
 AND membership.device_set_type = 'channel'
 AND membership.device_set_id = sqlc.arg('target_channel_id')
JOIN channel_firmware_authority authority
  ON authority.id = sqlc.arg('authority_id')
 AND authority.org_id = member.org_id
 AND authority.revision = sqlc.arg('authority_revision')
 AND authority.halted_at IS NULL
WHERE member.rollout_id = sqlc.arg('rollout_id')
  AND member.org_id = sqlc.arg('org_id')
  AND member.state = 'reverting'
  AND member.owner_released_at IS NULL
ON CONFLICT (authority_id, device_id) DO NOTHING;

-- name: AttachBetweenChannelRevertEnforcements :execrows
UPDATE firmware_rollout_member member
SET enforcement_id = enforcement.id,
    last_error = NULL,
    revision = member.revision + 1
FROM channel_firmware_enforcement enforcement
WHERE member.rollout_id = sqlc.arg('rollout_id')
  AND member.org_id = sqlc.arg('org_id')
  AND member.state = 'reverting'
  AND member.owner_released_at IS NULL
  AND enforcement.authority_id = sqlc.arg('authority_id')
  AND enforcement.device_id = member.device_id
  AND enforcement.cause_type = 'between_channel_revert'
  AND enforcement.cause_reference = member.id::text
  AND member.enforcement_id IS DISTINCT FROM enforcement.id;

-- name: CountBetweenChannelRevertMembersWithoutEnforcement :one
SELECT COUNT(*)::bigint
FROM firmware_rollout_member member
LEFT JOIN channel_firmware_enforcement enforcement
  ON enforcement.id = member.enforcement_id
 AND enforcement.org_id = member.org_id
 AND enforcement.device_id = member.device_id
 AND enforcement.authority_id = sqlc.arg('authority_id')
 AND enforcement.cause_type = 'between_channel_revert'
 AND enforcement.cause_reference = member.id::text
WHERE member.rollout_id = sqlc.arg('rollout_id')
  AND member.org_id = sqlc.arg('org_id')
  AND member.state = 'reverting'
  AND member.owner_released_at IS NULL
  AND enforcement.id IS NULL;

-- name: ListBetweenChannelFinalizations :many
SELECT member.id AS member_id,
       member.rollout_id,
       member.org_id,
       member.batch_id,
       member.device_id,
       device.device_identifier,
       member.state AS member_state,
       member.revision AS member_revision,
       enforcement.id AS enforcement_id,
       enforcement.state AS enforcement_state,
       enforcement.authority_id,
       enforcement.last_error,
       rollout.state AS rollout_state,
       rollout.revision AS rollout_revision,
       rollout.forward_authority_id,
       rollout.forward_authority_revision,
       rollout.revert_authority_id,
       rollout.revert_authority_revision,
       rollout.created_by_user_id,
       rollout.source_channel_id,
       rollout.target_channel_id,
       rollout.lane_model_id,
       rollout.group_id,
       rollout.model_identity_key,
       COALESCE(declaration.manufacturer, '')::text AS manufacturer,
       COALESCE(declaration.model, '')::text AS model,
       member.model_identity_validated_at,
       enforcement.command_completed_at,
       COALESCE(rollout_model_identity_v1(discovered.manufacturer, discovered.model), '')::text
           AS observed_model_identity_key,
       discovered.model_identity_observed_at,
       lane.id AS lane_id,
       lane.current_channel_id,
       declaration.current_channel_id AS model_current_channel_id
FROM firmware_rollout_member member
JOIN firmware_rollout rollout
  ON rollout.id = member.rollout_id
 AND rollout.org_id = member.org_id
JOIN device
  ON device.id = member.device_id
 AND device.org_id = member.org_id
LEFT JOIN discovered_device discovered
  ON discovered.id = device.discovered_device_id
 AND discovered.org_id = device.org_id
 AND discovered.deleted_at IS NULL
JOIN channel_firmware_enforcement enforcement
  ON enforcement.id = member.enforcement_id
 AND enforcement.org_id = member.org_id
 AND enforcement.device_id = member.device_id
JOIN channel_firmware_authority authority
  ON authority.id = enforcement.authority_id
 AND authority.org_id = enforcement.org_id
JOIN rollout_lane_channel attachment
  ON attachment.rollout_id = rollout.id
 AND attachment.org_id = rollout.org_id
JOIN rollout_lane lane
  ON lane.id = attachment.lane_id
 AND lane.org_id = attachment.org_id
LEFT JOIN rollout_lane_model declaration
  ON declaration.id = rollout.lane_model_id
 AND declaration.lane_id = lane.id
 AND declaration.org_id = lane.org_id
WHERE rollout.strategy_key = 'between_channel'
  AND member.owner_released_at IS NULL
  AND (
      (member.state = 'admitted'
          AND enforcement.cause_type = 'between_channel_forward')
      OR
      (member.state = 'reverting'
          AND enforcement.cause_type = 'between_channel_revert')
  )
  AND (
      enforcement.state IN ('confirmed', 'attention_required', 'cancelled')
      OR (
          enforcement.state IN ('pending', 'held')
          AND authority.halted_at IS NOT NULL
      )
  )
ORDER BY enforcement.updated_at, enforcement.id
LIMIT sqlc.arg('finalize_limit');

-- name: GetBetweenChannelFinalizationForUpdate :one
SELECT member.id AS member_id,
       member.rollout_id,
       member.org_id,
       member.batch_id,
       member.device_id,
       device.device_identifier,
       member.state AS member_state,
       member.revision AS member_revision,
       enforcement.id AS enforcement_id,
       enforcement.state AS enforcement_state,
       enforcement.authority_id,
       enforcement.last_error,
       rollout.state AS rollout_state,
       rollout.revision AS rollout_revision,
       rollout.forward_authority_id,
       rollout.forward_authority_revision,
       rollout.revert_authority_id,
       rollout.revert_authority_revision,
       rollout.created_by_user_id,
       rollout.source_channel_id,
       rollout.target_channel_id,
       rollout.lane_model_id,
       rollout.group_id,
       rollout.model_identity_key,
       COALESCE(declaration.manufacturer, '')::text AS manufacturer,
       COALESCE(declaration.model, '')::text AS model,
       member.model_identity_validated_at,
       enforcement.command_completed_at,
       COALESCE(rollout_model_identity_v1(discovered.manufacturer, discovered.model), '')::text
           AS observed_model_identity_key,
       discovered.model_identity_observed_at,
       lane.id AS lane_id,
       lane.current_channel_id,
       declaration.current_channel_id AS model_current_channel_id
FROM firmware_rollout_member member
JOIN firmware_rollout rollout
  ON rollout.id = member.rollout_id
 AND rollout.org_id = member.org_id
JOIN device
  ON device.id = member.device_id
 AND device.org_id = member.org_id
LEFT JOIN discovered_device discovered
  ON discovered.id = device.discovered_device_id
 AND discovered.org_id = device.org_id
 AND discovered.deleted_at IS NULL
JOIN channel_firmware_enforcement enforcement
  ON enforcement.id = member.enforcement_id
 AND enforcement.org_id = member.org_id
 AND enforcement.device_id = member.device_id
JOIN rollout_lane_channel attachment
  ON attachment.rollout_id = rollout.id
 AND attachment.org_id = rollout.org_id
JOIN rollout_lane lane
  ON lane.id = attachment.lane_id
 AND lane.org_id = attachment.org_id
LEFT JOIN rollout_lane_model declaration
  ON declaration.id = rollout.lane_model_id
 AND declaration.lane_id = lane.id
 AND declaration.org_id = lane.org_id
WHERE member.id = sqlc.arg('member_id')
  AND member.org_id = sqlc.arg('org_id')
FOR UPDATE OF member, enforcement, rollout, lane;

-- name: GetDeviceChannelMembership :one
SELECT device_set_id
FROM device_set_membership
WHERE org_id = sqlc.arg('org_id')
  AND device_id = sqlc.arg('device_id')
  AND device_set_type = 'channel';

-- name: FinalizeBetweenChannelForward :one
WITH removed AS (
    DELETE FROM device_set_membership membership
    WHERE membership.org_id = sqlc.arg('org_id')
      AND membership.device_id = sqlc.arg('device_id')
      AND membership.device_set_type = 'channel'
      AND membership.device_set_id = sqlc.arg('source_channel_id')
    RETURNING membership.org_id,
              membership.device_id,
              membership.device_identifier
),
inserted AS (
    INSERT INTO device_set_membership (
        org_id,
        device_set_id,
        device_set_type,
        device_id,
        device_identifier
    )
    SELECT removed.org_id,
           sqlc.arg('target_channel_id'),
           'channel',
           removed.device_id,
           removed.device_identifier
    FROM removed
    RETURNING device_id
)
UPDATE firmware_rollout_member member
SET state = 'succeeded',
    settled_at = CURRENT_TIMESTAMP,
    owner_released_at = CASE
        WHEN rollout.state = 'aborted' THEN CURRENT_TIMESTAMP
        ELSE member.owner_released_at
    END,
    last_error = NULL,
    revision = member.revision + 1
FROM inserted,
     firmware_rollout rollout
WHERE member.id = sqlc.arg('member_id')
  AND member.rollout_id = sqlc.arg('rollout_id')
  AND member.org_id = sqlc.arg('org_id')
  AND rollout.id = member.rollout_id
  AND rollout.org_id = member.org_id
  AND member.device_id = inserted.device_id
  AND member.state = 'admitted'
  AND member.revision = sqlc.arg('expected_revision')
  AND member.owner_released_at IS NULL
RETURNING member.*;

-- name: FinalizeBetweenChannelModelForward :one
WITH ended_binding AS (
    UPDATE rollout_lane_model_binding binding
    SET ended_at = CURRENT_TIMESTAMP,
        revision = binding.revision + 1
    WHERE binding.lane_model_id = sqlc.arg('lane_model_id')
      AND binding.lane_id = sqlc.arg('lane_id')
      AND binding.org_id = sqlc.arg('org_id')
      AND binding.device_id = sqlc.arg('device_id')
      AND binding.channel_id = sqlc.arg('source_channel_id')
      AND binding.ended_at IS NULL
    RETURNING binding.device_id,
              binding.model_identity_key
),
removed AS (
    DELETE FROM device_set_membership membership
    USING ended_binding
    WHERE membership.org_id = sqlc.arg('org_id')
      AND membership.device_id = ended_binding.device_id
      AND membership.device_set_type = 'channel'
      AND membership.device_set_id = sqlc.arg('source_channel_id')
    RETURNING membership.org_id,
              membership.device_id,
              membership.device_identifier
),
inserted AS (
    INSERT INTO device_set_membership (
        org_id,
        device_set_id,
        device_set_type,
        device_id,
        device_identifier
    )
    SELECT removed.org_id,
           sqlc.arg('target_channel_id'),
           'channel',
           removed.device_id,
           removed.device_identifier
    FROM removed
    RETURNING device_id
),
inserted_binding AS (
    INSERT INTO rollout_lane_model_binding (
        id,
        lane_id,
        lane_model_id,
        org_id,
        device_id,
        channel_id,
        model_identity_key,
        model_identity_observed_at,
        origin
    )
    SELECT sqlc.arg('binding_id'),
           sqlc.arg('lane_id'),
           sqlc.arg('lane_model_id'),
           sqlc.arg('org_id'),
           inserted.device_id,
           sqlc.arg('target_channel_id'),
           sqlc.arg('model_identity_key'),
           sqlc.arg('model_identity_observed_at'),
           'topology'
    FROM inserted
    RETURNING device_id
)
UPDATE firmware_rollout_member member
SET state = 'succeeded',
    settled_at = CURRENT_TIMESTAMP,
    owner_released_at = CASE
        WHEN rollout.state = 'aborted' THEN CURRENT_TIMESTAMP
        ELSE member.owner_released_at
    END,
    last_error = NULL,
    revision = member.revision + 1
FROM inserted_binding,
     firmware_rollout rollout
WHERE member.id = sqlc.arg('member_id')
  AND member.rollout_id = sqlc.arg('rollout_id')
  AND member.org_id = sqlc.arg('org_id')
  AND rollout.id = member.rollout_id
  AND rollout.org_id = member.org_id
  AND member.device_id = inserted_binding.device_id
  AND member.state = 'admitted'
  AND member.revision = sqlc.arg('expected_revision')
  AND member.owner_released_at IS NULL
RETURNING member.*;

-- name: FinalizeBetweenChannelRevert :one
WITH removed AS (
    DELETE FROM device_set_membership membership
    WHERE membership.org_id = sqlc.arg('org_id')
      AND membership.device_id = sqlc.arg('device_id')
      AND membership.device_set_type = 'channel'
      AND membership.device_set_id = sqlc.arg('target_channel_id')
    RETURNING membership.org_id,
              membership.device_id,
              membership.device_identifier
),
inserted AS (
    INSERT INTO device_set_membership (
        org_id,
        device_set_id,
        device_set_type,
        device_id,
        device_identifier
    )
    SELECT removed.org_id,
           sqlc.arg('source_channel_id'),
           'channel',
           removed.device_id,
           removed.device_identifier
    FROM removed
    RETURNING device_id
)
UPDATE firmware_rollout_member member
SET state = 'reverted',
    settled_at = CURRENT_TIMESTAMP,
    last_error = NULL,
    revision = member.revision + 1
FROM inserted
WHERE member.id = sqlc.arg('member_id')
  AND member.rollout_id = sqlc.arg('rollout_id')
  AND member.org_id = sqlc.arg('org_id')
  AND member.device_id = inserted.device_id
  AND member.state = 'reverting'
  AND member.revision = sqlc.arg('expected_revision')
  AND member.owner_released_at IS NULL
RETURNING member.*;

-- name: FinalizeBetweenChannelModelRevert :one
WITH ended_binding AS (
    UPDATE rollout_lane_model_binding binding
    SET ended_at = CURRENT_TIMESTAMP,
        revision = binding.revision + 1
    WHERE binding.lane_model_id = sqlc.arg('lane_model_id')
      AND binding.lane_id = sqlc.arg('lane_id')
      AND binding.org_id = sqlc.arg('org_id')
      AND binding.device_id = sqlc.arg('device_id')
      AND binding.channel_id = sqlc.arg('target_channel_id')
      AND binding.ended_at IS NULL
    RETURNING binding.device_id,
              binding.model_identity_key
),
removed AS (
    DELETE FROM device_set_membership membership
    USING ended_binding
    WHERE membership.org_id = sqlc.arg('org_id')
      AND membership.device_id = ended_binding.device_id
      AND membership.device_set_type = 'channel'
      AND membership.device_set_id = sqlc.arg('target_channel_id')
    RETURNING membership.org_id,
              membership.device_id,
              membership.device_identifier
),
inserted AS (
    INSERT INTO device_set_membership (
        org_id,
        device_set_id,
        device_set_type,
        device_id,
        device_identifier
    )
    SELECT removed.org_id,
           sqlc.arg('source_channel_id'),
           'channel',
           removed.device_id,
           removed.device_identifier
    FROM removed
    RETURNING device_id
),
inserted_binding AS (
    INSERT INTO rollout_lane_model_binding (
        id,
        lane_id,
        lane_model_id,
        org_id,
        device_id,
        channel_id,
        model_identity_key,
        model_identity_observed_at,
        origin
    )
    SELECT sqlc.arg('binding_id'),
           sqlc.arg('lane_id'),
           sqlc.arg('lane_model_id'),
           sqlc.arg('org_id'),
           inserted.device_id,
           sqlc.arg('source_channel_id'),
           sqlc.arg('model_identity_key'),
           sqlc.arg('model_identity_observed_at'),
           'topology'
    FROM inserted
    RETURNING device_id
)
UPDATE firmware_rollout_member member
SET state = 'reverted',
    settled_at = CURRENT_TIMESTAMP,
    last_error = NULL,
    revision = member.revision + 1
FROM inserted_binding
WHERE member.id = sqlc.arg('member_id')
  AND member.rollout_id = sqlc.arg('rollout_id')
  AND member.org_id = sqlc.arg('org_id')
  AND member.device_id = inserted_binding.device_id
  AND member.state = 'reverting'
  AND member.revision = sqlc.arg('expected_revision')
  AND member.owner_released_at IS NULL
RETURNING member.*;

-- name: MarkBetweenChannelMemberTerminal :one
UPDATE firmware_rollout_member
SET state = sqlc.arg('member_state'),
    settled_at = CURRENT_TIMESTAMP,
    owner_released_at = CURRENT_TIMESTAMP,
    last_error = sqlc.narg('last_error'),
    revision = revision + 1
WHERE id = sqlc.arg('member_id')
  AND rollout_id = sqlc.arg('rollout_id')
  AND org_id = sqlc.arg('org_id')
  AND revision = sqlc.arg('expected_revision')
  AND state = sqlc.arg('expected_state')
  AND owner_released_at IS NULL
RETURNING *;

-- name: CompleteSettledBetweenChannelBatch :one
WITH completed AS (
    UPDATE firmware_rollout_batch batch
    SET state = 'completed',
        completed_at = CURRENT_TIMESTAMP,
        revision = batch.revision + 1
    WHERE batch.id = sqlc.arg('batch_id')
      AND batch.rollout_id = sqlc.arg('rollout_id')
      AND batch.org_id = sqlc.arg('org_id')
      AND batch.state = 'admitted'
      AND NOT EXISTS (
          SELECT 1
          FROM firmware_rollout_member member
          WHERE member.batch_id = batch.id
            AND member.rollout_id = batch.rollout_id
            AND member.org_id = batch.org_id
            AND member.state NOT IN (
                'succeeded',
                'failed',
                'attention_required',
                'cancelled'
            )
      )
    RETURNING batch.*
)
SELECT completed.*,
       NOT EXISTS (
           SELECT 1
           FROM firmware_rollout_batch later
           WHERE later.rollout_id = completed.rollout_id
             AND later.org_id = completed.org_id
             AND later.position > completed.position
       ) AS is_final_batch
FROM completed;

-- name: CompleteSettledBetweenChannelBatches :execrows
UPDATE firmware_rollout_batch batch
SET state = 'completed',
    completed_at = CURRENT_TIMESTAMP,
    revision = batch.revision + 1
WHERE batch.rollout_id = sqlc.arg('rollout_id')
  AND batch.org_id = sqlc.arg('org_id')
  AND batch.state = 'admitted'
  AND EXISTS (
      SELECT 1
      FROM firmware_rollout rollout
      WHERE rollout.id = batch.rollout_id
        AND rollout.org_id = batch.org_id
        AND rollout.strategy_key = 'between_channel'
  )
  AND NOT EXISTS (
      SELECT 1
      FROM firmware_rollout_member member
      WHERE member.batch_id = batch.id
        AND member.rollout_id = batch.rollout_id
        AND member.org_id = batch.org_id
        AND member.state NOT IN (
            'succeeded',
            'failed',
            'attention_required',
            'cancelled'
        )
  );

-- name: MoveBetweenChannelRolloutToReview :one
UPDATE firmware_rollout
SET state = 'review',
    resume_state = NULL,
    revision = revision + 1
WHERE id = sqlc.arg('rollout_id')
  AND org_id = sqlc.arg('org_id')
  AND revision = sqlc.arg('expected_revision')
  AND state IN ('running', 'paused')
RETURNING *;

-- name: GetBetweenChannelForwardSettlement :one
SELECT COUNT(*)::bigint AS total_members,
       COUNT(*) FILTER (
           WHERE member.state IN (
               'succeeded',
               'failed',
               'attention_required',
               'cancelled'
           )
       )::bigint AS terminal_members,
       COUNT(*) FILTER (
           WHERE member.state <> 'succeeded'
       )::bigint AS failed_members,
       (
           SELECT COUNT(*)::bigint
           FROM firmware_rollout_batch batch
           WHERE batch.rollout_id = sqlc.arg('rollout_id')
             AND batch.org_id = sqlc.arg('org_id')
             AND batch.state <> 'completed'
       ) AS incomplete_batches
FROM firmware_rollout_member member
WHERE member.rollout_id = sqlc.arg('rollout_id')
  AND member.org_id = sqlc.arg('org_id');

-- name: GetBetweenChannelRevertSettlement :one
SELECT COUNT(*) FILTER (
           WHERE member.revert_selected_at IS NOT NULL
       )::bigint AS selected_members,
       COUNT(*) FILTER (
           WHERE member.revert_selected_at IS NOT NULL
             AND member.state IN (
                 'reverted',
                 'failed',
                 'attention_required',
                 'cancelled'
             )
       )::bigint AS terminal_members,
       COUNT(*) FILTER (
           WHERE member.revert_selected_at IS NOT NULL
             AND member.state <> 'reverted'
       )::bigint AS failed_members
FROM firmware_rollout_member member
WHERE member.rollout_id = sqlc.arg('rollout_id')
  AND member.org_id = sqlc.arg('org_id');

-- name: CompleteBetweenChannelRollout :one
UPDATE firmware_rollout
SET state = sqlc.arg('target_state'),
    resume_state = NULL,
    forward_authority_revision = COALESCE(
        sqlc.narg('forward_authority_revision'),
        forward_authority_revision
    ),
    revert_authority_revision = COALESCE(
        sqlc.narg('revert_authority_revision'),
        revert_authority_revision
    ),
    completed_at = CASE
        WHEN sqlc.arg('target_state') IN ('completed', 'completed_with_failures')
            THEN CURRENT_TIMESTAMP
        ELSE completed_at
    END,
    reverted_at = CASE
        WHEN sqlc.arg('target_state') = 'reverted'
            THEN CURRENT_TIMESTAMP
        ELSE reverted_at
    END,
    revision = revision + 1
WHERE id = sqlc.arg('rollout_id')
  AND org_id = sqlc.arg('org_id')
  AND revision = sqlc.arg('expected_revision')
  AND state = sqlc.arg('expected_state')
RETURNING *;

-- name: CancelHaltedBetweenChannelEnforcement :execrows
UPDATE channel_firmware_enforcement enforcement
SET state = 'cancelled',
    last_error = sqlc.arg('last_error'),
    revision = enforcement.revision + 1
FROM channel_firmware_authority authority
WHERE enforcement.id = sqlc.arg('enforcement_id')
  AND enforcement.authority_id = authority.id
  AND enforcement.org_id = authority.org_id
  AND authority.halted_at IS NOT NULL
  AND enforcement.state IN ('pending', 'held')
  AND enforcement.attempt_count = 0
  AND enforcement.command_batch_uuid IS NULL;

-- name: GetBetweenChannelCompletionCounts :one
SELECT COUNT(*)::bigint AS total_members,
       COUNT(*) FILTER (WHERE member.state = 'succeeded')::bigint AS succeeded_members,
       COUNT(*) FILTER (
           WHERE member.state IN (
               'succeeded',
               'failed',
               'attention_required',
               'cancelled'
           )
       )::bigint AS terminal_forward_members,
       COUNT(*) FILTER (
           WHERE member.revert_selected_at IS NOT NULL
       )::bigint AS revert_members,
       COUNT(*) FILTER (WHERE member.state = 'reverted')::bigint AS reverted_members
FROM firmware_rollout_member member
WHERE member.rollout_id = sqlc.arg('rollout_id')
  AND member.org_id = sqlc.arg('org_id');

-- name: AdvanceRolloutLaneCurrentChannel :one
UPDATE rollout_lane
SET current_channel_id = sqlc.arg('target_channel_id'),
    revision = revision + 1
WHERE id = sqlc.arg('lane_id')
  AND org_id = sqlc.arg('org_id')
  AND current_channel_id = sqlc.arg('expected_channel_id')
RETURNING *;

-- name: ListActiveRolloutOwnedDeviceIdentifiers :many
SELECT device.device_identifier
FROM firmware_rollout_member member
JOIN device
  ON device.id = member.device_id
 AND device.org_id = member.org_id
WHERE member.org_id = sqlc.arg('org_id')
  AND member.owner_released_at IS NULL
  AND device.device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
ORDER BY device.device_identifier;

-- name: GetRolloutLaneMembershipChangeByIdempotencyKey :one
SELECT *
FROM rollout_lane_membership_change
WHERE org_id = sqlc.arg('org_id')
  AND idempotency_key = sqlc.arg('idempotency_key');

-- name: RemoveRolloutLaneMembershipDevices :many
DELETE FROM device_set_membership membership
USING rollout_lane_channel attachment
WHERE membership.device_set_id = attachment.channel_id
  AND membership.org_id = attachment.org_id
  AND membership.device_set_type = 'channel'
  AND attachment.org_id = sqlc.arg('org_id')
  AND attachment.lane_id = ANY(sqlc.arg('lane_ids')::uuid[])
  AND membership.device_id = ANY(sqlc.arg('device_ids')::bigint[])
RETURNING membership.device_id;

-- name: AddRolloutLaneMembershipDevices :many
INSERT INTO device_set_membership (
    org_id,
    device_set_id,
    device_set_type,
    device_id,
    device_identifier
)
SELECT device.org_id,
       lane.current_channel_id,
       'channel',
       device.id,
       device.device_identifier
FROM rollout_lane lane
JOIN device
  ON device.org_id = lane.org_id
 AND device.id = ANY(sqlc.arg('device_ids')::bigint[])
 AND device.deleted_at IS NULL
WHERE lane.id = sqlc.arg('lane_id')
  AND lane.org_id = sqlc.arg('org_id')
  AND lane.deleted_at IS NULL
ORDER BY device.device_identifier
RETURNING device_id;

-- name: CreateRolloutLaneMembershipEnforcements :execrows
INSERT INTO channel_firmware_enforcement (
    org_id,
    device_id,
    desired_release_set_id,
    desired_release_target_id,
    desired_firmware_file_id,
    desired_firmware_version,
    cause_type,
    cause_reference,
    authority_id,
    authority_revision,
    state,
    last_observed_firmware_version,
    firmware_observed_at,
    confirmed_at
)
SELECT device.org_id,
       device.id,
       target.release_set_id,
       target.id,
       target.firmware_file_id,
       target.firmware_version,
       'rollout_lane_membership',
       sqlc.arg('change_id')::uuid::text,
       authority.id,
       authority.revision,
       CASE
           WHEN btrim(COALESCE(discovered.firmware_version, '')) = target.firmware_version
               THEN 'confirmed'
           ELSE 'pending'
       END,
       NULLIF(btrim(COALESCE(discovered.firmware_version, '')), ''),
       CASE
           WHEN btrim(COALESCE(discovered.firmware_version, '')) <> ''
               THEN discovered.last_seen
           ELSE NULL
       END,
       CASE
           WHEN btrim(COALESCE(discovered.firmware_version, '')) = target.firmware_version
               THEN CURRENT_TIMESTAMP
           ELSE NULL
       END
FROM rollout_lane lane
JOIN device_set_channel channel
  ON channel.device_set_id = lane.current_channel_id
 AND channel.org_id = lane.org_id
JOIN device
  ON device.org_id = lane.org_id
 AND device.id = ANY(sqlc.arg('device_ids')::bigint[])
 AND device.deleted_at IS NULL
JOIN discovered_device discovered
  ON discovered.id = device.discovered_device_id
 AND discovered.org_id = device.org_id
 AND discovered.deleted_at IS NULL
JOIN firmware_release_target target
  ON target.release_set_id = channel.release_set_id
 AND target.org_id = channel.org_id
 AND lower(btrim(target.target_manufacturer)) =
     lower(btrim(COALESCE(discovered.manufacturer, '')))
 AND lower(btrim(target.target_model)) =
     lower(btrim(COALESCE(discovered.model, '')))
JOIN channel_firmware_authority authority
  ON authority.id = sqlc.arg('authority_id')
 AND authority.org_id = lane.org_id
 AND authority.revision = sqlc.arg('authority_revision')
 AND authority.halted_at IS NULL
WHERE lane.id = sqlc.arg('lane_id')
  AND lane.org_id = sqlc.arg('org_id');

-- name: BumpRolloutLaneMembershipRevisions :many
UPDATE rollout_lane
SET revision = revision + 1
WHERE org_id = sqlc.arg('org_id')
  AND id = ANY(sqlc.arg('lane_ids')::uuid[])
  AND deleted_at IS NULL
RETURNING id, revision;

-- name: CreateRolloutLaneMembershipChange :one
INSERT INTO rollout_lane_membership_change (
    id,
    org_id,
    target_lane_id,
    authority_id,
    idempotency_key,
    request_fingerprint,
    requested,
    applied,
    reason,
    actor_user_id,
    actor_type,
    actor_credential_id
)
VALUES (
    sqlc.arg('change_id'),
    sqlc.arg('org_id'),
    sqlc.arg('target_lane_id'),
    sqlc.narg('authority_id'),
    sqlc.arg('idempotency_key'),
    sqlc.arg('request_fingerprint'),
    sqlc.arg('requested'),
    sqlc.arg('applied'),
    sqlc.arg('reason'),
    sqlc.arg('actor_user_id'),
    sqlc.arg('actor_type'),
    sqlc.narg('actor_credential_id')
)
RETURNING *;

-- The queries below are explicit integration-test support operations. Keeping
-- setup and assertions in sqlc preserves the repository's prepared-statement
-- boundary while still allowing tests to drive otherwise unreachable states.

-- name: TestMoveDeviceChannelMembership :execrows
UPDATE device_set_membership
SET device_set_id = sqlc.arg('target_channel_id')
WHERE org_id = sqlc.arg('org_id')
  AND device_identifier = sqlc.arg('device_identifier')
  AND device_set_type = 'channel';

-- name: TestSetRolloutLaneMembershipEnforcementState :execrows
UPDATE channel_firmware_enforcement enforcement
SET state = sqlc.arg('state'),
    attention_required_at = CASE
        WHEN sqlc.arg('state') = 'attention_required' THEN CURRENT_TIMESTAMP
        ELSE enforcement.attention_required_at
    END,
    confirmed_at = CASE
        WHEN sqlc.arg('state') = 'confirmed' THEN CURRENT_TIMESTAMP
        ELSE NULL
    END,
    last_error = sqlc.arg('last_error'),
    revision = enforcement.revision + 1
FROM channel_firmware_authority authority
WHERE authority.id = enforcement.authority_id
  AND authority.org_id = enforcement.org_id
  AND authority.org_id = sqlc.arg('org_id')
  AND authority.authority_type = sqlc.arg('authority_type')
  AND (
      sqlc.arg('authority_reference')::text = ''
      OR authority.authority_reference = sqlc.arg('authority_reference')
  )
  AND (
      sqlc.arg('current_state')::text = ''
      OR enforcement.state = sqlc.arg('current_state')
  );

-- name: GetRolloutLaneMembershipChangeTestState :one
SELECT CASE WHEN authority_id IS NULL THEN false ELSE true END AS has_authority,
       applied::text AS applied,
       actor_type,
       actor_user_id,
       COALESCE(actor_credential_id, '') AS actor_credential_id,
       reason,
       COALESCE(authority_id::text, '') AS authority_id
FROM rollout_lane_membership_change
WHERE org_id = sqlc.arg('org_id')
  AND idempotency_key = sqlc.arg('idempotency_key');

-- name: TestMutateRolloutLaneMembershipChangeReason :execrows
UPDATE rollout_lane_membership_change
SET reason = sqlc.arg('reason')
WHERE org_id = sqlc.arg('org_id')
  AND idempotency_key = sqlc.arg('idempotency_key');

-- name: TestDeleteRolloutLaneMembershipChange :execrows
DELETE FROM rollout_lane_membership_change
WHERE org_id = sqlc.arg('org_id')
  AND idempotency_key = sqlc.arg('idempotency_key');

-- name: TestTruncateRolloutLaneMembershipChanges :exec
TRUNCATE TABLE rollout_lane_membership_change;

-- name: TestSetDiscoveredFirmwareVersion :execrows
UPDATE discovered_device discovered
SET firmware_version = NULLIF(sqlc.arg('firmware_version')::text, ''),
    last_seen = CURRENT_TIMESTAMP
FROM device
WHERE device.discovered_device_id = discovered.id
  AND device.org_id = discovered.org_id
  AND device.org_id = sqlc.arg('org_id')
  AND device.device_identifier = sqlc.arg('device_identifier');

-- name: TestGetMembershipEnforcementState :one
SELECT enforcement.state
FROM channel_firmware_enforcement enforcement
JOIN channel_firmware_authority authority
  ON authority.id = enforcement.authority_id
 AND authority.org_id = enforcement.org_id
JOIN device
  ON device.id = enforcement.device_id
 AND device.org_id = enforcement.org_id
WHERE enforcement.org_id = sqlc.arg('org_id')
  AND authority.authority_type = 'rollout_lane_membership'
  AND authority.authority_reference = sqlc.arg('authority_reference')
  AND device.device_identifier = sqlc.arg('device_identifier');

-- name: TestLockChannelFirmwareAuthorityTable :exec
LOCK TABLE channel_firmware_authority IN ACCESS EXCLUSIVE MODE;

-- name: TestLockDeviceSetMembershipTable :exec
LOCK TABLE device_set_membership IN ACCESS EXCLUSIVE MODE;

-- name: TestLockRolloutLaneNowait :one
SELECT id
FROM rollout_lane
WHERE id = sqlc.arg('lane_id')
  AND org_id = sqlc.arg('org_id')
FOR UPDATE NOWAIT;

-- name: TestLockMembershipDeviceObservationNowait :one
SELECT device.id
FROM device
JOIN discovered_device discovered
  ON discovered.id = device.discovered_device_id
 AND discovered.org_id = device.org_id
WHERE device.org_id = sqlc.arg('org_id')
  AND device.device_identifier = sqlc.arg('device_identifier')
FOR UPDATE OF device, discovered NOWAIT;

-- name: CountDeviceChannelMembershipsForTest :one
SELECT COUNT(*)::bigint
FROM device_set_membership
WHERE org_id = sqlc.arg('org_id')
  AND device_identifier = sqlc.arg('device_identifier')
  AND device_set_type = 'channel';

-- name: GetRolloutLaneMembershipMutationCountsForTest :one
SELECT
    (SELECT COUNT(*) FROM device_set_membership membership
     WHERE membership.org_id = sqlc.arg('org_id') AND membership.device_set_type = 'channel')::bigint AS memberships,
    (SELECT COUNT(*) FROM channel_firmware_authority authority
     WHERE authority.org_id = sqlc.arg('org_id'))::bigint AS authorities,
    (SELECT COUNT(*) FROM channel_firmware_enforcement enforcement
     WHERE enforcement.org_id = sqlc.arg('org_id'))::bigint AS enforcements,
    (SELECT COUNT(*) FROM rollout_lane_membership_change membership_change
     WHERE membership_change.org_id = sqlc.arg('org_id'))::bigint AS changes;

-- name: GetDiscoveredFirmwareVersionForTest :one
SELECT discovered.firmware_version
FROM device
JOIN discovered_device discovered
  ON discovered.id = device.discovered_device_id
 AND discovered.org_id = device.org_id
WHERE device.org_id = sqlc.arg('org_id')
  AND device.device_identifier = sqlc.arg('device_identifier');

-- name: TestSoftDeleteDeviceByIdentifier :execrows
UPDATE device
SET deleted_at = CURRENT_TIMESTAMP
WHERE org_id = sqlc.arg('org_id')
  AND device_identifier = sqlc.arg('device_identifier');

-- name: RunRolloutLaneTopologyBackfill :exec
SELECT backfill_rollout_lane_model_topology(sqlc.arg('org_id'));

-- name: GetRolloutLaneTopologyCutover :one
SELECT *
FROM rollout_lane_topology_cutover
WHERE org_id = sqlc.arg('org_id');

-- name: LockRolloutLaneTopologyCutover :one
SELECT *
FROM rollout_lane_topology_cutover
WHERE org_id = sqlc.arg('org_id')
FOR UPDATE;

-- name: ListRolloutLaneTopologyAnomalies :many
SELECT anomaly_id,
       lane_id,
       device_id,
       device_identifier,
       lane_model_id,
       lane_model_revision,
       anomaly_type,
       supported_repair_actions,
       details
FROM rollout_lane_topology_anomaly
WHERE org_id = sqlc.arg('org_id')
ORDER BY lane_id, device_identifier, anomaly_type, anomaly_id;

-- name: CountRolloutLaneTopologyAnomalies :one
SELECT COUNT(*)::bigint
FROM rollout_lane_topology_anomaly
WHERE org_id = sqlc.arg('org_id');

-- name: CountActiveLegacyRolloutLaneWork :one
SELECT COUNT(DISTINCT rollout.id)::bigint
FROM rollout_lane_channel attachment
JOIN firmware_rollout rollout
  ON rollout.id = attachment.rollout_id
 AND rollout.org_id = attachment.org_id
WHERE attachment.org_id = sqlc.arg('org_id')
  AND (
      rollout.state NOT IN (
          'completed',
          'completed_with_failures',
          'aborted',
          'reverted'
      )
      OR EXISTS (
          SELECT 1
          FROM firmware_rollout_member member
          WHERE member.rollout_id = rollout.id
            AND member.org_id = rollout.org_id
            AND member.owner_released_at IS NULL
      )
      OR EXISTS (
          SELECT 1
          FROM firmware_rollout_control control
          WHERE control.rollout_id = rollout.id
            AND control.org_id = rollout.org_id
            AND control.status = 'started'
      )
      OR EXISTS (
          SELECT 1
          FROM channel_firmware_authority authority
          WHERE authority.org_id = rollout.org_id
            AND authority.id IN (
                rollout.forward_authority_id,
                rollout.revert_authority_id
            )
            AND authority.halted_at IS NULL
      )
      OR EXISTS (
          SELECT 1
          FROM channel_firmware_enforcement enforcement
          WHERE enforcement.org_id = rollout.org_id
            AND enforcement.authority_id IN (
                rollout.forward_authority_id,
                rollout.revert_authority_id
            )
            AND enforcement.state IN (
                'pending',
                'held',
                'dispatching',
                'dispatched',
                'verifying'
            )
      )
      OR EXISTS (
          SELECT 1
          FROM firmware_rollout_batch batch
          WHERE batch.rollout_id = rollout.id
            AND batch.org_id = rollout.org_id
            AND batch.completed_at IS NOT NULL
            AND NOT batch.post_window_finalized
      )
  );

-- name: GetRolloutLaneTopologyAdminOperationByKey :one
SELECT *
FROM rollout_lane_topology_admin_operation
WHERE org_id = sqlc.arg('org_id')
  AND idempotency_key = sqlc.arg('idempotency_key');

-- name: LockRolloutLaneModelForRepair :one
SELECT *
FROM rollout_lane_model
WHERE id = sqlc.arg('lane_model_id')
  AND lane_id = sqlc.arg('lane_id')
  AND org_id = sqlc.arg('org_id')
FOR UPDATE;

-- name: LockRolloutLaneModelRepairDevice :one
SELECT device.id AS device_id,
       membership.device_set_id AS channel_id,
       rollout_model_identity_v1(
           discovered.manufacturer,
           discovered.model
       ) AS model_identity_key,
       discovered.model_identity_observed_at
FROM device
JOIN discovered_device discovered
  ON discovered.id = device.discovered_device_id
 AND discovered.org_id = device.org_id
 AND discovered.deleted_at IS NULL
JOIN device_set_membership membership
  ON membership.device_id = device.id
 AND membership.org_id = device.org_id
 AND membership.device_set_type = 'channel'
WHERE device.org_id = sqlc.arg('org_id')
  AND device.device_identifier = sqlc.arg('device_identifier')
  AND device.deleted_at IS NULL
FOR UPDATE OF device, discovered, membership;

-- name: EndActiveRolloutLaneModelBinding :execrows
UPDATE rollout_lane_model_binding
SET ended_at = CURRENT_TIMESTAMP,
    ended_by_operation_id = sqlc.arg('operation_id'),
    revision = revision + 1
WHERE lane_id = sqlc.arg('lane_id')
  AND org_id = sqlc.arg('org_id')
  AND device_id = sqlc.arg('device_id')
  AND ended_at IS NULL;

-- name: CreateRolloutLaneModelBindingRepair :one
INSERT INTO rollout_lane_model_binding (
    id,
    lane_id,
    lane_model_id,
    org_id,
    device_id,
    channel_id,
    model_identity_key,
    model_identity_observed_at,
    origin
)
VALUES (
    sqlc.arg('binding_id'),
    sqlc.arg('lane_id'),
    sqlc.arg('lane_model_id'),
    sqlc.arg('org_id'),
    sqlc.arg('device_id'),
    sqlc.arg('channel_id'),
    sqlc.arg('model_identity_key'),
    sqlc.narg('model_identity_observed_at'),
    'repair'
)
RETURNING *;

-- name: BumpRolloutLaneModelRevision :one
UPDATE rollout_lane_model
SET revision = revision + 1
WHERE id = sqlc.arg('lane_model_id')
  AND lane_id = sqlc.arg('lane_id')
  AND org_id = sqlc.arg('org_id')
  AND revision = sqlc.arg('expected_revision')
RETURNING revision;

-- name: CreateRolloutLaneTopologyAdminOperation :one
INSERT INTO rollout_lane_topology_admin_operation (
    id,
    org_id,
    operation,
    lane_id,
    lane_model_id,
    device_id,
    idempotency_key,
    request_fingerprint,
    expected_revision,
    resulting_revision,
    reason,
    requested,
    applied,
    actor_user_id,
    actor_type,
    actor_credential_id
)
VALUES (
    sqlc.arg('operation_id'),
    sqlc.arg('org_id'),
    sqlc.arg('operation'),
    sqlc.narg('lane_id'),
    sqlc.narg('lane_model_id'),
    sqlc.narg('device_id'),
    sqlc.arg('idempotency_key'),
    sqlc.arg('request_fingerprint'),
    sqlc.arg('expected_revision'),
    sqlc.arg('resulting_revision'),
    sqlc.arg('reason'),
    sqlc.arg('requested'),
    sqlc.arg('applied'),
    sqlc.arg('actor_user_id'),
    sqlc.arg('actor_type'),
    sqlc.narg('actor_credential_id')
)
RETURNING *;

-- name: EnableRolloutLaneModelTopology :one
UPDATE rollout_lane_topology_cutover
SET enabled = TRUE,
    revision = revision + 1,
    enabled_at = CURRENT_TIMESTAMP,
    enabled_by_user_id = sqlc.arg('enabled_by_user_id'),
    enabled_actor_type = sqlc.arg('enabled_actor_type'),
    enabled_actor_credential_id = sqlc.narg('enabled_actor_credential_id'),
    enable_reason = sqlc.arg('enable_reason'),
    enable_idempotency_key = sqlc.arg('enable_idempotency_key')
WHERE org_id = sqlc.arg('org_id')
  AND revision = sqlc.arg('expected_revision')
  AND NOT enabled
RETURNING *;

-- name: LockRolloutLaneModelForMutation :one
SELECT *
FROM rollout_lane_model
WHERE lane_id = sqlc.arg('lane_id')
  AND org_id = sqlc.arg('org_id')
  AND (
      (sqlc.narg('lane_model_id')::uuid IS NOT NULL
          AND id = sqlc.narg('lane_model_id')::uuid)
      OR
      (sqlc.narg('model_identity_key')::text IS NOT NULL
          AND model_identity_key = sqlc.narg('model_identity_key')::text)
  )
FOR UPDATE;

-- name: LockRolloutLaneModelsForMutation :many
SELECT *
FROM rollout_lane_model
WHERE org_id = sqlc.arg('org_id')
  AND id = ANY(sqlc.arg('lane_model_ids')::uuid[])
ORDER BY model_identity_key, id
FOR UPDATE;

-- name: CountActiveRolloutLaneModelBindings :one
SELECT COUNT(*)::bigint
FROM rollout_lane_model_binding
WHERE lane_model_id = sqlc.arg('lane_model_id')
  AND lane_id = sqlc.arg('lane_id')
  AND org_id = sqlc.arg('org_id')
  AND ended_at IS NULL;

-- name: ListActiveRolloutLaneModelBindingsForDevices :many
SELECT binding.id,
       binding.lane_id,
       binding.lane_model_id,
       binding.device_id,
       binding.channel_id,
       binding.model_identity_key,
       declaration.revision AS lane_model_revision,
       declaration.manufacturer,
       declaration.model,
       lane.label AS lane_label,
       device.device_identifier,
       membership.device_set_id AS physical_channel_id
FROM rollout_lane_model_binding binding
JOIN rollout_lane_model declaration
  ON declaration.id = binding.lane_model_id
 AND declaration.lane_id = binding.lane_id
 AND declaration.org_id = binding.org_id
JOIN rollout_lane lane
  ON lane.id = binding.lane_id
 AND lane.org_id = binding.org_id
 AND lane.deleted_at IS NULL
JOIN device
  ON device.id = binding.device_id
 AND device.org_id = binding.org_id
 AND device.deleted_at IS NULL
LEFT JOIN device_set_membership membership
  ON membership.device_id = binding.device_id
 AND membership.org_id = binding.org_id
 AND membership.device_set_type = 'channel'
WHERE binding.org_id = sqlc.arg('org_id')
  AND binding.device_id = ANY(sqlc.arg('device_ids')::bigint[])
  AND binding.ended_at IS NULL
ORDER BY binding.lane_model_id, device.device_identifier;

-- name: CreateRolloutLaneModelDeclaration :one
INSERT INTO rollout_lane_model (
    id,
    lane_id,
    org_id,
    model_identity_key,
    manufacturer,
    model,
    current_channel_id,
    current_release_set_id,
    current_release_target_id,
    origin
)
VALUES (
    sqlc.arg('lane_model_id'),
    sqlc.arg('lane_id'),
    sqlc.arg('org_id'),
    sqlc.arg('model_identity_key'),
    sqlc.arg('manufacturer'),
    sqlc.arg('model'),
    sqlc.arg('current_channel_id'),
    sqlc.arg('current_release_set_id'),
    sqlc.arg('current_release_target_id'),
    'topology'
)
RETURNING *;

-- name: GetRolloutLaneReleaseTargetByModel :one
SELECT *
FROM firmware_release_target
WHERE release_set_id = sqlc.arg('release_set_id')
  AND org_id = sqlc.arg('org_id')
  AND rollout_model_identity_v1(target_manufacturer, target_model)
      = sqlc.arg('model_identity_key');

-- name: CreateRolloutLaneModelChannel :one
INSERT INTO rollout_lane_model_channel (
    lane_model_id,
    lane_id,
    org_id,
    channel_id,
    release_set_id,
    release_target_id,
    position,
    origin
)
VALUES (
    sqlc.arg('lane_model_id'),
    sqlc.arg('lane_id'),
    sqlc.arg('org_id'),
    sqlc.arg('channel_id'),
    sqlc.arg('release_set_id'),
    sqlc.arg('release_target_id'),
    (
        SELECT COALESCE(MAX(history.position) + 1, 0)
        FROM rollout_lane_model_channel history
        WHERE history.lane_model_id = sqlc.arg('lane_model_id')
    ),
    'topology'
)
RETURNING *;

-- name: AddRolloutLaneModelMembershipDevices :many
INSERT INTO device_set_membership (
    org_id,
    device_set_id,
    device_set_type,
    device_id,
    device_identifier
)
SELECT device.org_id,
       sqlc.arg('channel_id'),
       'channel',
       device.id,
       device.device_identifier
FROM device
WHERE device.org_id = sqlc.arg('org_id')
  AND device.id = ANY(sqlc.arg('device_ids')::bigint[])
  AND device.deleted_at IS NULL
ORDER BY device.device_identifier
RETURNING device_id;

-- name: RemoveRolloutLaneModelMembershipDevices :many
DELETE FROM device_set_membership
WHERE org_id = sqlc.arg('org_id')
  AND device_set_id = sqlc.arg('channel_id')
  AND device_set_type = 'channel'
  AND device_id = ANY(sqlc.arg('device_ids')::bigint[])
RETURNING device_id;

-- name: CreateRolloutLaneModelBindings :many
INSERT INTO rollout_lane_model_binding (
    id,
    lane_id,
    lane_model_id,
    org_id,
    device_id,
    channel_id,
    model_identity_key,
    model_identity_observed_at,
    origin
)
SELECT gen_random_uuid(),
       sqlc.arg('lane_id'),
       sqlc.arg('lane_model_id'),
       device.org_id,
       device.id,
       sqlc.arg('channel_id'),
       rollout_model_identity_v1(discovered.manufacturer, discovered.model),
       discovered.model_identity_observed_at,
       'topology'
FROM device
JOIN discovered_device discovered
  ON discovered.id = device.discovered_device_id
 AND discovered.org_id = device.org_id
 AND discovered.deleted_at IS NULL
WHERE device.org_id = sqlc.arg('org_id')
  AND device.id = ANY(sqlc.arg('device_ids')::bigint[])
  AND device.deleted_at IS NULL
  AND rollout_model_identity_v1(discovered.manufacturer, discovered.model)
      = sqlc.arg('model_identity_key')
ORDER BY device.device_identifier
RETURNING *;

-- name: EndRolloutLaneModelBindings :many
UPDATE rollout_lane_model_binding
SET ended_at = CURRENT_TIMESTAMP,
    ended_by_operation_id = sqlc.arg('operation_id'),
    revision = revision + 1
WHERE lane_id = sqlc.arg('lane_id')
  AND lane_model_id = sqlc.arg('lane_model_id')
  AND org_id = sqlc.arg('org_id')
  AND device_id = ANY(sqlc.arg('device_ids')::bigint[])
  AND ended_at IS NULL
RETURNING device_id;

-- name: AdvanceRolloutLaneModelTarget :one
UPDATE rollout_lane_model
SET current_channel_id = sqlc.arg('channel_id'),
    current_release_set_id = sqlc.arg('release_set_id'),
    current_release_target_id = sqlc.arg('release_target_id'),
    revision = revision + 1
WHERE id = sqlc.arg('lane_model_id')
  AND lane_id = sqlc.arg('lane_id')
  AND org_id = sqlc.arg('org_id')
  AND revision = sqlc.arg('expected_revision')
RETURNING *;

-- name: BumpRolloutLaneModelRevisions :many
UPDATE rollout_lane_model
SET revision = revision + 1
WHERE org_id = sqlc.arg('org_id')
  AND id = ANY(sqlc.arg('lane_model_ids')::uuid[])
RETURNING id, revision;

-- name: HasActiveRolloutLaneModelWork :one
SELECT EXISTS (
    SELECT 1
    FROM rollout_lane_model_binding binding
    JOIN channel_firmware_enforcement enforcement
      ON enforcement.org_id = binding.org_id
     AND enforcement.device_id = binding.device_id
     AND enforcement.state IN ('pending', 'held', 'dispatching', 'dispatched', 'verifying')
    WHERE binding.lane_model_id = sqlc.arg('lane_model_id')
      AND binding.lane_id = sqlc.arg('lane_id')
      AND binding.org_id = sqlc.arg('org_id')
      AND binding.ended_at IS NULL
)
OR EXISTS (
    SELECT 1
    FROM firmware_rollout_group_model grouped
    JOIN firmware_rollout child
      ON child.id = grouped.child_rollout_id
     AND child.org_id = grouped.org_id
    WHERE grouped.lane_model_id = sqlc.arg('lane_model_id')
      AND grouped.lane_id = sqlc.arg('lane_id')
      AND grouped.org_id = sqlc.arg('org_id')
      AND (
          child.state IN (
              'created',
              'running',
              'paused',
              'review',
              'reverting',
              'completed_with_failures'
          )
          OR EXISTS (
              SELECT 1
              FROM firmware_rollout_member member
              WHERE member.rollout_id = child.id
                AND member.org_id = child.org_id
                AND (
                    member.owner_released_at IS NULL
                    OR member.state IN ('pending', 'admitted', 'reverting')
                )
          )
      )
);

-- name: CreateRolloutLaneModelEnforcements :execrows
INSERT INTO channel_firmware_enforcement (
    org_id,
    device_id,
    desired_release_set_id,
    desired_release_target_id,
    desired_firmware_file_id,
    desired_firmware_version,
    cause_type,
    cause_reference,
    authority_id,
    authority_revision,
    state,
    last_observed_firmware_version,
    firmware_observed_at,
    confirmed_at
)
SELECT device.org_id,
       device.id,
       declaration.current_release_set_id,
       target.id,
       target.firmware_file_id,
       target.firmware_version,
       sqlc.arg('cause_type'),
       sqlc.arg('cause_reference'),
       authority.id,
       authority.revision,
       CASE
           WHEN btrim(COALESCE(discovered.firmware_version, '')) = target.firmware_version
               THEN 'confirmed'
           ELSE 'pending'
       END,
       NULLIF(btrim(COALESCE(discovered.firmware_version, '')), ''),
       CASE WHEN btrim(COALESCE(discovered.firmware_version, '')) <> ''
           THEN discovered.last_seen ELSE NULL END,
       CASE WHEN btrim(COALESCE(discovered.firmware_version, '')) = target.firmware_version
           THEN CURRENT_TIMESTAMP ELSE NULL END
FROM rollout_lane_model declaration
JOIN firmware_release_target target
  ON target.id = declaration.current_release_target_id
 AND target.release_set_id = declaration.current_release_set_id
 AND target.org_id = declaration.org_id
JOIN device
  ON device.org_id = declaration.org_id
 AND device.id = ANY(sqlc.arg('device_ids')::bigint[])
 AND device.deleted_at IS NULL
JOIN discovered_device discovered
  ON discovered.id = device.discovered_device_id
 AND discovered.org_id = device.org_id
 AND discovered.deleted_at IS NULL
JOIN channel_firmware_authority authority
  ON authority.id = sqlc.arg('authority_id')
 AND authority.org_id = declaration.org_id
 AND authority.revision = sqlc.arg('authority_revision')
 AND authority.halted_at IS NULL
WHERE declaration.id = sqlc.arg('lane_model_id')
  AND declaration.lane_id = sqlc.arg('lane_id')
  AND declaration.org_id = sqlc.arg('org_id')
  AND rollout_model_identity_v1(discovered.manufacturer, discovered.model)
      = declaration.model_identity_key
ON CONFLICT (authority_id, device_id) DO NOTHING;

-- name: GetRolloutLaneTopologyCountsForTest :one
SELECT
    (SELECT COUNT(*)
     FROM rollout_lane_model model
     WHERE model.lane_id = sqlc.arg('lane_id')
       AND model.org_id = sqlc.arg('org_id'))::bigint AS declarations,
    (SELECT COUNT(*)
     FROM rollout_lane_model_channel history
     WHERE history.lane_id = sqlc.arg('lane_id')
       AND history.org_id = sqlc.arg('org_id'))::bigint AS history_rows,
    (SELECT COUNT(DISTINCT history.channel_id)
     FROM rollout_lane_model_channel history
     WHERE history.lane_id = sqlc.arg('lane_id')
       AND history.org_id = sqlc.arg('org_id'))::bigint AS history_channels,
    (SELECT COUNT(*)
     FROM rollout_lane_model_binding binding
     WHERE binding.lane_id = sqlc.arg('lane_id')
       AND binding.org_id = sqlc.arg('org_id')
       AND binding.ended_at IS NULL)::bigint AS active_bindings;

-- name: GetRolloutLaneModelForTest :one
SELECT *
FROM rollout_lane_model
WHERE lane_id = sqlc.arg('lane_id')
  AND org_id = sqlc.arg('org_id')
  AND model_identity_key = rollout_model_identity_v1(
      sqlc.arg('manufacturer'),
      sqlc.arg('model')
  );

-- name: GetFirmwareRolloutGroupByStartKey :one
SELECT *
FROM firmware_rollout_group
WHERE lane_id = sqlc.arg('lane_id')
  AND org_id = sqlc.arg('org_id')
  AND idempotency_key = sqlc.arg('idempotency_key');

-- name: CreateFirmwareRolloutGroup :one
INSERT INTO firmware_rollout_group (
    id,
    lane_id,
    org_id,
    name,
    idempotency_key,
    create_fingerprint,
    reason,
    created_by_user_id,
    actor_type,
    actor_credential_id
)
VALUES (
    sqlc.arg('group_id'),
    sqlc.arg('lane_id'),
    sqlc.arg('org_id'),
    sqlc.arg('name'),
    sqlc.arg('idempotency_key'),
    sqlc.arg('create_fingerprint'),
    sqlc.arg('reason'),
    sqlc.arg('created_by_user_id'),
    sqlc.arg('actor_type'),
    sqlc.narg('actor_credential_id')
)
RETURNING *;

-- name: ClaimRolloutLaneActiveParent :one
INSERT INTO rollout_lane_active_parent (
    lane_id,
    org_id,
    group_id,
    claim_idempotency_key,
    claim_fingerprint
)
VALUES (
    sqlc.arg('lane_id'),
    sqlc.arg('org_id'),
    sqlc.arg('group_id'),
    sqlc.arg('claim_idempotency_key'),
    sqlc.arg('claim_fingerprint')
)
RETURNING *;

-- name: GetRolloutLaneActiveParent :one
SELECT *
FROM rollout_lane_active_parent
WHERE lane_id = sqlc.arg('lane_id')
  AND org_id = sqlc.arg('org_id');

-- name: ListRolloutLaneActiveParents :many
SELECT *
FROM rollout_lane_active_parent
WHERE org_id = sqlc.arg('org_id')
ORDER BY lane_id, group_id;

-- name: LockRolloutLaneActiveParent :one
SELECT *
FROM rollout_lane_active_parent
WHERE lane_id = sqlc.arg('lane_id')
  AND org_id = sqlc.arg('org_id')
FOR UPDATE;

-- name: GetRolloutLaneSettlementState :one
WITH lane_rollouts AS (
    SELECT DISTINCT child.id,
                    child.org_id,
                    child.state,
                    child.forward_authority_id,
                    child.revert_authority_id
    FROM firmware_rollout child
    LEFT JOIN rollout_lane_channel attachment
      ON attachment.rollout_id = child.id
     AND attachment.org_id = child.org_id
    WHERE child.org_id = sqlc.arg('org_id')
      AND (
          sqlc.narg('group_id')::uuid IS NULL
          OR child.group_id = sqlc.narg('group_id')::uuid
      )
      AND (
          child.lane_id = sqlc.arg('lane_id')
          OR attachment.lane_id = sqlc.arg('lane_id')
      )
),
lane_members AS (
    SELECT member.*
    FROM firmware_rollout_member member
    JOIN lane_rollouts child
      ON child.id = member.rollout_id
     AND child.org_id = member.org_id
),
lane_controls AS (
    SELECT control.*
    FROM firmware_rollout_control control
    JOIN lane_rollouts child
      ON child.id = control.rollout_id
     AND child.org_id = control.org_id
)
SELECT
    EXISTS (
        SELECT 1
        FROM lane_rollouts child
        WHERE child.state NOT IN (
            'aborted',
            'completed',
            'completed_with_failures',
            'reverted'
        )
    ) AS child_unsettled,
    EXISTS (
        SELECT 1
        FROM lane_members member
        WHERE member.owner_released_at IS NULL
    ) AS owner_unsettled,
    EXISTS (
        SELECT 1
        FROM lane_controls control
        WHERE control.status = 'started'
    ) AS control_unsettled,
    EXISTS (
        SELECT 1
        FROM channel_firmware_authority authority
        JOIN lane_rollouts child
          ON authority.org_id = child.org_id
         AND authority.id IN (
             child.forward_authority_id,
             child.revert_authority_id
         )
        WHERE authority.halted_at IS NULL
    ) AS authority_unsettled,
    EXISTS (
        SELECT 1
        FROM channel_firmware_enforcement enforcement
        JOIN lane_rollouts child
          ON enforcement.org_id = child.org_id
         AND enforcement.authority_id IN (
             child.forward_authority_id,
             child.revert_authority_id
         )
        WHERE enforcement.state IN (
            'pending',
            'held',
            'dispatching',
            'dispatched',
            'verifying'
        )
    ) AS enforcement_unsettled,
    EXISTS (
        SELECT 1
        FROM lane_members member
        WHERE member.state IN ('admitted', 'reverting')
    ) AS finalization_unsettled,
    EXISTS (
        SELECT 1
        FROM lane_rollouts child
        WHERE child.state = 'reverting'
    ) AS revert_unsettled,
    EXISTS (
        SELECT 1
        FROM firmware_rollout_batch batch
        JOIN lane_rollouts child
          ON child.id = batch.rollout_id
         AND child.org_id = batch.org_id
        WHERE batch.completed_at IS NOT NULL
          AND NOT batch.post_window_finalized
        UNION ALL
        SELECT 1
        FROM firmware_rollout_evidence evidence
        JOIN lane_rollouts child
          ON child.id = evidence.rollout_id
         AND child.org_id = evidence.org_id
        WHERE evidence.status = 'open'
    ) AS evidence_unsettled;

-- name: ReleaseRolloutLaneActiveParent :execrows
DELETE FROM rollout_lane_active_parent
WHERE lane_id = sqlc.arg('lane_id')
  AND org_id = sqlc.arg('org_id')
  AND group_id = sqlc.arg('group_id');

-- name: CreateFirmwareRolloutGroupModel :one
INSERT INTO firmware_rollout_group_model (
    group_id,
    lane_id,
    lane_model_id,
    org_id,
    model_identity_key,
    source_channel_id,
    source_release_set_id,
    source_release_target_id,
    target_channel_id,
    target_release_set_id,
    target_release_target_id,
    snapshot
)
VALUES (
    sqlc.arg('group_id'),
    sqlc.arg('lane_id'),
    sqlc.arg('lane_model_id'),
    sqlc.arg('org_id'),
    sqlc.arg('model_identity_key'),
    sqlc.arg('source_channel_id'),
    sqlc.arg('source_release_set_id'),
    sqlc.arg('source_release_target_id'),
    sqlc.arg('target_channel_id'),
    sqlc.arg('target_release_set_id'),
    sqlc.arg('target_release_target_id'),
    sqlc.arg('snapshot')
)
RETURNING *;

-- name: AttachFirmwareRolloutGroupModelChild :one
UPDATE firmware_rollout_group_model
SET child_rollout_id = sqlc.arg('child_rollout_id')
WHERE group_id = sqlc.arg('group_id')
  AND lane_model_id = sqlc.arg('lane_model_id')
  AND org_id = sqlc.arg('org_id')
  AND child_rollout_id IS NULL
RETURNING *;

-- name: AdvanceRolloutLaneModelCurrentTarget :one
UPDATE rollout_lane_model
SET current_channel_id = sqlc.arg('target_channel_id'),
    current_release_set_id = sqlc.arg('target_release_set_id'),
    current_release_target_id = sqlc.arg('target_release_target_id'),
    revision = revision + 1
WHERE id = sqlc.arg('lane_model_id')
  AND lane_id = sqlc.arg('lane_id')
  AND org_id = sqlc.arg('org_id')
  AND current_channel_id = sqlc.arg('expected_channel_id')
  AND revision = sqlc.arg('expected_revision')
RETURNING *;

-- name: TestCreateRolloutLaneModelChannel :one
INSERT INTO rollout_lane_model_channel (
    lane_model_id,
    lane_id,
    org_id,
    channel_id,
    release_set_id,
    release_target_id,
    position,
    origin
)
VALUES (
    sqlc.arg('lane_model_id'),
    sqlc.arg('lane_id'),
    sqlc.arg('org_id'),
    sqlc.arg('channel_id'),
    sqlc.arg('release_set_id'),
    sqlc.arg('release_target_id'),
    (
        SELECT COALESCE(MAX(history.position) + 1, 0)
        FROM rollout_lane_model_channel history
        WHERE history.lane_model_id = sqlc.arg('lane_model_id')
    ),
    'topology'
)
RETURNING *;

-- name: TestSetRolloutLaneModelCurrentChannel :one
UPDATE rollout_lane_model
SET current_channel_id = sqlc.arg('channel_id'),
    current_release_set_id = sqlc.arg('release_set_id'),
    current_release_target_id = sqlc.arg('release_target_id'),
    revision = revision + 1
WHERE id = sqlc.arg('lane_model_id')
  AND lane_id = sqlc.arg('lane_id')
  AND org_id = sqlc.arg('org_id')
RETURNING *;

-- name: TestCreateFirmwareRolloutGroup :one
INSERT INTO firmware_rollout_group (
    id,
    lane_id,
    org_id,
    name,
    idempotency_key,
    create_fingerprint,
    reason,
    created_by_user_id,
    actor_type
)
VALUES (
    sqlc.arg('group_id'),
    sqlc.arg('lane_id'),
    sqlc.arg('org_id'),
    sqlc.arg('name'),
    sqlc.arg('idempotency_key'),
    sqlc.arg('create_fingerprint'),
    sqlc.arg('reason'),
    sqlc.arg('created_by_user_id'),
    'user'
)
RETURNING *;

-- name: TestClaimRolloutLaneActiveParent :one
INSERT INTO rollout_lane_active_parent (
    lane_id,
    org_id,
    group_id,
    claim_idempotency_key,
    claim_fingerprint
)
VALUES (
    sqlc.arg('lane_id'),
    sqlc.arg('org_id'),
    sqlc.arg('group_id'),
    sqlc.arg('claim_idempotency_key'),
    sqlc.arg('claim_fingerprint')
)
RETURNING *;

-- name: GetRolloutLaneActiveParentForTest :one
SELECT claim.lane_id,
       claim.org_id,
       claim.group_id,
       parent.lane_id AS parent_lane_id,
       parent.org_id AS parent_org_id
FROM rollout_lane_active_parent claim
JOIN firmware_rollout_group parent
  ON parent.id = claim.group_id
 AND parent.lane_id = claim.lane_id
 AND parent.org_id = claim.org_id
WHERE claim.lane_id = sqlc.arg('lane_id')
  AND claim.org_id = sqlc.arg('org_id');

-- name: CountRolloutLaneActiveParentsForTest :one
SELECT COUNT(*)::bigint
FROM rollout_lane_active_parent
WHERE lane_id = sqlc.arg('lane_id')
  AND org_id = sqlc.arg('org_id');

-- name: TestUpdateFirmwareRolloutGroupMetadata :one
UPDATE firmware_rollout_group
SET name = sqlc.arg('name')
WHERE id = sqlc.arg('group_id')
  AND org_id = sqlc.arg('org_id')
RETURNING *;

-- name: TestUpdateFirmwareRolloutGroupResult :one
UPDATE firmware_rollout_group
SET terminal_outcome = sqlc.narg('terminal_outcome'),
    result_ready = sqlc.arg('result_ready')
WHERE id = sqlc.arg('group_id')
  AND org_id = sqlc.arg('org_id')
RETURNING *;
