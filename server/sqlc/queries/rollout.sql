-- name: CreateFirmwareRollout :one
INSERT INTO firmware_rollout (
    id,
    org_id,
    name,
    strategy_key,
    forward_authority_id,
    forward_authority_revision,
    source_channel_id,
    target_channel_id,
    source_release_set_id,
    target_release_set_id,
    source_snapshot,
    target_snapshot,
    revert_snapshot,
    idempotency_key,
    create_fingerprint,
    reason,
    created_by_user_id
)
VALUES (
    sqlc.arg('rollout_id'),
    sqlc.arg('org_id'),
    sqlc.arg('name'),
    sqlc.arg('strategy_key'),
    sqlc.arg('forward_authority_id'),
    sqlc.arg('forward_authority_revision'),
    sqlc.narg('source_channel_id'),
    sqlc.narg('target_channel_id'),
    sqlc.narg('source_release_set_id'),
    sqlc.narg('target_release_set_id'),
    sqlc.arg('source_snapshot'),
    sqlc.arg('target_snapshot'),
    sqlc.arg('revert_snapshot'),
    sqlc.arg('idempotency_key'),
    sqlc.arg('create_fingerprint'),
    sqlc.arg('reason'),
    sqlc.arg('created_by_user_id')
)
RETURNING *;

-- name: GetFirmwareRolloutByIdempotencyKey :one
SELECT *
FROM firmware_rollout
WHERE org_id = sqlc.arg('org_id')
  AND idempotency_key = sqlc.arg('idempotency_key');

-- name: CreateFirmwareRolloutBatches :many
INSERT INTO firmware_rollout_batch (
    rollout_id,
    org_id,
    position,
    label
)
SELECT
    sqlc.arg('rollout_id'),
    sqlc.arg('org_id'),
    (input.value->>'position')::int,
    input.value->>'label'
FROM jsonb_array_elements(sqlc.arg('batches')::jsonb) AS input(value)
ORDER BY (input.value->>'position')::int
RETURNING *;

-- name: CreateFirmwareRolloutMembers :many
INSERT INTO firmware_rollout_member (
    rollout_id,
    batch_id,
    org_id,
    device_id,
    position,
    source_snapshot,
    target_snapshot,
    revert_snapshot
)
SELECT
    sqlc.arg('rollout_id'),
    batch.id,
    device.org_id,
    device.id,
    (input.value->>'position')::int,
    input.value->'source_snapshot',
    input.value->'target_snapshot',
    input.value->'revert_snapshot'
FROM jsonb_array_elements(sqlc.arg('members')::jsonb) AS input(value)
JOIN firmware_rollout_batch batch
  ON batch.rollout_id = sqlc.arg('rollout_id')
 AND batch.org_id = sqlc.arg('org_id')
 AND batch.position = (input.value->>'batch_position')::int
JOIN device
  ON device.device_identifier = input.value->>'device_identifier'
 AND device.org_id = sqlc.arg('org_id')
 AND device.deleted_at IS NULL
ORDER BY (input.value->>'position')::int
RETURNING *;

-- name: CreateFirmwareRolloutCause :one
INSERT INTO firmware_rollout_cause (
    rollout_id,
    member_id,
    control_id,
    org_id,
    operation,
    reason,
    actor_user_id,
    actor_type,
    actor_credential_id,
    from_state,
    to_state,
    rollout_revision
)
VALUES (
    sqlc.arg('rollout_id'),
    sqlc.narg('member_id'),
    sqlc.narg('control_id'),
    sqlc.arg('org_id'),
    sqlc.arg('operation'),
    sqlc.arg('reason'),
    sqlc.arg('actor_user_id'),
    sqlc.arg('actor_type'),
    sqlc.narg('actor_credential_id'),
    sqlc.narg('from_state'),
    sqlc.arg('to_state'),
    sqlc.arg('rollout_revision')
)
RETURNING *;

-- name: GetFirmwareRollout :one
SELECT *
FROM firmware_rollout
WHERE id = sqlc.arg('rollout_id')
  AND org_id = sqlc.arg('org_id');

-- name: LockFirmwareRollout :one
SELECT *
FROM firmware_rollout
WHERE id = sqlc.arg('rollout_id')
  AND org_id = sqlc.arg('org_id')
FOR UPDATE;

-- name: ListFirmwareRollouts :many
SELECT *
FROM firmware_rollout
WHERE org_id = sqlc.arg('org_id')
  AND (
      cardinality(sqlc.arg('states')::text[]) = 0
      OR state = ANY(sqlc.arg('states')::text[])
  )
ORDER BY created_at DESC, id DESC;

-- name: ListFirmwareRolloutBatches :many
SELECT *
FROM firmware_rollout_batch
WHERE rollout_id = sqlc.arg('rollout_id')
  AND org_id = sqlc.arg('org_id')
ORDER BY position, id;

-- name: ListFirmwareRolloutMembers :many
SELECT member.*,
       device.device_identifier
FROM firmware_rollout_member member
JOIN device
  ON device.id = member.device_id
 AND device.org_id = member.org_id
WHERE member.rollout_id = sqlc.arg('rollout_id')
  AND member.org_id = sqlc.arg('org_id')
ORDER BY member.position, member.id;

-- name: ListFirmwareRolloutCauses :many
SELECT *
FROM firmware_rollout_cause
WHERE rollout_id = sqlc.arg('rollout_id')
  AND org_id = sqlc.arg('org_id')
ORDER BY created_at, id;

-- name: GetFirmwareRolloutControlByKey :one
SELECT *
FROM firmware_rollout_control
WHERE rollout_id = sqlc.arg('rollout_id')
  AND org_id = sqlc.arg('org_id')
  AND idempotency_key = sqlc.arg('idempotency_key');

-- name: GetFirmwareRolloutControl :one
SELECT *
FROM firmware_rollout_control
WHERE id = sqlc.arg('control_id')
  AND rollout_id = sqlc.arg('rollout_id')
  AND org_id = sqlc.arg('org_id')
FOR UPDATE;

-- name: GetFirmwareRolloutBatchForControl :one
SELECT *
FROM firmware_rollout_batch
WHERE rollout_id = sqlc.arg('rollout_id')
  AND org_id = sqlc.arg('org_id')
  AND state = 'pending'
  AND (
      sqlc.narg('batch_id')::bigint IS NULL
      OR id = sqlc.narg('batch_id')
  )
ORDER BY position, id
LIMIT 1
FOR UPDATE;

-- name: CreateFirmwareRolloutControl :one
INSERT INTO firmware_rollout_control (
    id,
    rollout_id,
    org_id,
    batch_id,
    operation,
    idempotency_key,
    request_fingerprint,
    expected_revision,
    resulting_revision,
    status,
    created_by_user_id,
    actor_type,
    actor_credential_id
)
VALUES (
    sqlc.arg('control_id'),
    sqlc.arg('rollout_id'),
    sqlc.arg('org_id'),
    sqlc.narg('batch_id'),
    sqlc.arg('operation'),
    sqlc.arg('idempotency_key'),
    sqlc.arg('request_fingerprint'),
    sqlc.arg('expected_revision'),
    sqlc.arg('resulting_revision'),
    sqlc.arg('status'),
    sqlc.arg('created_by_user_id'),
    sqlc.arg('actor_type'),
    sqlc.narg('actor_credential_id')
)
RETURNING *;

-- name: ApplyFirmwareRolloutTransition :one
UPDATE firmware_rollout
SET state = sqlc.arg('target_state'),
    resume_state = sqlc.narg('resume_state'),
    revision = revision + 1,
    forward_authority_revision = COALESCE(
        sqlc.narg('forward_authority_revision'),
        forward_authority_revision
    ),
    revert_authority_id = COALESCE(
        sqlc.narg('revert_authority_id'),
        revert_authority_id
    ),
    revert_authority_revision = COALESCE(
        sqlc.narg('revert_authority_revision'),
        revert_authority_revision
    ),
    started_at = CASE
        WHEN sqlc.arg('target_state') = 'running'
            THEN COALESCE(started_at, CURRENT_TIMESTAMP)
        ELSE started_at
    END,
    paused_at = CASE
        WHEN sqlc.arg('target_state') = 'paused'
            THEN CURRENT_TIMESTAMP
        ELSE paused_at
    END,
    aborted_at = CASE
        WHEN sqlc.arg('target_state') = 'aborted'
            THEN CURRENT_TIMESTAMP
        ELSE aborted_at
    END,
    completed_at = CASE
        WHEN sqlc.arg('target_state') IN ('completed', 'completed_with_failures')
            THEN CURRENT_TIMESTAMP
        ELSE completed_at
    END,
    reverting_at = CASE
        WHEN sqlc.arg('target_state') = 'reverting'
            THEN CURRENT_TIMESTAMP
        ELSE reverting_at
    END,
    reverted_at = CASE
        WHEN sqlc.arg('target_state') = 'reverted'
            THEN CURRENT_TIMESTAMP
        ELSE reverted_at
    END
WHERE id = sqlc.arg('rollout_id')
  AND org_id = sqlc.arg('org_id')
  AND revision = sqlc.arg('expected_revision')
RETURNING *;

-- name: AdmitFirmwareRolloutBatch :one
UPDATE firmware_rollout_batch
SET state = 'admitted',
    revision = revision + 1
WHERE id = sqlc.arg('batch_id')
  AND rollout_id = sqlc.arg('rollout_id')
  AND org_id = sqlc.arg('org_id')
  AND state = 'pending'
RETURNING *;

-- name: AdmitFirmwareRolloutMembers :execrows
UPDATE firmware_rollout_member
SET state = 'admitted',
    admitted_at = CURRENT_TIMESTAMP,
    revision = revision + 1
WHERE batch_id = sqlc.arg('batch_id')
  AND rollout_id = sqlc.arg('rollout_id')
  AND org_id = sqlc.arg('org_id')
  AND state = 'pending'
  AND owner_released_at IS NULL;

-- name: CancelPendingFirmwareRolloutBatches :execrows
UPDATE firmware_rollout_batch
SET state = 'cancelled',
    revision = revision + 1
WHERE rollout_id = sqlc.arg('rollout_id')
  AND org_id = sqlc.arg('org_id')
  AND state = 'pending';

-- name: CancelPendingFirmwareRolloutMembers :execrows
UPDATE firmware_rollout_member
SET state = 'cancelled',
    settled_at = CURRENT_TIMESTAMP,
    owner_released_at = CURRENT_TIMESTAMP,
    revision = revision + 1
WHERE rollout_id = sqlc.arg('rollout_id')
  AND org_id = sqlc.arg('org_id')
  AND state = 'pending';

-- name: CancelUnclaimedFirmwareRolloutMembers :execrows
WITH cancelled_enforcements AS (
    UPDATE channel_firmware_enforcement enforcement
    SET state = 'cancelled',
        last_error = 'rollout authority was halted before dispatch',
        revision = enforcement.revision + 1
    FROM firmware_rollout rollout
    WHERE rollout.id = sqlc.arg('rollout_id')
      AND rollout.org_id = sqlc.arg('org_id')
      AND enforcement.authority_id = rollout.forward_authority_id
      AND enforcement.org_id = rollout.org_id
      AND enforcement.state IN ('pending', 'held')
      AND enforcement.attempt_count = 0
      AND enforcement.command_batch_uuid IS NULL
    RETURNING enforcement.id
)
UPDATE firmware_rollout_member member
SET state = 'cancelled',
    settled_at = CURRENT_TIMESTAMP,
    owner_released_at = CURRENT_TIMESTAMP,
    last_error = 'rollout authority was halted before dispatch',
    revision = member.revision + 1
WHERE member.rollout_id = sqlc.arg('rollout_id')
  AND member.org_id = sqlc.arg('org_id')
  AND member.state = 'admitted'
  AND (
      member.enforcement_id IS NULL
      OR member.enforcement_id IN (
          SELECT id
          FROM cancelled_enforcements
      )
  );

-- name: ReleaseFirmwareRolloutOwners :execrows
UPDATE firmware_rollout_member
SET owner_released_at = CURRENT_TIMESTAMP,
    revision = revision + 1
WHERE rollout_id = sqlc.arg('rollout_id')
  AND org_id = sqlc.arg('org_id')
  AND owner_released_at IS NULL;

-- name: ReleaseTerminalFirmwareRolloutOwners :execrows
UPDATE firmware_rollout_member
SET owner_released_at = CURRENT_TIMESTAMP,
    revision = revision + 1
WHERE rollout_id = sqlc.arg('rollout_id')
  AND org_id = sqlc.arg('org_id')
  AND state IN (
      'succeeded',
      'failed',
      'attention_required',
      'cancelled',
      'reverted'
  )
  AND owner_released_at IS NULL;

-- name: PrepareFirmwareRolloutMembersForRevert :execrows
UPDATE firmware_rollout_member
SET state = 'reverting',
    owner_released_at = NULL,
    settled_at = NULL,
    revert_selected_at = CURRENT_TIMESTAMP,
    revision = revision + 1
WHERE rollout_id = sqlc.arg('rollout_id')
  AND org_id = sqlc.arg('org_id')
  AND state = 'succeeded';

-- name: HasFirmwareRolloutSucceededMembers :one
SELECT EXISTS (
    SELECT 1
    FROM firmware_rollout_member
    WHERE rollout_id = sqlc.arg('rollout_id')
      AND org_id = sqlc.arg('org_id')
      AND state = 'succeeded'
);

-- name: CompleteFirmwareRolloutBatches :execrows
UPDATE firmware_rollout_batch
SET state = 'completed',
    revision = revision + 1
WHERE rollout_id = sqlc.arg('rollout_id')
  AND org_id = sqlc.arg('org_id')
  AND state = 'admitted';

-- name: CompleteFirmwareRolloutRevertMembers :execrows
UPDATE firmware_rollout_member
SET state = 'reverted',
    settled_at = CURRENT_TIMESTAMP,
    owner_released_at = CURRENT_TIMESTAMP,
    revision = revision + 1
WHERE rollout_id = sqlc.arg('rollout_id')
  AND org_id = sqlc.arg('org_id')
  AND state = 'reverting';

-- name: UpdateFirmwareRolloutMember :one
UPDATE firmware_rollout_member
SET state = sqlc.arg('state'),
    enforcement_id = COALESCE(sqlc.narg('enforcement_id'), enforcement_id),
    command_batch_uuid = COALESCE(
        sqlc.narg('command_batch_uuid'),
        command_batch_uuid
    ),
    last_error = sqlc.narg('last_error'),
    settled_at = CASE
        WHEN sqlc.arg('state') IN (
            'succeeded',
            'failed',
            'attention_required',
            'cancelled',
            'reverted'
        ) THEN CURRENT_TIMESTAMP
        ELSE settled_at
    END,
    revision = revision + 1
WHERE id = sqlc.arg('member_id')
  AND rollout_id = sqlc.arg('rollout_id')
  AND org_id = sqlc.arg('org_id')
  AND revision = sqlc.arg('expected_revision')
RETURNING *;

-- name: FinishFirmwareRolloutControl :one
UPDATE firmware_rollout_control
SET status = sqlc.arg('status'),
    error_message = sqlc.narg('error_message')
WHERE id = sqlc.arg('control_id')
  AND rollout_id = sqlc.arg('rollout_id')
  AND org_id = sqlc.arg('org_id')
  AND status = 'started'
RETURNING *;

-- name: MoveFirmwareRolloutToReviewAfterControlFailure :execrows
UPDATE firmware_rollout
SET state = 'review',
    resume_state = NULL,
    revision = revision + 1
WHERE id = sqlc.arg('rollout_id')
  AND org_id = sqlc.arg('org_id')
  AND state = 'running';

-- name: ResetFirmwareRolloutAdmissionMembersAfterFailure :execrows
UPDATE firmware_rollout_member
SET state = 'pending',
    admitted_at = NULL,
    revision = revision + 1
WHERE rollout_id = sqlc.arg('rollout_id')
  AND batch_id = sqlc.arg('batch_id')
  AND org_id = sqlc.arg('org_id')
  AND state = 'admitted'
  AND enforcement_id IS NULL
  AND owner_released_at IS NULL;

-- name: ResetFirmwareRolloutAdmissionBatchAfterFailure :execrows
UPDATE firmware_rollout_batch batch
SET state = 'pending',
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
        AND member.state = 'admitted'
  );

-- name: ResetFirmwareRolloutRevertMembersAfterFailure :execrows
UPDATE firmware_rollout_member member
SET state = 'succeeded',
    settled_at = CURRENT_TIMESTAMP,
    owner_released_at = CURRENT_TIMESTAMP,
    revert_selected_at = NULL,
    revision = member.revision + 1
FROM firmware_rollout rollout
WHERE member.rollout_id = sqlc.arg('rollout_id')
  AND member.org_id = sqlc.arg('org_id')
  AND rollout.id = member.rollout_id
  AND rollout.org_id = member.org_id
  AND member.state = 'reverting'
  AND NOT EXISTS (
      SELECT 1
      FROM channel_firmware_enforcement enforcement
      WHERE enforcement.id = member.enforcement_id
        AND enforcement.org_id = member.org_id
        AND enforcement.device_id = member.device_id
        AND enforcement.authority_id = rollout.revert_authority_id
        AND enforcement.cause_type = 'between_channel_revert'
  );

-- name: CountFirmwareRolloutDurableRevertWork :one
SELECT COUNT(*)::bigint
FROM firmware_rollout_member member
JOIN firmware_rollout rollout
  ON rollout.id = member.rollout_id
 AND rollout.org_id = member.org_id
LEFT JOIN channel_firmware_enforcement enforcement
  ON enforcement.id = member.enforcement_id
 AND enforcement.org_id = member.org_id
 AND enforcement.device_id = member.device_id
WHERE member.rollout_id = sqlc.arg('rollout_id')
  AND member.org_id = sqlc.arg('org_id')
  AND (
      (
          member.revert_selected_at IS NOT NULL
          AND member.state IN (
              'reverted',
              'attention_required',
              'cancelled',
              'failed'
          )
      )
      OR (
          enforcement.authority_id = rollout.revert_authority_id
          AND enforcement.cause_type = 'between_channel_revert'
      )
  );

-- name: ResetFirmwareRolloutRevertAfterFailure :one
UPDATE firmware_rollout rollout
SET state = cause.from_state,
    reverting_at = NULL,
    revision = rollout.revision + 1
FROM firmware_rollout_cause cause
WHERE rollout.id = sqlc.arg('rollout_id')
  AND rollout.org_id = sqlc.arg('org_id')
  AND rollout.state = 'reverting'
  AND cause.rollout_id = rollout.id
  AND cause.org_id = rollout.org_id
  AND cause.control_id = sqlc.arg('control_id')
  AND cause.operation = 'revert'
  AND cause.from_state IN ('aborted', 'completed', 'completed_with_failures')
RETURNING rollout.*;
