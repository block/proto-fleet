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
  $1,
  $2,
  $3,
  $4,
  $5,
  $6,
  $7
);

-- name: MarkCommandBatchProcessing :exec
UPDATE command_batch_log
SET status = 'PROCESSING',
    started_at = NOW()
WHERE uuid = $1;

-- name: MarkCommandBatchFinished :exec
UPDATE command_batch_log
SET status = 'FINISHED',
   finished_at = NOW()
WHERE uuid = $1;

-- name: MarkCommandBatchFinishedWithStartedAt :exec
UPDATE command_batch_log
SET status = 'FINISHED',
    started_at = NOW(),
    finished_at = NOW()
WHERE uuid = $1;

-- name: UpsertCommandOnDeviceLog :exec
-- PostgreSQL version using CTE for the subquery
WITH batch AS (
    SELECT id FROM command_batch_log WHERE uuid = $4
)
INSERT INTO command_on_device_log (
   command_batch_log_id,
   device_id,
   status,
   updated_at
)
SELECT
  batch.id,
  $1,
  $2,
  $3
FROM batch
ON CONFLICT (command_batch_log_id, device_id) DO UPDATE SET
    status = EXCLUDED.status,
    updated_at = EXCLUDED.updated_at;

-- name: GetBatchStatusAndDeviceCounts :one
SELECT
    cbl.id,
    cbl.uuid,
    cbl.status,
    cbl.devices_count,
    CAST(COALESCE(SUM(CASE WHEN codl.status = 'SUCCESS' THEN 1 ELSE 0 END), 0) AS BIGINT) AS successful_devices,
    CAST(COALESCE(SUM(CASE WHEN codl.status = 'FAILED' THEN 1 ELSE 0 END), 0) AS BIGINT) AS failed_devices,
    COALESCE(JSON_AGG(d.device_identifier) FILTER (WHERE codl.status = 'SUCCESS'), '[]'::json) AS success_device_identifiers,
    COALESCE(JSON_AGG(d.device_identifier) FILTER (WHERE codl.status = 'FAILED'), '[]'::json) AS failure_device_identifiers
FROM
    command_batch_log cbl
        LEFT JOIN
    command_on_device_log codl ON cbl.id = codl.command_batch_log_id
        LEFT JOIN
    device d ON codl.device_id = d.id
WHERE
    cbl.uuid = $1
GROUP BY
    cbl.id;

-- name: GetBatchLog :one
SELECT
    cbl.status,
    cbl.type
FROM command_batch_log cbl
WHERE cbl.uuid = $1;
