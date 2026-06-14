-- name: CreateFirmwareRollout :one
INSERT INTO firmware_rollout (
    rollout_uuid,
    org_id,
    name,
    firmware_file_id,
    state,
    batch_size,
    batch_interval_sec,
    scope_type,
    scope_jsonb,
    created_by
) VALUES (
    $1,
    $2,
    $3,
    $4,
    'draft',
    $5,
    $6,
    $7,
    $8,
    $9
)
RETURNING *;

-- name: GetFirmwareRolloutByUUID :one
SELECT *
FROM firmware_rollout
WHERE rollout_uuid = $1
  AND org_id = $2;

-- name: ListFirmwareRolloutsByOrg :many
SELECT *
FROM firmware_rollout
WHERE org_id = $1
  AND (
    sqlc.narg('cursor_created_at')::timestamptz IS NULL
    OR (created_at, id) < (sqlc.narg('cursor_created_at')::timestamptz, sqlc.narg('cursor_id')::bigint)
  )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('page_size');

-- name: StartFirmwareRollout :one
UPDATE firmware_rollout
SET state = 'running',
    target_count = $3,
    started_at = CURRENT_TIMESTAMP
WHERE id = $1
  AND org_id = $2
  AND state = 'draft'
RETURNING *;

-- name: PauseFirmwareRollout :one
UPDATE firmware_rollout
SET state = 'paused'
WHERE rollout_uuid = $1
  AND org_id = $2
  AND state = 'running'
RETURNING *;

-- name: ResumeFirmwareRollout :one
UPDATE firmware_rollout
SET state = 'running'
WHERE rollout_uuid = $1
  AND org_id = $2
  AND state = 'paused'
RETURNING *;

-- name: ReopenFirmwareRolloutForRetry :one
UPDATE firmware_rollout
SET state = 'running',
    ended_at = NULL
WHERE id = $1
  AND org_id = $2
  AND state IN ('paused', 'completed_with_failures')
RETURNING *;

-- name: CancelFirmwareRollout :one
UPDATE firmware_rollout
SET state = 'canceled',
    ended_at = COALESCE(ended_at, CURRENT_TIMESTAMP)
WHERE rollout_uuid = $1
  AND org_id = $2
  AND state IN ('draft', 'running', 'paused')
RETURNING *;

-- name: MarkFirmwareRolloutTerminal :one
UPDATE firmware_rollout
SET state = $3,
    ended_at = COALESCE(ended_at, CURRENT_TIMESTAMP)
WHERE id = $1
  AND org_id = $2
  AND state = 'running'
RETURNING *;

-- name: TouchFirmwareRolloutBatchDispatch :exec
UPDATE firmware_rollout
SET last_batch_at = CURRENT_TIMESTAMP
WHERE id = $1;

-- name: InsertFirmwareRolloutTarget :exec
INSERT INTO firmware_rollout_target (
    rollout_id,
    device_identifier,
    state
) VALUES (
    $1,
    $2,
    'pending'
);

-- name: BulkCancelPendingFirmwareRolloutTargets :execrows
UPDATE firmware_rollout_target
SET state = 'canceled'
WHERE rollout_id = $1
  AND state IN ('pending', 'dispatching');

-- name: ResetFailedFirmwareRolloutTargetsForRetry :execrows
UPDATE firmware_rollout_target
SET state = 'pending',
    last_error = NULL
WHERE rollout_id = $1
  AND state = 'failed';

-- name: ClaimFirmwareRolloutTargetsForDispatch :many
WITH picked AS (
    SELECT t.rollout_id, t.device_identifier, t.current_attempt_number + 1 AS attempt_number
    FROM firmware_rollout_target t
    WHERE t.rollout_id = $1
      AND t.state = 'pending'
    ORDER BY t.added_at, t.device_identifier
    LIMIT $2
    FOR UPDATE SKIP LOCKED
),
updated AS (
    UPDATE firmware_rollout_target t
    SET state = 'dispatching',
        current_attempt_number = picked.attempt_number
    FROM picked
    WHERE t.rollout_id = picked.rollout_id
      AND t.device_identifier = picked.device_identifier
    RETURNING t.rollout_id, t.device_identifier, t.current_attempt_number
)
INSERT INTO firmware_rollout_attempt (
    rollout_id,
    device_identifier,
    attempt_number,
    status
)
SELECT updated.rollout_id, updated.device_identifier, updated.current_attempt_number, 'dispatching'
FROM updated
RETURNING rollout_id, device_identifier, attempt_number;

-- name: MarkFirmwareRolloutAttemptDispatched :exec
UPDATE firmware_rollout_attempt
SET command_batch_uuid = $4,
    status = 'dispatched'
WHERE rollout_id = $1
  AND device_identifier = $2
  AND attempt_number = $3;

-- name: MarkFirmwareRolloutTargetDispatched :exec
UPDATE firmware_rollout_target
SET state = 'dispatched',
    last_command_batch_uuid = $3
WHERE rollout_id = $1
  AND device_identifier = $2;

-- name: MarkFirmwareRolloutDispatchFailed :exec
UPDATE firmware_rollout_target
SET state = 'failed',
    last_error = $4
WHERE rollout_id = $1
  AND device_identifier = $2
  AND current_attempt_number = $3;

-- name: MarkFirmwareRolloutAttemptFailed :exec
UPDATE firmware_rollout_attempt
SET status = 'failed',
    error_info = $4,
    finished_at = CURRENT_TIMESTAMP
WHERE rollout_id = $1
  AND device_identifier = $2
  AND attempt_number = $3;

-- name: ListFirmwareRolloutDispatchesToRefresh :many
SELECT
    t.rollout_id,
    t.device_identifier,
    t.current_attempt_number,
    t.last_command_batch_uuid
FROM firmware_rollout_target t
JOIN firmware_rollout r ON r.id = t.rollout_id
WHERE r.state = 'running'
  AND t.state = 'dispatched'
  AND t.last_command_batch_uuid IS NOT NULL
LIMIT $1;

-- name: GetFirmwareRolloutCommandResult :one
SELECT
    codl.status,
    codl.error_info,
    codl.updated_at
FROM command_on_device_log codl
JOIN command_batch_log cbl ON cbl.id = codl.command_batch_log_id
JOIN device d ON d.id = codl.device_id
WHERE cbl.uuid = $1
  AND d.device_identifier = $2
LIMIT 1;

-- name: MarkFirmwareRolloutTargetTerminal :exec
UPDATE firmware_rollout_target
SET state = $4,
    last_error = $5
WHERE rollout_id = $1
  AND device_identifier = $2
  AND current_attempt_number = $3;

-- name: MarkFirmwareRolloutAttemptTerminal :exec
UPDATE firmware_rollout_attempt
SET status = $4,
    error_info = $5,
    finished_at = $6
WHERE rollout_id = $1
  AND device_identifier = $2
  AND attempt_number = $3;

-- name: GetFirmwareRolloutCounts :one
SELECT
    COUNT(*)::int AS total_count,
    COUNT(*) FILTER (WHERE state = 'pending')::int AS pending_count,
    COUNT(*) FILTER (WHERE state IN ('dispatching', 'dispatched'))::int AS in_progress_count,
    COUNT(*) FILTER (WHERE state = 'succeeded')::int AS success_count,
    COUNT(*) FILTER (WHERE state = 'failed')::int AS failure_count,
    COUNT(*) FILTER (WHERE state = 'canceled')::int AS canceled_count,
    COUNT(*) FILTER (WHERE current_attempt_number > 1)::int AS retried_count
FROM firmware_rollout_target
WHERE rollout_id = $1;

-- name: ListFirmwareRolloutTargets :many
SELECT
    t.rollout_id,
    t.device_identifier,
    t.state,
    t.current_attempt_number,
    t.last_command_batch_uuid,
    t.last_error,
    t.updated_at,
    COALESCE(d.custom_name, dd.model, t.device_identifier) AS device_name,
    dd.ip_address,
    d.mac_address
FROM firmware_rollout_target t
LEFT JOIN device d ON d.device_identifier = t.device_identifier
LEFT JOIN discovered_device dd ON dd.id = d.discovered_device_id
WHERE t.rollout_id = $1
  AND (
    sqlc.narg('state_filter')::text IS NULL
    OR t.state = sqlc.narg('state_filter')::text
  )
  AND (
    sqlc.narg('cursor_device_identifier')::text IS NULL
    OR t.device_identifier > sqlc.narg('cursor_device_identifier')::text
  )
ORDER BY t.device_identifier
LIMIT sqlc.arg('page_size');

-- name: ListFirmwareRolloutEvents :many
SELECT *
FROM firmware_rollout_event
WHERE rollout_id = $1
ORDER BY created_at, id;

-- name: InsertFirmwareRolloutEvent :exec
INSERT INTO firmware_rollout_event (
    rollout_id,
    event_type,
    actor_type,
    user_id,
    username,
    message,
    metadata
) VALUES (
    $1,
    $2,
    $3,
    $4,
    $5,
    $6,
    $7
);

-- name: ListRunnableFirmwareRollouts :many
SELECT *
FROM firmware_rollout
WHERE state = 'running'
ORDER BY started_at, id
LIMIT $1;

-- name: FirmwareRolloutHasPendingOrInProgressTargets :one
SELECT EXISTS (
    SELECT 1
    FROM firmware_rollout_target
    WHERE rollout_id = $1
      AND state IN ('pending', 'dispatching', 'dispatched')
) AS has_work;

-- name: FirmwareRolloutHasFailedTargets :one
SELECT EXISTS (
    SELECT 1
    FROM firmware_rollout_target
    WHERE rollout_id = $1
      AND state = 'failed'
) AS has_failures;

-- name: UpsertFirmwareRolloutHeartbeat :exec
INSERT INTO firmware_rollout_reconciler_heartbeat (
    id,
    last_tick_at,
    last_tick_uuid,
    last_tick_duration_ms,
    active_rollout_count
) VALUES (
    1,
    $1,
    $2,
    $3,
    $4
)
ON CONFLICT (id) DO UPDATE SET
    last_tick_at = EXCLUDED.last_tick_at,
    last_tick_uuid = EXCLUDED.last_tick_uuid,
    last_tick_duration_ms = EXCLUDED.last_tick_duration_ms,
    active_rollout_count = EXCLUDED.active_rollout_count;
