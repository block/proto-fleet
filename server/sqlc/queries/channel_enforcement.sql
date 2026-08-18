-- name: CreateChannelFirmwareAuthority :one
INSERT INTO channel_firmware_authority (
    id,
    org_id,
    authority_type,
    authority_reference,
    created_by_user_id
)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, org_id, authority_type, authority_reference, revision,
          halted_at, created_by_user_id, created_at, updated_at;

-- name: AdvanceChannelFirmwareAuthorityRevision :one
UPDATE channel_firmware_authority
SET revision = revision + 1
WHERE id = sqlc.arg('authority_id')
  AND org_id = sqlc.arg('org_id')
  AND revision = sqlc.arg('expected_revision')
  AND halted_at IS NULL
RETURNING id, org_id, authority_type, authority_reference, revision,
          halted_at, created_by_user_id, created_at, updated_at;

-- name: HaltChannelFirmwareAuthority :one
UPDATE channel_firmware_authority
SET revision = revision + 1,
    halted_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg('authority_id')
  AND org_id = sqlc.arg('org_id')
  AND revision = sqlc.arg('expected_revision')
  AND halted_at IS NULL
RETURNING id, org_id, authority_type, authority_reference, revision,
          halted_at, created_by_user_id, created_at, updated_at;

-- name: CreateChannelFirmwareEnforcement :one
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
SELECT
    authority.org_id,
    device.id,
    target.release_set_id,
    target.id,
    target.firmware_file_id,
    target.firmware_version,
    sqlc.arg('cause_type'),
    sqlc.narg('cause_reference'),
    authority.id,
    authority.revision
FROM channel_firmware_authority authority
JOIN device
  ON device.id = sqlc.arg('device_id')
 AND device.org_id = authority.org_id
 AND device.deleted_at IS NULL
JOIN firmware_release_target target
  ON target.id = sqlc.arg('release_target_id')
 AND target.org_id = authority.org_id
WHERE authority.id = sqlc.arg('authority_id')
  AND authority.org_id = sqlc.arg('org_id')
  AND authority.revision = sqlc.arg('authority_revision')
  AND authority.halted_at IS NULL
RETURNING id, org_id, device_id, desired_release_set_id,
          desired_release_target_id, desired_firmware_file_id,
          desired_firmware_version, cause_type, cause_reference,
          authority_id, authority_revision, state, attempt_count,
          command_batch_uuid, revision, desired_at, held_at, claimed_at,
          enqueued_at, command_completed_at, next_reconcile_at,
          last_observed_firmware_version, firmware_observed_at,
          last_observed_hashrate_hs, hashing_observed_at, confirmed_at,
          attention_required_at, last_error, created_at, updated_at;

-- name: GetChannelFirmwareEnforcement :one
SELECT enforcement.id,
       enforcement.org_id,
       enforcement.device_id,
       device.device_identifier,
       enforcement.desired_release_set_id,
       enforcement.desired_release_target_id,
       enforcement.desired_firmware_file_id,
       enforcement.desired_firmware_version,
       enforcement.cause_type,
       enforcement.cause_reference,
       enforcement.authority_id,
       enforcement.authority_revision,
       enforcement.state,
       enforcement.attempt_count,
       enforcement.command_batch_uuid,
       enforcement.revision,
       enforcement.desired_at,
       enforcement.held_at,
       enforcement.claimed_at,
       enforcement.enqueued_at,
       enforcement.command_completed_at,
       enforcement.next_reconcile_at,
       enforcement.last_observed_firmware_version,
       enforcement.firmware_observed_at,
       enforcement.last_observed_hashrate_hs,
       enforcement.hashing_observed_at,
       enforcement.confirmed_at,
       enforcement.attention_required_at,
       enforcement.last_error,
       enforcement.created_at,
       enforcement.updated_at,
       authority.created_by_user_id
FROM channel_firmware_enforcement enforcement
JOIN device
  ON device.id = enforcement.device_id
 AND device.org_id = enforcement.org_id
JOIN channel_firmware_authority authority
  ON authority.id = enforcement.authority_id
 AND authority.org_id = enforcement.org_id
WHERE enforcement.id = $1;

-- name: ListChannelFirmwareEnforcementsForReconcile :many
SELECT enforcement.id,
       enforcement.org_id,
       enforcement.device_id,
       device.device_identifier,
       enforcement.desired_release_set_id,
       enforcement.desired_release_target_id,
       enforcement.desired_firmware_file_id,
       enforcement.desired_firmware_version,
       enforcement.cause_type,
       enforcement.cause_reference,
       enforcement.authority_id,
       enforcement.authority_revision,
       enforcement.state,
       enforcement.attempt_count,
       enforcement.command_batch_uuid,
       enforcement.revision,
       enforcement.desired_at,
       enforcement.held_at,
       enforcement.claimed_at,
       enforcement.enqueued_at,
       enforcement.command_completed_at,
       enforcement.next_reconcile_at,
       enforcement.last_observed_firmware_version,
       enforcement.firmware_observed_at,
       enforcement.last_observed_hashrate_hs,
       enforcement.hashing_observed_at,
       enforcement.confirmed_at,
       enforcement.attention_required_at,
       enforcement.last_error,
       enforcement.created_at,
       enforcement.updated_at,
       authority.created_by_user_id
FROM channel_firmware_enforcement enforcement
JOIN device
  ON device.id = enforcement.device_id
 AND device.org_id = enforcement.org_id
JOIN channel_firmware_authority authority
  ON authority.id = enforcement.authority_id
 AND authority.org_id = enforcement.org_id
WHERE enforcement.state IN (
    'pending',
    'held',
    'dispatching',
    'dispatched',
    'verifying'
)
  AND enforcement.next_reconcile_at <= CURRENT_TIMESTAMP
ORDER BY enforcement.next_reconcile_at, enforcement.updated_at, enforcement.id
LIMIT sqlc.arg('reconcile_limit');

-- name: ListChannelManagedDeviceIdentifiers :many
SELECT membership.device_identifier
FROM device_set_membership membership
WHERE membership.org_id = sqlc.arg('org_id')
  AND membership.device_set_type = 'channel'
  AND membership.device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
ORDER BY membership.device_identifier;

-- name: ClaimChannelFirmwareEnforcement :execrows
-- Authority is locked before the enforcement row. Halt/revision updates lock
-- the same authority row, so only a claim committed before the control change
-- can proceed.
WITH locked_authority AS MATERIALIZED (
    SELECT source.id, source.org_id, source.revision
    FROM channel_firmware_authority source
    WHERE source.id = sqlc.arg('authority_id')
      AND source.org_id = sqlc.arg('org_id')
      AND source.revision = sqlc.arg('authority_revision')
      AND source.halted_at IS NULL
    FOR UPDATE
)
UPDATE channel_firmware_enforcement enforcement
SET state = 'dispatching',
    command_batch_uuid = sqlc.arg('command_batch_uuid'),
    claimed_at = sqlc.arg('claimed_at'),
    held_at = NULL,
    last_error = NULL,
    revision = enforcement.revision + 1
FROM locked_authority authority
WHERE enforcement.id = sqlc.arg('enforcement_id')
  AND enforcement.org_id = authority.org_id
  AND enforcement.authority_id = authority.id
  AND enforcement.authority_revision = authority.revision
  AND enforcement.revision = sqlc.arg('expected_revision')
  AND enforcement.state IN ('pending', 'held')
  AND enforcement.attempt_count = 0
  AND enforcement.command_batch_uuid IS NULL;

-- name: HoldChannelFirmwareEnforcement :execrows
UPDATE channel_firmware_enforcement
SET state = 'held',
    command_batch_uuid = NULL,
    claimed_at = NULL,
    held_at = sqlc.arg('held_at'),
    last_error = sqlc.arg('reason'),
    revision = revision + 1
WHERE id = sqlc.arg('enforcement_id')
  AND revision = sqlc.arg('expected_revision')
  AND state = 'dispatching'
  AND attempt_count = 0
  AND command_batch_uuid = sqlc.arg('expected_batch_uuid');

-- name: ReturnChannelFirmwareEnforcementPending :execrows
UPDATE channel_firmware_enforcement
SET state = 'pending',
    command_batch_uuid = NULL,
    claimed_at = NULL,
    last_error = sqlc.arg('reason'),
    revision = revision + 1
WHERE id = sqlc.arg('enforcement_id')
  AND revision = sqlc.arg('expected_revision')
  AND state = 'dispatching'
  AND attempt_count = 0
  AND command_batch_uuid = sqlc.arg('expected_batch_uuid');

-- name: MarkChannelFirmwareEnforcementDispatched :execrows
UPDATE channel_firmware_enforcement
SET state = 'dispatched',
    attempt_count = 1,
    enqueued_at = sqlc.arg('enqueued_at'),
    last_error = NULL,
    revision = revision + 1
WHERE id = sqlc.arg('enforcement_id')
  AND revision = sqlc.arg('expected_revision')
  AND state = 'dispatching'
  AND attempt_count = 0
  AND command_batch_uuid = sqlc.arg('expected_batch_uuid');

-- name: GetChannelFirmwareCommandOutcome :one
SELECT status, error_info, updated_at
FROM queue_message
WHERE command_batch_log_uuid = sqlc.arg('batch_uuid')
  AND device_id = sqlc.arg('device_id')
ORDER BY id DESC
LIMIT 1;

-- name: MarkChannelFirmwareEnforcementVerifying :execrows
UPDATE channel_firmware_enforcement
SET state = 'verifying',
    command_completed_at = sqlc.arg('command_completed_at'),
    next_reconcile_at = CURRENT_TIMESTAMP,
    last_error = NULL,
    revision = revision + 1
WHERE id = sqlc.arg('enforcement_id')
  AND revision = sqlc.arg('expected_revision')
  AND state = 'dispatched'
  AND attempt_count = 1
  AND command_batch_uuid = sqlc.arg('expected_batch_uuid');

-- name: RecordChannelFirmwareObservation :execrows
UPDATE channel_firmware_enforcement
SET last_observed_firmware_version = CASE
        WHEN sqlc.narg('observation_error')::text IS NOT NULL
            THEN last_observed_firmware_version
        ELSE sqlc.arg('firmware_version')
    END,
    firmware_observed_at = CASE
        WHEN sqlc.narg('observation_error')::text IS NOT NULL
            THEN firmware_observed_at
        ELSE sqlc.arg('firmware_observed_at')
    END,
    last_observed_hashrate_hs = CASE
        WHEN sqlc.narg('observation_error')::text IS NOT NULL
            THEN last_observed_hashrate_hs
        ELSE sqlc.narg('hashrate_hs')
    END,
    hashing_observed_at = CASE
        WHEN sqlc.narg('observation_error')::text IS NOT NULL
            THEN hashing_observed_at
        ELSE sqlc.narg('hashing_observed_at')
    END,
    last_error = sqlc.narg('observation_error')::text,
    next_reconcile_at = sqlc.arg('next_reconcile_at'),
    revision = revision + 1
WHERE id = sqlc.arg('enforcement_id')
  AND revision = sqlc.arg('expected_revision')
  AND state = 'verifying'
  AND command_batch_uuid = sqlc.arg('expected_batch_uuid');

-- name: ConfirmChannelFirmwareEnforcement :execrows
UPDATE channel_firmware_enforcement
SET state = 'confirmed',
    last_observed_firmware_version = sqlc.arg('firmware_version'),
    firmware_observed_at = sqlc.arg('observed_at'),
    last_observed_hashrate_hs = sqlc.arg('hashrate_hs'),
    hashing_observed_at = sqlc.arg('observed_at'),
    confirmed_at = sqlc.arg('confirmed_at'),
    last_error = NULL,
    revision = revision + 1
WHERE id = sqlc.arg('enforcement_id')
  AND revision = sqlc.arg('expected_revision')
  AND state = 'verifying'
  AND command_batch_uuid = sqlc.arg('expected_batch_uuid')
  AND sqlc.arg('observed_at') > command_completed_at;

-- name: MarkChannelFirmwareEnforcementAttentionRequired :execrows
UPDATE channel_firmware_enforcement
SET state = 'attention_required',
    attention_required_at = sqlc.arg('attention_required_at'),
    last_error = sqlc.arg('reason'),
    revision = revision + 1
WHERE id = sqlc.arg('enforcement_id')
  AND revision = sqlc.arg('expected_revision')
  AND state = sqlc.arg('expected_state')
  AND command_batch_uuid = sqlc.arg('expected_batch_uuid');
