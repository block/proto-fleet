-- name: CreateCommandBatchLog :execresult
-- organization_id is captured from the caller's session so downstream
-- org-scoped queries (e.g. GetBatchHeaderForOrg) can filter directly on the
-- batch's owning organization rather than joining through user_organization.
INSERT INTO command_batch_log (
    uuid,
    type,
    created_by,
    created_at,
    status,
    devices_count,
    payload,
    organization_id
) VALUES (
  $1,
  $2,
  $3,
  $4,
  $5,
  $6,
  $7,
  $8
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

-- name: GetBatchDeviceCounts :one
-- Scalar counts only: same SUM/CASE aggregates as GetBatchStatusAndDeviceCounts
-- but without the per-device JSON_AGG arrays or the device join. Used by the
-- activity finalizer, the completion reconciler, and the detail RPC -- all
-- callers that read only the counts and would otherwise pay for materializing
-- two device-identifier arrays they never look at (expensive for large
-- batches, and wasteful inside a REPEATABLE READ snapshot).
SELECT
    cbl.devices_count,
    CAST(COALESCE(SUM(CASE WHEN codl.status = 'SUCCESS' THEN 1 ELSE 0 END), 0) AS BIGINT) AS successful_devices,
    CAST(COALESCE(SUM(CASE WHEN codl.status = 'FAILED' THEN 1 ELSE 0 END), 0) AS BIGINT) AS failed_devices
FROM command_batch_log cbl
LEFT JOIN command_on_device_log codl ON cbl.id = codl.command_batch_log_id
WHERE cbl.uuid = $1
GROUP BY cbl.id;

-- name: GetBatchLog :one
SELECT
    cbl.status,
    cbl.type
FROM command_batch_log cbl
WHERE cbl.uuid = $1;

-- name: GetBatchHeaderForOrg :one
-- Returns the batch header only if its recorded organization_id matches the
-- caller's session org. Rows whose organization_id is NULL (legacy backfill
-- miss: creator had no live user_organization membership at migration time)
-- are invisible to this query, which is the correct closed-by-default
-- posture for a cross-org RPC. The handler returns "not found" when the
-- query yields no row.
SELECT
    cbl.uuid,
    cbl.type,
    cbl.status,
    cbl.devices_count
FROM command_batch_log cbl
WHERE cbl.uuid = $1
  AND cbl.organization_id = $2;

-- name: ListBatchDeviceResults :many
-- Returns one row per device in the batch, ordered deterministically so the
-- client can page or virtualize without reshuffling results across polls.
-- The LEFT JOIN to device preserves identifiers for soft-deleted devices.
--
-- max_rows caps the read server-side so a pathological batch cannot push
-- millions of rows through the driver buffer before the Go truncation cap
-- fires. Callers that want to detect truncation pass (cap + 1); reading one
-- sentinel row beyond the cap keeps `len(rows) > cap` a valid signal.
SELECT
    d.device_identifier,
    codl.status,
    codl.error_info,
    codl.updated_at
FROM command_on_device_log codl
JOIN command_batch_log cbl ON cbl.id = codl.command_batch_log_id
LEFT JOIN device d ON d.id = codl.device_id
WHERE cbl.uuid = $1
ORDER BY d.device_identifier NULLS LAST, codl.id
LIMIT sqlc.arg('max_rows');

-- name: DeleteCommandOnDeviceLogsOlderThan :execrows
-- Paginated retention delete of per-device logs older than the cutoff.
-- Bounded by @max_rows to keep each run short; the cleaner loops until the
-- query returns zero rows.
DELETE FROM command_on_device_log
WHERE id IN (
    SELECT codl.id FROM command_on_device_log codl
    WHERE codl.updated_at < sqlc.arg('cutoff')
    ORDER BY codl.updated_at
    LIMIT sqlc.arg('max_rows')
);

-- name: DeleteCommandBatchLogsOlderThan :execrows
-- Paginated retention delete of command batch headers. Only FINISHED batches
-- older than the cutoff are eligible; a NOT EXISTS guard prevents deleting a
-- header while any per-device rows still reference it (FK safety).
DELETE FROM command_batch_log cbl
WHERE cbl.id IN (
    SELECT cbl_inner.id FROM command_batch_log cbl_inner
    WHERE cbl_inner.status = 'FINISHED'
      AND cbl_inner.finished_at IS NOT NULL
      AND cbl_inner.finished_at < sqlc.arg('cutoff')
      AND NOT EXISTS (
        SELECT 1 FROM command_on_device_log codl
        WHERE codl.command_batch_log_id = cbl_inner.id
      )
    ORDER BY cbl_inner.finished_at
    LIMIT sqlc.arg('max_rows')
);
