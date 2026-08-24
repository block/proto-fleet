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
    group_id,
    lane_id,
    lane_model_id,
    model_identity_key,
    model_identity_validated_at,
    source_release_target_id,
    target_release_target_id,
    source_snapshot,
    target_snapshot,
    revert_snapshot,
    hashrate_policy_max_drop_basis_points,
    hashrate_policy_healthy_duration_seconds,
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
    sqlc.narg('group_id'),
    sqlc.narg('lane_id'),
    sqlc.narg('lane_model_id'),
    sqlc.narg('model_identity_key'),
    sqlc.narg('model_identity_validated_at'),
    sqlc.narg('source_release_target_id'),
    sqlc.narg('target_release_target_id'),
    sqlc.arg('source_snapshot'),
    sqlc.arg('target_snapshot'),
    sqlc.arg('revert_snapshot'),
    sqlc.narg('hashrate_policy_max_drop_basis_points'),
    sqlc.narg('hashrate_policy_healthy_duration_seconds'),
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
    model_identity_key,
    model_identity_validated_at,
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
    NULLIF(input.value->>'model_identity_key', ''),
    NULLIF(input.value->>'model_identity_validated_at', '')::timestamptz,
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

-- name: HasCurrentFirmwareRolloutAdmissionFailure :one
SELECT EXISTS (
    SELECT 1
    FROM firmware_rollout_control failed_control
    WHERE failed_control.rollout_id = sqlc.arg('rollout_id')
      AND failed_control.org_id = sqlc.arg('org_id')
      AND failed_control.operation IN ('admit', 'continue')
      AND failed_control.status = 'failed'
      AND NOT EXISTS (
          SELECT 1
          FROM firmware_rollout_control later_control
          WHERE later_control.rollout_id = failed_control.rollout_id
            AND later_control.org_id = failed_control.org_id
            AND later_control.operation IN ('admit', 'continue')
            AND later_control.created_at > failed_control.created_at
      )
);

-- name: GetFirmwareRolloutGroup :one
SELECT *
FROM firmware_rollout_group
WHERE id = sqlc.arg('group_id')
  AND org_id = sqlc.arg('org_id');

-- name: ListFirmwareRolloutGroupChildren :many
SELECT child.*
FROM firmware_rollout child
JOIN firmware_rollout_group_model model
  ON model.child_rollout_id = child.id
 AND model.group_id = child.group_id
 AND model.org_id = child.org_id
WHERE child.group_id = sqlc.arg('group_id')
  AND child.org_id = sqlc.arg('org_id')
ORDER BY model.model_identity_key, child.id;

-- name: ListFirmwareRolloutGroupModels :many
SELECT *
FROM firmware_rollout_group_model
WHERE group_id = sqlc.arg('group_id')
  AND org_id = sqlc.arg('org_id')
ORDER BY model_identity_key, lane_model_id;

-- name: ListFirmwareRolloutGroups :many
SELECT *
FROM firmware_rollout_group
WHERE org_id = sqlc.arg('org_id')
ORDER BY created_at DESC, id DESC;

-- name: ListFirmwareRolloutGroupModelsByGroupIDs :many
SELECT *
FROM firmware_rollout_group_model
WHERE org_id = sqlc.arg('org_id')
  AND group_id = ANY(sqlc.arg('group_ids')::uuid[])
ORDER BY group_id, model_identity_key, lane_model_id;

-- name: ListFirmwareRolloutGroupChildrenByGroupIDs :many
SELECT child.*
FROM firmware_rollout child
JOIN firmware_rollout_group_model model
  ON model.child_rollout_id = child.id
 AND model.group_id = child.group_id
 AND model.org_id = child.org_id
WHERE child.org_id = sqlc.arg('org_id')
  AND child.group_id = ANY(sqlc.arg('group_ids')::uuid[])
ORDER BY child.group_id, model.model_identity_key, child.id;

-- name: ListFirmwareRolloutBatchesByRolloutIDs :many
SELECT *
FROM firmware_rollout_batch
WHERE org_id = sqlc.arg('org_id')
  AND rollout_id = ANY(sqlc.arg('rollout_ids')::uuid[])
ORDER BY rollout_id, position, id;

-- name: ListFirmwareRolloutMembersByRolloutIDs :many
SELECT member.*,
       device.device_identifier
FROM firmware_rollout_member member
JOIN device
  ON device.id = member.device_id
 AND device.org_id = member.org_id
WHERE member.org_id = sqlc.arg('org_id')
  AND member.rollout_id = ANY(sqlc.arg('rollout_ids')::uuid[])
ORDER BY member.rollout_id, member.position, member.id;

-- name: ListFirmwareRolloutEvidenceByRolloutIDs :many
SELECT *
FROM firmware_rollout_evidence
WHERE org_id = sqlc.arg('org_id')
  AND rollout_id = ANY(sqlc.arg('rollout_ids')::uuid[])
ORDER BY rollout_id, member_id, phase;

-- name: ListFirmwareRolloutCausesByRolloutIDs :many
SELECT *
FROM firmware_rollout_cause
WHERE org_id = sqlc.arg('org_id')
  AND rollout_id = ANY(sqlc.arg('rollout_ids')::uuid[])
ORDER BY rollout_id, created_at, id;

-- name: ListCurrentFirmwareRolloutAdmissionFailures :many
SELECT rollout.id AS rollout_id,
       EXISTS (
           SELECT 1
           FROM firmware_rollout_control failed_control
           WHERE failed_control.rollout_id = rollout.id
             AND failed_control.org_id = rollout.org_id
             AND failed_control.operation IN ('admit', 'continue')
             AND failed_control.status = 'failed'
             AND NOT EXISTS (
                 SELECT 1
                 FROM firmware_rollout_control later_control
                 WHERE later_control.rollout_id = failed_control.rollout_id
                   AND later_control.org_id = failed_control.org_id
                   AND later_control.operation IN ('admit', 'continue')
                   AND later_control.created_at > failed_control.created_at
             )
       ) AS failed_admission
FROM firmware_rollout rollout
WHERE rollout.org_id = sqlc.arg('org_id')
  AND rollout.id = ANY(sqlc.arg('rollout_ids')::uuid[])
ORDER BY rollout.id;

-- name: RefreshFirmwareRolloutGroupResult :execrows
WITH child_projection AS (
    SELECT parent.id AS group_id,
           COUNT(child.id)::bigint AS child_count,
           COUNT(child.id) FILTER (
               WHERE child.state NOT IN (
                   'aborted',
                   'completed',
                   'completed_with_failures',
                   'reverted'
               )
           )::bigint AS nonterminal_count,
           COUNT(DISTINCT child.state) FILTER (
               WHERE child.state IN (
                   'aborted',
                   'completed',
                   'completed_with_failures',
                   'reverted'
               )
           )::bigint AS terminal_outcome_count,
           MIN(
               CASE child.state
                   WHEN 'completed' THEN 'successful'
                   ELSE child.state
               END
           ) FILTER (
               WHERE child.state IN (
                   'aborted',
                   'completed',
                   'completed_with_failures',
                   'reverted'
               )
           ) AS uniform_terminal_outcome,
           BOOL_AND(
               NOT EXISTS (
                   SELECT 1
                   FROM firmware_rollout_batch batch
                   WHERE batch.rollout_id = child.id
                     AND batch.org_id = child.org_id
                     AND NOT batch.post_window_finalized
               )
           ) AS evidence_ready
    FROM firmware_rollout_group parent
    LEFT JOIN firmware_rollout child
      ON child.group_id = parent.id
     AND child.org_id = parent.org_id
    WHERE parent.id = sqlc.arg('group_id')
      AND parent.org_id = sqlc.arg('org_id')
    GROUP BY parent.id
),
next_result AS (
    SELECT group_id,
           CASE
               WHEN child_count = 0 OR nonterminal_count > 0 THEN NULL
               WHEN terminal_outcome_count = 1 THEN uniform_terminal_outcome
               ELSE 'mixed'
           END AS terminal_outcome,
           child_count > 0
               AND nonterminal_count = 0
               AND evidence_ready
               AND NOT EXISTS (
                   SELECT 1
                   FROM rollout_lane_active_parent claim
                   WHERE claim.group_id = child_projection.group_id
                     AND claim.org_id = sqlc.arg('org_id')
               ) AS result_ready
    FROM child_projection
)
UPDATE firmware_rollout_group parent
SET terminal_outcome = next_result.terminal_outcome,
    result_ready = next_result.result_ready
FROM next_result
WHERE parent.id = next_result.group_id
  AND parent.org_id = sqlc.arg('org_id')
  AND (
      parent.terminal_outcome IS DISTINCT FROM next_result.terminal_outcome
      OR parent.result_ready IS DISTINCT FROM next_result.result_ready
  );

-- name: RefreshFirmwareRolloutGroupResults :execrows
WITH child_projection AS (
    SELECT parent.id AS group_id,
           COUNT(child.id)::bigint AS child_count,
           COUNT(child.id) FILTER (
               WHERE child.state NOT IN ('aborted', 'completed', 'completed_with_failures', 'reverted')
           )::bigint AS nonterminal_count,
           COUNT(DISTINCT child.state) FILTER (
               WHERE child.state IN ('aborted', 'completed', 'completed_with_failures', 'reverted')
           )::bigint AS terminal_outcome_count,
           MIN(
               CASE child.state
                   WHEN 'completed' THEN 'successful'
                   ELSE child.state
               END
           ) FILTER (
               WHERE child.state IN ('aborted', 'completed', 'completed_with_failures', 'reverted')
           ) AS uniform_terminal_outcome,
           BOOL_AND(
               NOT EXISTS (
                   SELECT 1
                   FROM firmware_rollout_batch batch
                   WHERE batch.rollout_id = child.id
                     AND batch.org_id = child.org_id
                     AND NOT batch.post_window_finalized
               )
           ) AS evidence_ready
    FROM firmware_rollout_group parent
    LEFT JOIN firmware_rollout child
      ON child.group_id = parent.id
     AND child.org_id = parent.org_id
    WHERE parent.id = ANY(sqlc.arg('group_ids')::uuid[])
      AND parent.org_id = sqlc.arg('org_id')
    GROUP BY parent.id
),
next_result AS (
    SELECT group_id,
           CASE
               WHEN child_count = 0 OR nonterminal_count > 0 THEN NULL
               WHEN terminal_outcome_count = 1 THEN uniform_terminal_outcome
               ELSE 'mixed'
           END AS terminal_outcome,
           child_count > 0
               AND nonterminal_count = 0
               AND evidence_ready
               AND NOT EXISTS (
                   SELECT 1
                   FROM rollout_lane_active_parent claim
                   WHERE claim.group_id = child_projection.group_id
                     AND claim.org_id = sqlc.arg('org_id')
               ) AS result_ready
    FROM child_projection
)
UPDATE firmware_rollout_group parent
SET terminal_outcome = next_result.terminal_outcome,
    result_ready = next_result.result_ready
FROM next_result
WHERE parent.id = next_result.group_id
  AND parent.org_id = sqlc.arg('org_id')
  AND (
      parent.terminal_outcome IS DISTINCT FROM next_result.terminal_outcome
      OR parent.result_ready IS DISTINCT FROM next_result.result_ready
  );

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
  AND group_id IS NULL
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
    admission_attempt,
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
    sqlc.narg('admission_attempt'),
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

-- name: PrepareModelFirmwareRolloutMembersForRevert :execrows
UPDATE firmware_rollout_member member
SET state = 'reverting',
    owner_released_at = NULL,
    settled_at = NULL,
    revert_selected_at = CURRENT_TIMESTAMP,
    revision = member.revision + 1
FROM firmware_rollout child,
     rollout_lane_model_binding binding,
     device_set_membership membership
WHERE member.rollout_id = sqlc.arg('rollout_id')
  AND member.org_id = sqlc.arg('org_id')
  AND member.state = 'succeeded'
  AND child.id = member.rollout_id
  AND child.org_id = member.org_id
  AND child.lane_id IS NOT NULL
  AND child.lane_model_id IS NOT NULL
  AND child.target_channel_id IS NOT NULL
  AND binding.lane_id = child.lane_id
  AND binding.lane_model_id = child.lane_model_id
  AND binding.org_id = child.org_id
  AND binding.device_id = member.device_id
  AND binding.channel_id = child.target_channel_id
  AND binding.ended_at IS NULL
  AND membership.org_id = binding.org_id
  AND membership.device_id = binding.device_id
  AND membership.device_set_type = 'channel'
  AND membership.device_set_id = binding.channel_id;

-- name: CountFirmwareRolloutSucceededMembers :one
SELECT COUNT(*)::bigint
FROM firmware_rollout_member
WHERE rollout_id = sqlc.arg('rollout_id')
  AND org_id = sqlc.arg('org_id')
  AND state = 'succeeded';

-- name: ListFirmwareRolloutMemberDeviceIDs :many
SELECT device_id
FROM firmware_rollout_member
WHERE rollout_id = sqlc.arg('rollout_id')
  AND org_id = sqlc.arg('org_id')
ORDER BY device_id;

-- name: HasNewerOrConflictingRolloutLaneModelWork :one
SELECT EXISTS (
    SELECT 1
    FROM firmware_rollout other
    WHERE other.lane_id = sqlc.arg('lane_id')
      AND other.lane_model_id = sqlc.arg('lane_model_id')
      AND other.org_id = sqlc.arg('org_id')
      AND other.id <> sqlc.arg('rollout_id')
      AND (
          other.created_at > sqlc.arg('rollout_created_at')
          OR other.state NOT IN (
              'aborted',
              'completed',
              'completed_with_failures',
              'reverted'
          )
          OR EXISTS (
              SELECT 1
              FROM firmware_rollout_member other_member
              WHERE other_member.rollout_id = other.id
                AND other_member.org_id = other.org_id
                AND other_member.owner_released_at IS NULL
          )
      )
);

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
    completed_at = CURRENT_TIMESTAMP,
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

-- name: ResetFirmwareRolloutAdmissionAfterFailure :one
UPDATE firmware_rollout rollout
SET state = cause.from_state,
    resume_state = NULL,
    started_at = CASE
        WHEN cause.from_state = 'created' THEN NULL
        ELSE rollout.started_at
    END,
    revision = rollout.revision + 1
FROM firmware_rollout_cause cause
WHERE rollout.id = sqlc.arg('rollout_id')
  AND rollout.org_id = sqlc.arg('org_id')
  AND cause.rollout_id = rollout.id
  AND cause.org_id = rollout.org_id
  AND cause.control_id = sqlc.arg('control_id')
  AND cause.operation IN ('admit', 'continue')
  AND cause.from_state IN ('created', 'review')
RETURNING rollout.*;

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
    admission_attempt = batch.admission_attempt + 1,
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
