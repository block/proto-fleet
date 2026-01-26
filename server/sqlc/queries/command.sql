-- name: CreateCommandBatchLog :execresult
INSERT INTO command_batch_log (
    uuid,
    type,
    created_by,
    created_at,
    status,
    devices_count,
    payload
) VALUES (
  ?,
  ?,
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
WHERE uuid = ?;

-- name: MarkCommandBatchFinished :exec
UPDATE command_batch_log
SET status = 'FINISHED',
   finished_at = NOW()
WHERE uuid = ?;

-- name: MarkCommandBatchFinishedWithStartedAt :exec
UPDATE command_batch_log
SET status = 'FINISHED',
    started_at = NOW(),
    finished_at = NOW()
WHERE uuid = ?;

-- name: UpsertCommandOnDeviceLog :exec
INSERT INTO command_on_device_log (
   command_batch_log_id,
   device_id,
   status,
   updated_at
)
SELECT
  cbl.id,
  ?,
  ?,
  ?
FROM command_batch_log cbl
WHERE cbl.uuid = ?
ON DUPLICATE KEY UPDATE
    status = VALUES(status),
    updated_at = VALUES(updated_at);

-- name: GetBatchStatusAndDeviceCounts :one
SELECT
    cbl.id,
    cbl.uuid,
    cbl.status,
    cbl.devices_count,
    CAST(COALESCE(SUM(CASE WHEN codl.status = 'SUCCESS' THEN 1 ELSE 0 END), 0) AS SIGNED) AS successful_devices,
    CAST(COALESCE(SUM(CASE WHEN codl.status = 'FAILED' THEN 1 ELSE 0 END), 0) AS SIGNED) AS failed_devices,
    COALESCE(JSON_ARRAYAGG(
        CASE WHEN codl.status = 'SUCCESS' THEN d.device_identifier ELSE NULL END
    ), JSON_ARRAY()) AS success_device_identifiers,
    COALESCE(JSON_ARRAYAGG(
        CASE WHEN codl.status = 'FAILED' THEN d.device_identifier ELSE NULL END
    ), JSON_ARRAY()) AS failure_device_identifiers
FROM
    command_batch_log cbl
        LEFT JOIN
    command_on_device_log codl ON cbl.id = codl.command_batch_log_id
        LEFT JOIN
    device d ON codl.device_id = d.id
WHERE
    cbl.uuid = ?
GROUP BY
    cbl.id;

-- name: GetBatchLog :one
SELECT
    cbl.status,
    cbl.type
FROM command_batch_log cbl
WHERE cbl.uuid = ?;
