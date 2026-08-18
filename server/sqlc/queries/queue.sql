-- name: CreateQueueMessage :exec
INSERT INTO queue_message (
    command_batch_log_uuid,
    command_type,
    device_id,
    status,
    retry_count,
    max_attempts,
    payload
) VALUES (
     $1,
     $2,
     $3,
     $4,
     $5,
     $6,
     $7
);

-- name: CreateQueueMessages :exec
INSERT INTO queue_message (
    command_batch_log_uuid,
    command_type,
    device_id,
    status,
    retry_count,
    payload
)
SELECT
    sqlc.arg('command_batch_log_uuid'),
    sqlc.arg('command_type'),
    devices.device_id,
    'PENDING'::queue_status_enum,
    0,
    payloads.payload::JSONB
FROM unnest(sqlc.arg('device_ids')::BIGINT[]) WITH ORDINALITY AS devices(device_id, ord)
JOIN unnest(sqlc.arg('payloads')::TEXT[]) WITH ORDINALITY AS payloads(payload, ord) USING (ord);

-- name: GetQueueMessagesByBatch :many
SELECT id, device_id, status, error_info, payload
FROM queue_message
WHERE command_batch_log_uuid = $1;

-- name: CountQueueMessagesByBatch :one
SELECT COUNT(*)
FROM queue_message
WHERE command_batch_log_uuid = $1;

-- name: SetLocalTransactionTimeout :exec
SELECT set_config(
    'transaction_timeout',
    sqlc.arg('timeout_milliseconds')::BIGINT::TEXT || 'ms',
    TRUE
);

-- name: UpdateMessageStatus :execresult
UPDATE queue_message
SET status = $1,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $2
  AND status = 'PROCESSING';

-- name: UpdateMessageAfterFailure :execresult
UPDATE queue_message
SET status = CASE
        WHEN retry_count + 1 >= max_attempts THEN 'FAILED'::queue_status_enum
        ELSE 'PENDING'::queue_status_enum
        END,
    retry_count = retry_count + 1,
    error_info = $1,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $2
  AND status = 'PROCESSING';

-- name: UpdateMessagePermanentlyFailed :execresult
UPDATE queue_message
SET status = 'FAILED'::queue_status_enum,
    error_info = $1,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $2
  AND status = 'PROCESSING';

-- name: ClaimMessageForProcessing :execresult
UPDATE queue_message
SET status = 'PROCESSING'::queue_status_enum,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $1
  AND status = 'PENDING';

-- name: GetMessagesToProcess :many
SELECT m.id, m.command_batch_log_uuid, m.device_id, m.command_type, m.status,
       m.retry_count, m.max_attempts, m.error_info, m.payload,
       m.created_at, m.updated_at,
       d.org_id
FROM queue_message m
JOIN device d ON m.device_id = d.id
WHERE m.status = 'PENDING'
  AND m.retry_count < m.max_attempts
  AND NOT EXISTS (
    SELECT 1
    FROM queue_message earlier
    WHERE earlier.device_id = m.device_id
      AND (earlier.status = 'PENDING' OR earlier.status = 'PROCESSING')
      AND earlier.created_at < m.created_at
)
ORDER BY m.created_at
LIMIT sqlc.arg('dequeue_limit');

-- name: ReapMessages :many
-- Startup reaping fails every PROCESSING row left by the previous process.
-- Periodic reaping only fails rows that exceeded their command-specific cutoff.
WITH candidates AS (
    SELECT message.id
    FROM queue_message AS message
    WHERE message.status = 'PROCESSING'
      AND (
        sqlc.arg('include_fresh')::BOOLEAN
        OR message.updated_at < CASE
            WHEN message.command_type = 'FirmwareUpdate'
                THEN sqlc.arg('firmware_cutoff')::TIMESTAMPTZ
            ELSE sqlc.arg('cutoff')::TIMESTAMPTZ
        END
      )
    ORDER BY message.updated_at, message.id
    LIMIT sqlc.arg('reap_limit')
    FOR UPDATE
)
UPDATE queue_message AS message
SET
    status = 'FAILED'::queue_status_enum,
    error_info = CASE
        WHEN message.command_type = 'FirmwareUpdate'
            THEN sqlc.arg('firmware_error_info')::TEXT
        ELSE sqlc.arg('error_info')::TEXT
    END,
    updated_at = CURRENT_TIMESTAMP
FROM candidates, device
WHERE message.id = candidates.id
  AND message.status = 'PROCESSING'
  AND message.device_id = device.id
RETURNING
    message.id,
    message.device_id,
    message.command_batch_log_uuid,
    message.error_info,
    message.command_type,
    device.org_id,
    device.site_id;

-- name: ResetReapedFirmwareStatuses :exec
UPDATE device_status
SET
    status = 'ACTIVE'::device_status_enum,
    status_timestamp = CURRENT_TIMESTAMP,
    status_details = NULL
WHERE device_id = ANY(sqlc.arg('device_ids')::BIGINT[])
  AND status IN ('UPDATING', 'REBOOT_REQUIRED');

-- name: FinishTerminalCommandBatches :execrows
WITH candidates AS MATERIALIZED (
    SELECT batch.id
    FROM command_batch_log AS batch
    WHERE batch.status IN ('PENDING', 'PROCESSING')
      AND EXISTS (
          SELECT 1
          FROM queue_message AS message
          WHERE message.command_batch_log_uuid = batch.uuid
      )
      AND NOT EXISTS (
          SELECT 1
          FROM queue_message AS message
          WHERE message.command_batch_log_uuid = batch.uuid
            AND message.status IN ('PENDING', 'PROCESSING')
      )
    ORDER BY batch.id
    LIMIT sqlc.arg('finish_limit')
    FOR UPDATE
)
UPDATE command_batch_log AS batch
SET
    status = 'FINISHED'::batch_status_enum,
    finished_at = CURRENT_TIMESTAMP
FROM candidates
WHERE batch.id = candidates.id
  AND batch.status IN ('PENDING', 'PROCESSING');

-- name: IsBatchFinished :one
SELECT
    CASE
        WHEN COUNT(*) = 0 THEN false
        WHEN COUNT(*) = SUM(CASE WHEN status IN ('SUCCESS', 'FAILED') THEN 1 ELSE 0 END) THEN true
        ELSE false
    END AS is_finished
FROM queue_message
WHERE command_batch_log_uuid = $1;
