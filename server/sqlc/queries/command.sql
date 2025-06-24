-- name: CreateCommandBatchLog :execresult
INSERT INTO command_batch_log (
    uuid,
    type,
    created_by,
    created_at,
    status
) VALUES (
  ?,
  ?,
  ?,
  ?,
  ?
);

-- name: MarkCommandBatchProcessing :exec
UPDATE command_batch_log
SET status = 'PROCESSING',
    started_at = NOW()
WHERE id = ?;

-- name: MarkCommandBatchFinished :exec
UPDATE command_batch_log
SET status = 'FINISHED',
   finished_at = NOW()
WHERE id = ?;

-- name: MarkCommandBatchFinishedWithStartedAt :exec
UPDATE command_batch_log
SET status = 'FINISHED',
    started_at = NOW(),
    finished_at = NOW()
WHERE id = ?;

-- name: UpsertCommandOnDeviceLog :exec
INSERT INTO command_on_device_log (
   command_batch_log_id,
   device_id,
   status,
   updated_at
) VALUES (
  ?,
  ?,
  ?,
  ?
) ON DUPLICATE KEY UPDATE
    status = VALUES(status),
    updated_at = VALUES(updated_at);

-- name: GetBatchStatusAndDeviceCounts :one
SELECT
    cbl.id,
    cbl.uuid,
    cbl.status,
    COUNT(codl.id) AS total_devices,
    SUM(CASE WHEN codl.status = 'SUCCESS' THEN 1 ELSE 0 END) AS successful_devices,
    SUM(CASE WHEN codl.status = 'FAILED' THEN 1 ELSE 0 END) AS failed_devices
FROM
    command_batch_log cbl
        LEFT JOIN
    command_on_device_log codl ON cbl.id = codl.command_batch_log_id
WHERE
    cbl.uuid = ?
GROUP BY
    cbl.id;
