-- name: CreateQueueMessage :exec
INSERT INTO queue_message (
    command_batch_log_uuid,
    command_type,
    device_id,
    status,
    retry_count,
    payload
) VALUES (
     $1,
     $2,
     $3,
     $4,
     $5,
     $6
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
        WHEN retry_count + 1 >= $1 THEN 'FAILED'::queue_status_enum
        ELSE 'PENDING'::queue_status_enum
        END,
    retry_count = retry_count + 1,
    error_info = $2,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $3
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
SELECT m.*
FROM queue_message m
WHERE m.status = 'PENDING'
  AND m.retry_count < $1
  AND NOT EXISTS (
    SELECT 1
    FROM queue_message earlier
    WHERE earlier.device_id = m.device_id
      AND (earlier.status = 'PENDING' OR earlier.status = 'PROCESSING')
      AND earlier.created_at < m.created_at
)
ORDER BY m.created_at
LIMIT $2;

-- name: ReapStuckProcessingMessages :many
WITH stuck AS (
    SELECT m.id FROM queue_message m
    WHERE m.status = 'PROCESSING'
      AND m.updated_at < @cutoff
      AND m.command_type != 'FirmwareUpdate'
    LIMIT @reap_limit
)
UPDATE queue_message
SET status = 'FAILED'::queue_status_enum,
    error_info = 'reaped: stuck in PROCESSING beyond timeout',
    updated_at = CURRENT_TIMESTAMP
FROM stuck
WHERE queue_message.id = stuck.id
  AND queue_message.status = 'PROCESSING'
RETURNING queue_message.id, queue_message.device_id, queue_message.command_batch_log_uuid;

-- name: ReapStuckFirmwareUpdateMessages :many
WITH stuck AS (
    SELECT m.id FROM queue_message m
    WHERE m.status = 'PROCESSING'
      AND m.updated_at < @cutoff
      AND m.command_type = 'FirmwareUpdate'
    LIMIT @reap_limit
)
UPDATE queue_message
SET status = 'FAILED'::queue_status_enum,
    error_info = 'reaped: firmware update stuck in PROCESSING beyond timeout',
    updated_at = CURRENT_TIMESTAMP
FROM stuck
WHERE queue_message.id = stuck.id
  AND queue_message.status = 'PROCESSING'
RETURNING queue_message.id, queue_message.device_id, queue_message.command_batch_log_uuid;

-- name: IsBatchFinished :one
SELECT
    CASE
        WHEN COUNT(*) = 0 THEN false
        WHEN COUNT(*) = SUM(CASE WHEN status IN ('SUCCESS', 'FAILED') THEN 1 ELSE 0 END) THEN true
        ELSE false
    END AS is_finished
FROM queue_message
WHERE command_batch_log_uuid = $1;

-- name: IsBatchProcessing :one
SELECT
    CASE
        WHEN COUNT(*) > 0 THEN true
        ELSE false
        END AS is_processing
FROM queue_message
WHERE command_batch_log_uuid = $1
  AND status = 'PROCESSING';
