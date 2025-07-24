-- name: CreateQueueMessage :exec
INSERT INTO queue_message (
    command_batch_log_uuid,
    command_type,
    device_id,
    status,
    retry_count,
    payload
) VALUES (
     ?,
     ?,
     ?,
     ?,
     ?,
     ?
);

-- name: UpdateMessageStatus :exec
UPDATE queue_message
SET status = ?,
    updated_at = CURRENT_TIMESTAMP(6)
WHERE id = ?;

-- name: UpdateMessageAfterFailure :exec
UPDATE queue_message
SET status = CASE
        WHEN retry_count + 1 >= ? THEN 'FAILED'
        ELSE 'PENDING'
        END,
    retry_count = retry_count + 1,
    error_info = ?,
    updated_at = CURRENT_TIMESTAMP(6)
WHERE id = ?;

-- name: GetMessagesToProcess :many
SELECT m.*
FROM queue_message m
WHERE m.status = 'PENDING'
  AND m.retry_count < ?
  AND NOT EXISTS (
    SELECT 1
    FROM queue_message earlier
    WHERE earlier.device_id = m.device_id
      AND (earlier.status = 'PENDING' OR earlier.status = 'PROCESSING')
      AND earlier.created_at < m.created_at
)
ORDER BY m.created_at
LIMIT ?;

-- name: IsBatchFinished :one
SELECT
    CASE
        WHEN COUNT(*) = 0 THEN false
        WHEN COUNT(*) = SUM(CASE WHEN status IN ('SUCCESS', 'FAILED') THEN 1 ELSE 0 END) THEN true
        ELSE false
    END AS is_finished
FROM queue_message
WHERE command_batch_log_uuid = ?;

-- name: IsBatchProcessing :one
SELECT
    CASE
        WHEN COUNT(*) > 0 THEN true
        ELSE false
        END AS is_processing
FROM queue_message
WHERE command_batch_log_uuid = ?
  AND status = 'PROCESSING';
