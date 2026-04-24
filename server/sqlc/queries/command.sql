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
--
-- custom_name/manufacturer/model/ip_address/mac_address are captured from
-- device + discovered_device at command-completion time (the first terminal
-- write) and deliberately left untouched by the ON CONFLICT branch, so
-- retries and the reaper never overwrite the first-write values. The read
-- path composes the display name via fleetmanagement.ComposeDeviceName so
-- this query stays free of any rendering rules.
WITH batch AS (
    SELECT id FROM command_batch_log WHERE uuid = $4
),
dev AS (
    SELECT
        d.custom_name   AS custom_name,
        dd.manufacturer AS manufacturer,
        dd.model        AS model,
        dd.ip_address   AS ip_address,
        d.mac_address   AS mac_address
    FROM device d
    JOIN discovered_device dd ON dd.id = d.discovered_device_id
    WHERE d.id = $1
)
INSERT INTO command_on_device_log (
   command_batch_log_id,
   device_id,
   status,
   updated_at,
   error_info,
   custom_name,
   manufacturer,
   model,
   ip_address,
   mac_address
)
-- batch × dev is a deliberate cross-join: both CTEs must return exactly one
-- row for the INSERT to write. fk_command_on_device_log_device guarantees
-- device $1 exists, and device.discovered_device_id is NOT NULL, so dev
-- always matches in practice.
SELECT
  batch.id,
  $1,
  $2,
  $3,
  $5,
  dev.custom_name,
  dev.manufacturer,
  dev.model,
  dev.ip_address,
  dev.mac_address
FROM batch, dev
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
-- Returns the batch header only if its recorded organization_id matches the
-- caller's session org. Rows with organization_id IS NULL (pre-migration
-- history) are intentionally invisible to this query.
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
    codl.updated_at,
    codl.custom_name,
    codl.manufacturer,
    codl.model,
    codl.ip_address,
    codl.mac_address
FROM command_on_device_log codl
JOIN command_batch_log cbl ON cbl.id = codl.command_batch_log_id
LEFT JOIN device d ON d.id = codl.device_id
WHERE cbl.uuid = $1
ORDER BY d.device_identifier NULLS LAST, codl.id
LIMIT sqlc.arg('max_rows');
