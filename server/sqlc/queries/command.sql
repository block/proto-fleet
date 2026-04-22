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
-- PostgreSQL version using CTE for the subquery.
-- error_info is NULL for SUCCESS rows; for FAILED rows it is either the worker
-- error string (truncated by the caller) or the reaper reason.
WITH batch AS (
    SELECT id FROM command_batch_log WHERE uuid = $4
)
INSERT INTO command_on_device_log (
   command_batch_log_id,
   device_id,
   status,
   updated_at,
   error_info
)
SELECT
  batch.id,
  $1,
  $2,
  $3,
  $5
FROM batch
ON CONFLICT (command_batch_log_id, device_id) DO UPDATE SET
    status = EXCLUDED.status,
    updated_at = EXCLUDED.updated_at,
    error_info = EXCLUDED.error_info;

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

-- name: GetBatchHeaderForOrg :one
-- Returns the batch header only if the creating user belongs to the caller's
-- organization, giving the detail RPC tenant isolation without a dedicated
-- org_id column on command_batch_log (tracked as an issue #22 follow-up).
-- Returns no rows when the batch does not exist or the caller is not
-- authorized, which the handler translates into "not found".
SELECT
    cbl.uuid,
    cbl.type,
    cbl.status,
    cbl.devices_count
FROM command_batch_log cbl
JOIN user_organization uo ON uo.user_id = cbl.created_by
WHERE cbl.uuid = $1
  AND uo.organization_id = $2
  AND uo.deleted_at IS NULL;

-- name: ListBatchDeviceResults :many
-- Returns one row per device in the batch, ordered deterministically so the
-- client can page or virtualize without reshuffling results across polls.
-- The LEFT JOIN to device preserves identifiers for soft-deleted devices.
SELECT
    d.device_identifier,
    codl.status,
    codl.error_info,
    codl.updated_at
FROM command_on_device_log codl
JOIN command_batch_log cbl ON cbl.id = codl.command_batch_log_id
LEFT JOIN device d ON d.id = codl.device_id
WHERE cbl.uuid = $1
ORDER BY d.device_identifier NULLS LAST, codl.id;
