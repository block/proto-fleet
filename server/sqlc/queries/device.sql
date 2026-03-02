-- name: InsertDevice :one
INSERT INTO device (
    org_id,
    discovered_device_id,
    device_identifier,
    mac_address,
    serial_number
) VALUES (
    $1,
    $2,
    $3,
    $4,
    $5
)
RETURNING id;

-- name: UpdateDeviceIPAssignment :exec
-- PostgreSQL equivalent of UPDATE with INNER JOIN
UPDATE discovered_device
SET
  ip_address = $1,
  port = $2,
  url_scheme = $3
FROM device d
WHERE discovered_device.id = d.discovered_device_id
  AND d.id = $4;

-- name: GetPairedDevicesIds :many
SELECT
    d.id as device_id
from device d
JOIN device_pairing dp ON d.id = dp.device_id
WHERE dp.pairing_status = 'PAIRED'
    AND d.org_id = $1
    AND d.deleted_at IS NULL
ORDER BY dp.id, d.id;

-- name: GetTotalPairedDevices :one
SELECT COUNT(*)
FROM device d
JOIN discovered_device dd ON d.discovered_device_id = dd.id
JOIN device_pairing dp ON d.id = dp.device_id
LEFT JOIN device_status ds ON d.id = ds.device_id
WHERE dp.pairing_status IN ('PAIRED', 'AUTHENTICATION_NEEDED')
    AND d.deleted_at IS NULL
    AND d.org_id = $1
    AND dd.is_active = TRUE
    AND (sqlc.narg('status_filter')::text IS NULL OR ds.status::text = ANY(string_to_array(sqlc.narg('status_filter'), ',')))
    AND (sqlc.narg('model_filter')::text IS NULL OR dd.model = ANY(string_to_array(sqlc.narg('model_filter'), ',')));

-- name: GetTotalDevicesPendingAuth :one
SELECT COUNT(*)
FROM device d
JOIN device_pairing dp ON d.id = dp.device_id
WHERE dp.pairing_status = 'AUTHENTICATION_NEEDED'
    AND d.deleted_at IS NULL
    AND d.org_id = $1;

-- name: UpsertDevicePairing :execresult
INSERT INTO device_pairing (
    device_id,
    pairing_status,
    paired_at
) VALUES (
    $1,
    $2,
    CURRENT_TIMESTAMP
)
ON CONFLICT (device_id) DO UPDATE SET
    pairing_status = EXCLUDED.pairing_status,
    paired_at = CURRENT_TIMESTAMP,
    unpaired_at = NULL;

-- name: UpdateDevicePairingStatusByIdentifier :exec
-- PostgreSQL equivalent of UPDATE with INNER JOIN
UPDATE device_pairing
SET pairing_status = $1
FROM device d
WHERE device_pairing.device_id = d.id
  AND d.device_identifier = $2
  AND d.deleted_at IS NULL;

-- name: GetDeviceByID :one
SELECT *
FROM device
WHERE id = $1
  AND org_id = $2
  AND deleted_at IS NULL
LIMIT 1;

-- name: GetDeviceByDeviceIdentifier :one
SELECT *
FROM device
WHERE device_identifier = $1
  AND org_id = $2
  AND deleted_at IS NULL
    LIMIT 1;

-- name: UpdateDeviceInfo :exec
UPDATE device
SET
    mac_address = $1,
    serial_number = $2
WHERE device_identifier = $3
  AND org_id = $4
  AND deleted_at IS NULL;

-- name: GetDevicePairingStatusByDeviceDatabaseID :one
SELECT
    dp.pairing_status
FROM device_pairing dp
WHERE dp.device_id = $1
LIMIT 1;

-- name: GetDeviceIDByDeviceIdentifier :one
SELECT id
FROM device
WHERE device_identifier = $1
  AND deleted_at IS NULL
LIMIT 1;

-- name: GetDeviceIdentifierByID :one
SELECT device_identifier
FROM device
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1;

-- name: GetDeviceIDsByDeviceIdentifiers :many
SELECT id
FROM device
WHERE device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND deleted_at IS NULL;

-- name: GetDeviceIDsWithIdentifiers :many
-- Returns device IDs mapped to their identifiers for batch operations.
SELECT id, device_identifier
FROM device
WHERE device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND deleted_at IS NULL;

-- name: AllDevicesBelongToOrg :one
-- Returns true if all provided device identifiers belong to the specified organization.
-- Used for authorization checks - fails fast if any device is not owned by the org.
SELECT COUNT(*) = sqlc.arg('expected_count') as all_belong
FROM device
WHERE device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND org_id = $1
  AND deleted_at IS NULL;

-- name: GetAllPairedDeviceIdentifiers :many
SELECT d.device_identifier
FROM device d
JOIN device_pairing dp ON d.id = dp.device_id
WHERE dp.pairing_status = 'PAIRED'
    AND d.deleted_at IS NULL;

-- name: CountMinersByState :one
-- Counts miners by their operational state for fleet health dashboard.
--
-- Bucket Assignment Priority (mutual exclusivity):
-- 1. Offline (offline_count):
--    - OFFLINE/NULL status (highest priority, regardless of errors or auth status)
-- 2. Sleeping (sleeping_count):
--    - MAINTENANCE/INACTIVE status (only if not offline, regardless of errors or auth status)
-- 3. Needs Attention (broken_count):
--    - NEEDS_MINING_POOL device status OR
--    - ERROR device status OR
--    - AUTHENTICATION_NEEDED pairing status OR
--    - Has open errors with severity CRITICAL/MAJOR/MINOR
--    - (only if not offline or sleeping)
-- 4. Hashing (hashing_count):
--    - ACTIVE status + no auth needed + no actionable errors (only if none of the above)
--
-- Error Handling:
-- - Only open errors (closed_at IS NULL) are considered
-- - INFO severity errors (severity=4) are excluded
-- - UNSPECIFIED severity errors (severity=0) are excluded
-- - Offline/sleeping status takes precedence over errors
SELECT
    -- Offline: OFFLINE or NULL status - highest priority (regardless of errors or auth)
    COALESCE(SUM(CASE
        WHEN ds.status = 'OFFLINE' OR ds.status IS NULL
        THEN 1
        ELSE 0
    END), 0)::bigint as offline_count,

    -- Sleeping: MAINTENANCE or INACTIVE - second priority (if not offline, regardless of errors or auth)
    COALESCE(SUM(CASE
        WHEN ds.status IN ('MAINTENANCE', 'INACTIVE')
        THEN 1
        ELSE 0
    END), 0)::bigint as sleeping_count,

    -- Broken/Needs Attention: NEEDS_MINING_POOL OR ERROR status OR auth needed OR actionable errors
    -- Only if not offline or sleeping
    COALESCE(SUM(CASE
        WHEN ds.status NOT IN ('OFFLINE', 'MAINTENANCE', 'INACTIVE')
             AND ds.status IS NOT NULL
             AND (ds.status IN ('ERROR', 'NEEDS_MINING_POOL')
                  OR dp.pairing_status = 'AUTHENTICATION_NEEDED'
                  OR open_errors.device_id IS NOT NULL)
        THEN 1
        ELSE 0
    END), 0)::bigint as broken_count,

    -- Hashing: ACTIVE + no auth needed + no actionable errors (if none of the above)
    COALESCE(SUM(CASE
        WHEN ds.status = 'ACTIVE'
             AND dp.pairing_status != 'AUTHENTICATION_NEEDED'
             AND open_errors.device_id IS NULL
        THEN 1
        ELSE 0
    END), 0)::bigint as hashing_count
FROM device d
JOIN discovered_device dd ON d.discovered_device_id = dd.id
JOIN device_pairing dp ON d.id = dp.device_id
LEFT JOIN device_status ds ON d.id = ds.device_id
-- Check for open actionable errors (CRITICAL, MAJOR, MINOR only)
LEFT JOIN (
    SELECT DISTINCT device_id
    FROM errors
    WHERE errors.org_id = sqlc.arg('org_id')
      AND errors.closed_at IS NULL
      AND errors.severity IN (1, 2, 3)  -- Exclude INFO (4) and UNSPECIFIED (0)
) open_errors ON d.id = open_errors.device_id
WHERE d.deleted_at IS NULL
  AND d.org_id = sqlc.arg('org_id')
  AND dd.is_active = TRUE
  AND dp.pairing_status IN ('PAIRED', 'AUTHENTICATION_NEEDED')
  AND (sqlc.narg('status_filter')::text IS NULL OR ds.status::text = ANY(string_to_array(sqlc.narg('status_filter'), ',')))
  AND (sqlc.narg('model_filter')::text IS NULL OR dd.model = ANY(string_to_array(sqlc.narg('model_filter'), ',')));

-- name: UpsertDeviceStatus :exec
INSERT INTO device_status (
    device_id,
    status,
    status_timestamp,
    status_details
) VALUES (
    $1,
    $2,
    $3,
    $4
)
ON CONFLICT (device_id) DO UPDATE SET
    status = EXCLUDED.status,
    status_timestamp = EXCLUDED.status_timestamp,
    status_details = EXCLUDED.status_details;

-- name: GetDeviceStatus :one
SELECT
    ds.status
FROM device_status ds
WHERE ds.device_id = $1
LIMIT 1;

-- name: GetDeviceStatusByDeviceIdentifier :one
SELECT
    ds.status
FROM device_status ds
JOIN device d ON ds.device_id = d.id
WHERE d.device_identifier = $1
  AND d.deleted_at IS NULL
LIMIT 1;

-- name: GetDeviceStatusForDeviceIdentifiers :many
SELECT
    d.device_identifier,
    ds.status
FROM device_status ds
JOIN device d ON ds.device_id = d.id
WHERE d.device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND d.deleted_at IS NULL;

-- name: GetMinerModelGroups :many
SELECT
    dd.model,
    dd.manufacturer,
    COUNT(*)::int AS count
FROM device d
JOIN discovered_device dd ON d.discovered_device_id = dd.id
JOIN device_pairing dp ON d.id = dp.device_id
LEFT JOIN device_status ds ON d.id = ds.device_id
WHERE dp.pairing_status = 'PAIRED'
  AND d.deleted_at IS NULL
  AND d.org_id = @org_id
  AND dd.model IS NOT NULL
  AND dd.model != ''
  AND (sqlc.narg('model_filter')::text IS NULL OR dd.model = ANY(string_to_array(sqlc.narg('model_filter'), ',')))
  AND (sqlc.narg('status_filter')::text IS NULL OR ds.status::text = ANY(string_to_array(sqlc.narg('status_filter'), ',')))
GROUP BY dd.model, dd.manufacturer
ORDER BY dd.manufacturer, dd.model;

-- name: GetAvailableModels :many
SELECT DISTINCT dd.model
FROM device d
JOIN discovered_device dd ON d.discovered_device_id = dd.id
JOIN device_pairing dp ON d.id = dp.device_id
WHERE dp.pairing_status = 'PAIRED'
  AND d.deleted_at IS NULL
  AND d.org_id = $1
  AND dd.model IS NOT NULL
  AND dd.model != ''
ORDER BY dd.model
;

-- name: GetOfflineDevices :many
SELECT
    d.id,
    d.device_identifier,
    d.mac_address,
    d.org_id,
    dd.device_identifier AS discovered_device_identifier,
    dd.type,
    dd.ip_address,
    dd.port,
    dd.url_scheme
FROM device d
JOIN device_pairing dp ON d.id = dp.device_id
JOIN device_status ds ON d.id = ds.device_id
JOIN discovered_device dd ON d.discovered_device_id = dd.id
WHERE dp.pairing_status = 'PAIRED'
  AND d.deleted_at IS NULL
  AND ds.status = 'OFFLINE'
  AND d.mac_address IS NOT NULL
  AND d.mac_address != ''
ORDER BY ds.status_timestamp DESC
LIMIT $1;

-- name: ListMinerStateSnapshots :many
-- TYPE GENERATION STUB - This query is never executed.
-- The actual list query uses a hand-written query builder in device.go
-- because sqlc cannot parameterize ORDER BY direction or dynamic columns.
-- This stub exists solely to generate the ListMinerStateSnapshotsRow type.
SELECT
    dd.device_identifier,
    COALESCE(d.mac_address, '') as mac_address,
    d.serial_number,
    dd.model,
    dd.manufacturer,
    dd.type,
    dd.firmware_version,
    ds.status as device_status,
    ds.status_timestamp,
    ds.status_details,
    dd.ip_address,
    dd.port,
    dd.url_scheme,
    CASE WHEN d.id IS NOT NULL THEN COALESCE(dp.pairing_status::text, 'UNPAIRED') ELSE 'UNPAIRED' END as pairing_status,
    dd.id as cursor_id,
    COALESCE(d.id, 0) as device_id,
    d.custom_name
FROM discovered_device dd
LEFT JOIN device d ON dd.id = d.discovered_device_id
LEFT JOIN device_pairing dp ON d.id = dp.device_id
LEFT JOIN device_status ds ON d.id = ds.device_id
WHERE FALSE;

-- name: GetDevicePropertiesForRename :many
-- Returns the device properties needed for name generation during a rename operation.
SELECT
    d.device_identifier,
    COALESCE(d.mac_address, '') as mac_address,
    d.serial_number,
    dd.model,
    dd.manufacturer
FROM device d
JOIN discovered_device dd ON d.discovered_device_id = dd.id
WHERE d.device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND d.org_id = sqlc.arg('org_id')
  AND d.deleted_at IS NULL;


-- name: GetTotalMinerStateSnapshots :one
-- Unified query that supports all filters including component error filtering
-- Uses EXISTS for error checks (more efficient than LEFT JOIN + DISTINCT)
SELECT COUNT(*) as total
FROM discovered_device dd
LEFT JOIN device d ON dd.id = d.discovered_device_id
    AND d.deleted_at IS NULL
    AND d.org_id = sqlc.arg('org_id')
LEFT JOIN device_pairing dp ON d.id = dp.device_id
LEFT JOIN device_status ds ON d.id = ds.device_id
WHERE dd.org_id = sqlc.arg('org_id')
    AND dd.is_active = TRUE
    AND dd.deleted_at IS NULL
    -- Pairing status filter
    AND (
        sqlc.narg('pairing_status_filter')::text IS NULL
        OR CASE WHEN d.id IS NOT NULL THEN COALESCE(dp.pairing_status::text, 'UNPAIRED') ELSE 'UNPAIRED' END
           = ANY(sqlc.arg('pairing_status_values')::text[])
    )
    -- Model filter
    AND (sqlc.narg('model_filter')::text IS NULL OR dd.model = ANY(sqlc.arg('model_values')::text[]))
    -- Status filter with error handling
    AND (
        sqlc.narg('status_filter')::text IS NULL
        OR (
            ds.status::text = ANY(sqlc.arg('status_values')::text[])
            AND (
                ds.status IN ('OFFLINE', 'MAINTENANCE', 'INACTIVE', 'NEEDS_MINING_POOL')
                OR (ds.status = 'ACTIVE' AND NOT EXISTS (
                    SELECT 1 FROM errors
                    WHERE errors.device_id = d.id
                      AND errors.org_id = sqlc.arg('org_id')
                      AND errors.closed_at IS NULL
                      AND errors.severity IN (1, 2, 3)
                ))
                OR (sqlc.narg('needs_attention_filter')::boolean = TRUE)
            )
        )
        OR (sqlc.narg('needs_attention_filter')::boolean = TRUE
            AND dp.pairing_status = 'AUTHENTICATION_NEEDED'
            AND ds.status NOT IN ('OFFLINE', 'MAINTENANCE', 'INACTIVE', 'NEEDS_MINING_POOL')
            AND ds.status IS NOT NULL)
        OR (sqlc.narg('needs_attention_filter')::boolean = TRUE
            AND EXISTS (
                SELECT 1 FROM errors
                WHERE errors.device_id = d.id
                  AND errors.org_id = sqlc.arg('org_id')
                  AND errors.closed_at IS NULL
                  AND errors.severity IN (1, 2, 3)
            )
            AND ds.status NOT IN ('OFFLINE', 'MAINTENANCE', 'INACTIVE', 'NEEDS_MINING_POOL')
            AND ds.status IS NOT NULL)
    )
    -- Component error filter
    AND (
        sqlc.narg('error_component_types_filter')::text IS NULL
        OR EXISTS (
            SELECT 1 FROM errors
            WHERE errors.device_id = d.id
              AND errors.closed_at IS NULL
              AND errors.component_type = ANY(sqlc.arg('error_component_type_values')::int[])
        )
    );

-- name: GetFilteredDeviceIds :many
-- Returns device IDs filtered by pairing status and optional device status.
-- Used for bulk command operations.
SELECT
    d.id as device_id
FROM device d
JOIN device_pairing dp ON d.id = dp.device_id
JOIN discovered_device dd ON d.discovered_device_id = dd.id
LEFT JOIN device_status ds ON d.id = ds.device_id
WHERE d.org_id = sqlc.arg('org_id')
    AND dp.pairing_status::text = COALESCE(sqlc.narg('pairing_status')::text, 'PAIRED')
    AND d.deleted_at IS NULL
    AND (sqlc.narg('device_status')::text IS NULL OR ds.status::text = sqlc.narg('device_status')::text)
    AND (sqlc.narg('model_filter')::text IS NULL OR dd.model = ANY(string_to_array(sqlc.narg('model_filter'), ',')))
ORDER BY d.id;

-- name: GetDeviceInfoForCapabilityCheck :many
-- Returns device information needed for capability checking.
-- Used when checking if specific devices support a command.
SELECT
    d.id,
    d.device_identifier,
    dd.manufacturer,
    dd.model,
    dd.type,
    dd.firmware_version
FROM device d
JOIN discovered_device dd ON d.discovered_device_id = dd.id
JOIN device_pairing dp ON d.id = dp.device_id
WHERE d.device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND d.deleted_at IS NULL
  AND d.org_id = sqlc.arg('org_id')
  AND dp.pairing_status = 'PAIRED';

-- name: GetAllDeviceInfoForCapabilityCheck :many
-- Returns device information for all paired devices in an organization.
-- Used when checking capabilities for "select all" operations.
SELECT
    d.id,
    d.device_identifier,
    dd.manufacturer,
    dd.model,
    dd.type,
    dd.firmware_version
FROM device d
JOIN discovered_device dd ON d.discovered_device_id = dd.id
JOIN device_pairing dp ON d.id = dp.device_id
WHERE d.org_id = $1
  AND d.deleted_at IS NULL
  AND dp.pairing_status = 'PAIRED';

-- name: SoftDeleteDevices :execrows
-- Soft-deletes devices by setting deleted_at timestamp.
-- Returns the number of rows affected.
UPDATE device SET deleted_at = NOW()
WHERE device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL;

-- name: SoftDeleteDiscoveredDevicesForDeletedDevices :exec
-- Soft-deletes discovered_device records linked to the specified devices.
UPDATE discovered_device dd SET deleted_at = NOW()
FROM device d
WHERE dd.id = d.discovered_device_id
  AND d.device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND d.org_id = sqlc.arg('org_id')
  AND dd.deleted_at IS NULL;

-- GetDeviceIdentifiersByOrgWithFilter is implemented as a dynamic query in
-- sqlstores/device.go to reuse appendFilterSQL and ensure semantic parity with
-- the list view's "needs attention" filter logic (ERROR status includes devices
-- with open actionable errors).

