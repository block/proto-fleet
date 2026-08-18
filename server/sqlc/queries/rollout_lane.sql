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
  AND org_id = sqlc.arg('org_id');

-- name: LockRolloutLane :one
SELECT *
FROM rollout_lane
WHERE id = sqlc.arg('lane_id')
  AND org_id = sqlc.arg('org_id')
FOR UPDATE;

-- name: ListRolloutLanes :many
SELECT *
FROM rollout_lane
WHERE org_id = sqlc.arg('org_id')
ORDER BY label, id;

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

-- name: GetNextRolloutLaneChannelPosition :one
SELECT COALESCE(MAX(position) + 1, 0)::int
FROM rollout_lane_channel
WHERE lane_id = sqlc.arg('lane_id')
  AND org_id = sqlc.arg('org_id');

-- name: GetRolloutLaneForRollout :one
SELECT lane.*
FROM rollout_lane lane
JOIN rollout_lane_channel attachment
  ON attachment.lane_id = lane.id
 AND attachment.org_id = lane.org_id
WHERE attachment.rollout_id = sqlc.arg('rollout_id')
  AND attachment.org_id = sqlc.arg('org_id');

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
SELECT id
FROM device
WHERE org_id = sqlc.arg('org_id')
  AND id = ANY(sqlc.arg('device_ids')::bigint[])
  AND deleted_at IS NULL
ORDER BY device_identifier
FOR UPDATE;

-- name: ListBetweenChannelDeviceModels :many
SELECT device.id AS device_id,
       device.device_identifier,
       COALESCE(discovered.manufacturer, '') AS manufacturer,
       COALESCE(discovered.model, '') AS model
FROM device
JOIN discovered_device discovered
  ON discovered.id = device.discovered_device_id
 AND discovered.org_id = device.org_id
 AND discovered.deleted_at IS NULL
WHERE device.org_id = sqlc.arg('org_id')
  AND device.device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND device.deleted_at IS NULL
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
    authority_revision
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
       authority.revision
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
       member.device_id,
       device.device_identifier,
       member.state AS member_state,
       member.revision AS member_revision,
       enforcement.id AS enforcement_id,
       enforcement.state AS enforcement_state,
       enforcement.authority_id,
       enforcement.last_error,
       rollout.forward_authority_id,
       rollout.revert_authority_id,
       rollout.source_channel_id,
       rollout.target_channel_id,
       lane.id AS lane_id,
       lane.current_channel_id
FROM firmware_rollout_member member
JOIN firmware_rollout rollout
  ON rollout.id = member.rollout_id
 AND rollout.org_id = member.org_id
JOIN device
  ON device.id = member.device_id
 AND device.org_id = member.org_id
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
       member.device_id,
       device.device_identifier,
       member.state AS member_state,
       member.revision AS member_revision,
       enforcement.id AS enforcement_id,
       enforcement.state AS enforcement_state,
       enforcement.authority_id,
       enforcement.last_error,
       rollout.forward_authority_id,
       rollout.revert_authority_id,
       rollout.source_channel_id,
       rollout.target_channel_id,
       lane.id AS lane_id,
       lane.current_channel_id
FROM firmware_rollout_member member
JOIN firmware_rollout rollout
  ON rollout.id = member.rollout_id
 AND rollout.org_id = member.org_id
JOIN device
  ON device.id = member.device_id
 AND device.org_id = member.org_id
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
