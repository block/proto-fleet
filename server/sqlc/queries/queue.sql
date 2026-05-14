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
SELECT m.id, m.command_batch_log_uuid, m.device_id, m.command_type, m.status,
       m.retry_count, m.error_info, m.payload, m.created_at, m.updated_at,
       d.org_id
FROM queue_message m
JOIN device d ON m.device_id = d.id
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
FROM stuck, device
WHERE queue_message.id = stuck.id
  AND queue_message.status = 'PROCESSING'
  AND queue_message.device_id = device.id
RETURNING queue_message.id, queue_message.device_id, queue_message.command_batch_log_uuid,
    queue_message.error_info, queue_message.command_type, device.org_id;

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
FROM stuck, device
WHERE queue_message.id = stuck.id
  AND queue_message.status = 'PROCESSING'
  AND queue_message.device_id = device.id
RETURNING queue_message.id, queue_message.device_id, queue_message.command_batch_log_uuid,
    queue_message.error_info, queue_message.command_type, device.org_id;

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
