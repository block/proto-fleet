-- name: InsertDevice :execresult
INSERT INTO device (
    org_id,
    discovered_device_id,
    device_identifier,
    mac_address,
    serial_number
) VALUES (
    ?,
    ?,
    ?,
    ?,
    ?
);

-- name: GetDeviceByIdentifier :one
SELECT id, device_identifier
FROM device
WHERE device_identifier = ?
    AND org_id = ?
LIMIT 1;

-- name: UpdateDeviceIPAssignment :exec
UPDATE discovered_device dd
INNER JOIN device d ON dd.id = d.discovered_device_id
SET
  dd.ip_address = ?,
  dd.port = ?,
  dd.url_scheme = ?
WHERE d.id = ?;

-- name: GetPairedDevicesIds :many
SELECT
    d.id as device_id
from device d
JOIN device_pairing dp ON d.id = dp.device_id
WHERE dp.pairing_status = 'PAIRED'
    AND d.org_id = ?
    AND d.deleted_at IS NULL
ORDER BY dp.id, d.id;

-- name: GetFilteredDeviceIds :many
SELECT
    d.id as device_id
FROM device d
JOIN device_pairing dp ON d.id = dp.device_id
LEFT JOIN device_status ds ON d.id = ds.device_id
WHERE d.org_id = ?
    AND dp.pairing_status = COALESCE(sqlc.narg('pairing_status'), 'PAIRED')
    AND d.deleted_at IS NULL
    AND (sqlc.narg('device_status') IS NULL OR ds.status = sqlc.narg('device_status'))
ORDER BY d.id;

-- name: GetTotalPairedDevices :one
SELECT COUNT(*)
FROM device d
JOIN discovered_device dd ON d.discovered_device_id = dd.id
JOIN device_pairing dp ON d.id = dp.device_id
LEFT JOIN device_status ds ON d.id = ds.device_id
WHERE dp.pairing_status IN ('PAIRED', 'AUTHENTICATION_NEEDED')
    AND d.deleted_at IS NULL
    AND d.org_id = ?
    AND dd.is_active = TRUE
    AND (sqlc.narg('status_filter') is null OR FIND_IN_SET(ds.status, sqlc.narg('status_filter')))
    AND (sqlc.narg('type_filter') is null OR FIND_IN_SET(dd.type, sqlc.narg('type_filter')));

-- name: GetTotalDevicesPendingAuth :one
SELECT COUNT(*)
FROM device d
JOIN device_pairing dp ON d.id = dp.device_id
WHERE dp.pairing_status = 'AUTHENTICATION_NEEDED'
    AND d.deleted_at IS NULL
    AND d.org_id = ?;

-- name: UpsertDevicePairing :execresult
INSERT INTO device_pairing (
    device_id,
    pairing_status,
    paired_at
) VALUES (
    ?,
    ?,
    CURRENT_TIMESTAMP(6)
)
ON DUPLICATE KEY UPDATE
    pairing_status = VALUES(pairing_status),
    paired_at = CURRENT_TIMESTAMP(6),
    unpaired_at = NULL;

-- name: UpdateDevicePairingStatusByIdentifier :exec
UPDATE device_pairing dp
INNER JOIN device d ON dp.device_id = d.id
SET dp.pairing_status = ?
WHERE d.device_identifier = ?
  AND d.deleted_at IS NULL;

-- name: GetDeviceByID :one
SELECT *
FROM device
WHERE id = ?
  AND org_id = ?
  AND deleted_at IS NULL
LIMIT 1;

-- name: GetDeviceByDeviceIdentifier :one
SELECT *
FROM device
WHERE device_identifier = ?
  AND org_id = ?
  AND deleted_at IS NULL
    LIMIT 1;

-- name: UpdateDeviceInfo :exec
UPDATE device
SET
    mac_address = ?,
    serial_number = ?
WHERE device_identifier = ?
  AND org_id = ?
  AND deleted_at IS NULL;

-- name: GetDevicePairingStatusByDeviceDatabaseID :one
SELECT
    dp.pairing_status
FROM device_pairing dp
WHERE dp.device_id = ?
LIMIT 1;

-- name: GetDeviceIDByDeviceIdentifier :one
SELECT id
FROM device
WHERE device_identifier = ?
LIMIT 1;

-- name: GetDeviceIdentifierByID :one
SELECT device_identifier
FROM device
WHERE id = ?
LIMIT 1;

-- name: GetDeviceIDsByDeviceIdentifiers :many
SELECT id
FROM device
WHERE device_identifier IN (sqlc.slice('device_identifiers'));

-- name: GetDeviceIDsWithIdentifiers :many
-- Returns device IDs mapped to their identifiers for batch operations.
SELECT id, device_identifier
FROM device
WHERE device_identifier IN (sqlc.slice('device_identifiers'));

-- name: AllDevicesBelongToOrg :one
-- Returns true if all provided device identifiers belong to the specified organization.
-- Used for authorization checks - fails fast if any device is not owned by the org.
SELECT COUNT(*) = sqlc.arg('expected_count') as all_belong
FROM device
WHERE device_identifier IN (sqlc.slice('device_identifiers'))
  AND org_id = ?
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
    SUM(CASE
        WHEN ds.status = 'OFFLINE' OR ds.status IS NULL
        THEN 1
        ELSE 0
    END) as offline_count,

    -- Sleeping: MAINTENANCE or INACTIVE - second priority (if not offline, regardless of errors or auth)
    SUM(CASE
        WHEN ds.status IN ('MAINTENANCE', 'INACTIVE')
        THEN 1
        ELSE 0
    END) as sleeping_count,

    -- Broken/Needs Attention: NEEDS_MINING_POOL OR ERROR status OR auth needed OR actionable errors
    -- Only if not offline or sleeping
    SUM(CASE
        WHEN ds.status NOT IN ('OFFLINE', 'MAINTENANCE', 'INACTIVE')
             AND ds.status IS NOT NULL
             AND (ds.status IN ('ERROR', 'NEEDS_MINING_POOL')
                  OR dp.pairing_status = 'AUTHENTICATION_NEEDED'
                  OR open_errors.device_id IS NOT NULL)
        THEN 1
        ELSE 0
    END) as broken_count,

    -- Hashing: ACTIVE + no auth needed + no actionable errors (if none of the above)
    SUM(CASE
        WHEN ds.status = 'ACTIVE'
             AND dp.pairing_status != 'AUTHENTICATION_NEEDED'
             AND open_errors.device_id IS NULL
        THEN 1
        ELSE 0
    END) as hashing_count
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
  AND (sqlc.narg('status_filter') is null OR FIND_IN_SET(ds.status, sqlc.narg('status_filter')))
  AND (sqlc.narg('type_filter') is null OR FIND_IN_SET(dd.type, sqlc.narg('type_filter')));

-- name: UpsertDeviceStatus :exec
INSERT INTO device_status (
    device_id,
    status,
    status_timestamp,
    status_details
) VALUES (
    ?,
    ?,
    ?,
    ?
)
ON DUPLICATE KEY UPDATE
    status = VALUES(status),
    status_timestamp = VALUES(status_timestamp),
    status_details = VALUES(status_details);

-- name: GetDeviceStatus :one
SELECT
    ds.status
FROM device_status ds
WHERE ds.device_id = ?
LIMIT 1;

-- name: GetDeviceStatusByDeviceIdentifier :one
SELECT
    ds.status
FROM device_status ds
JOIN device d ON ds.device_id = d.id
WHERE d.device_identifier = ?
  AND d.deleted_at IS NULL
LIMIT 1;

-- name: GetDeviceStatusForDeviceIdentifiers :many
SELECT
    d.device_identifier,
    ds.status
FROM device_status ds
JOIN device d ON ds.device_id = d.id
WHERE d.device_identifier IN (sqlc.slice('device_identifiers'))
  AND d.deleted_at IS NULL;

-- name: GetAvailableMinerTypes :many
SELECT DISTINCT dd.type
FROM device d
JOIN discovered_device dd ON d.discovered_device_id = dd.id
JOIN device_pairing dp ON d.id = dp.device_id
WHERE dp.pairing_status = 'PAIRED'
  AND d.deleted_at IS NULL
  AND d.org_id = ?
  AND dd.type IS NOT NULL
ORDER BY dd.type
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
LIMIT ?;

-- name: ListMinerStateSnapshots :many
-- Unified query that supports all filters including component error filtering
-- Uses LEFT JOIN with errors table to support filtering by component types when needed
SELECT DISTINCT
    device_identifier,
    mac_address,
    serial_number,
    model,
    manufacturer,
    type,
    firmware_version,
    device_status,
    status_timestamp,
    status_details,
    ip_address,
    port,
    url_scheme,
    pairing_status,
    cursor_id,
    device_id
FROM (
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
        dd.id as cursor_id,
        COALESCE(d.id, 0) as device_id,
        CASE
            WHEN d.id IS NOT NULL AND dp.pairing_status = 'PAIRED' THEN 'PAIRED'
            WHEN d.id IS NOT NULL AND dp.pairing_status = 'AUTHENTICATION_NEEDED' THEN 'AUTHENTICATION_NEEDED'
            WHEN d.id IS NOT NULL AND dp.pairing_status = 'PENDING' THEN 'PENDING'
            WHEN d.id IS NOT NULL AND dp.pairing_status = 'FAILED' THEN 'FAILED'
            ELSE 'UNPAIRED'
        END as pairing_status,
        e.id as error_id
    FROM discovered_device dd
    LEFT JOIN device d ON dd.id = d.discovered_device_id
        AND d.deleted_at IS NULL
        AND d.org_id = sqlc.arg('org_id')
    LEFT JOIN device_pairing dp ON d.id = dp.device_id
    LEFT JOIN device_status ds ON d.id = ds.device_id
    -- Check for open actionable errors (CRITICAL, MAJOR, MINOR only)
    LEFT JOIN (
        SELECT DISTINCT device_id
        FROM errors
        WHERE errors.org_id = sqlc.arg('org_id')
          AND errors.closed_at IS NULL
          AND errors.severity IN (1, 2, 3)  -- Exclude INFO (4) and UNSPECIFIED (0)
    ) open_errors ON d.id = open_errors.device_id
    -- Always include errors join to support component type filtering
    LEFT JOIN errors e ON d.id = e.device_id
        AND e.closed_at IS NULL
        AND (
            sqlc.narg('error_component_types_filter') IS NULL
            OR e.component_type IN (sqlc.slice('error_component_type_values'))
        )
    WHERE dd.org_id = sqlc.arg('org_id')
        AND dd.is_active = TRUE
        AND dd.deleted_at IS NULL
        -- Cursor pagination (applied early for performance)
        AND (
            COALESCE(sqlc.narg('cursor_id'), 0) = 0
            OR dd.id > sqlc.narg('cursor_id')
        )
        -- Status filter (only applies to paired devices with status)
        -- Priority: offline/sleeping status takes precedence over errors
        AND (
            sqlc.narg('status_filter') IS NULL
            OR (
                ds.status IN (sqlc.slice('status_values'))
                AND (
                    -- For offline/sleeping/needs-pool filters: include devices regardless of errors
                    ds.status IN ('OFFLINE', 'MAINTENANCE', 'INACTIVE', 'NEEDS_MINING_POOL')
                    -- For active status: exclude devices with errors (only show truly healthy)
                    OR (ds.status = 'ACTIVE' AND open_errors.device_id IS NULL)
                    -- For error status: include devices with errors
                    OR (sqlc.narg('needs_attention_filter') = TRUE)
                )
            )
            -- For needs attention filter: only include auth needed devices that are not offline/sleeping/needs pool
            OR (sqlc.narg('needs_attention_filter') = TRUE
                AND dp.pairing_status = 'AUTHENTICATION_NEEDED'
                AND ds.status NOT IN ('OFFLINE', 'MAINTENANCE', 'INACTIVE', 'NEEDS_MINING_POOL')
                AND ds.status IS NOT NULL)
            -- For needs attention filter: only include devices with errors that are not offline/sleeping/needs pool
            -- Note: This catches ACTIVE devices with errors. They don't match the first branch (line 379)
            -- because ds.status IN ('ERROR') would be FALSE (status is 'ACTIVE', not 'ERROR').
            -- Instead, they're caught here by checking open_errors.device_id IS NOT NULL.
            OR (sqlc.narg('needs_attention_filter') = TRUE
                AND open_errors.device_id IS NOT NULL
                AND ds.status NOT IN ('OFFLINE', 'MAINTENANCE', 'INACTIVE', 'NEEDS_MINING_POOL')
                AND ds.status IS NOT NULL)
        )
        -- Type filter
        AND (
            sqlc.narg('type_filter') IS NULL
            OR dd.type IN (sqlc.slice('type_values'))
        )
        -- Component error filter - only include devices with matching errors when filter is provided
        AND (
            sqlc.narg('error_component_types_filter') IS NULL
            OR e.id IS NOT NULL
        )
) AS devices_with_status
WHERE
    -- Pairing status filter - if no filter provided (NULL), return all
    (
        sqlc.narg('pairing_status_filter') IS NULL
        OR pairing_status IN (sqlc.slice('pairing_status_values'))
    )
ORDER BY cursor_id
LIMIT ?;


-- name: GetTotalMinerStateSnapshots :one
-- Unified query that supports all filters including component error filtering
-- Uses same structure as ListMinerStateSnapshots for consistency
SELECT COUNT(DISTINCT device_id) as total
FROM (
    SELECT
        dd.id as device_id,
        CASE
            WHEN d.id IS NOT NULL AND dp.pairing_status = 'PAIRED' THEN 'PAIRED'
            WHEN d.id IS NOT NULL AND dp.pairing_status = 'AUTHENTICATION_NEEDED' THEN 'AUTHENTICATION_NEEDED'
            WHEN d.id IS NOT NULL AND dp.pairing_status = 'PENDING' THEN 'PENDING'
            WHEN d.id IS NOT NULL AND dp.pairing_status = 'FAILED' THEN 'FAILED'
            ELSE 'UNPAIRED'
        END as pairing_status
    FROM discovered_device dd
    LEFT JOIN device d ON dd.id = d.discovered_device_id
        AND d.deleted_at IS NULL
        AND d.org_id = sqlc.arg('org_id')
    LEFT JOIN device_pairing dp ON d.id = dp.device_id
    LEFT JOIN device_status ds ON d.id = ds.device_id
    -- Check for open actionable errors (CRITICAL, MAJOR, MINOR only)
    LEFT JOIN (
        SELECT DISTINCT device_id
        FROM errors
        WHERE errors.org_id = sqlc.arg('org_id')
          AND errors.closed_at IS NULL
          AND errors.severity IN (1, 2, 3)  -- Exclude INFO (4) and UNSPECIFIED (0)
    ) open_errors ON d.id = open_errors.device_id
    -- Always include errors join to support component type filtering
    LEFT JOIN errors e ON d.id = e.device_id
        AND e.closed_at IS NULL
        AND (
            sqlc.narg('error_component_types_filter') IS NULL
            OR e.component_type IN (sqlc.slice('error_component_type_values'))
        )
    WHERE dd.org_id = sqlc.arg('org_id')
        AND dd.is_active = TRUE
        AND dd.deleted_at IS NULL
        -- Status filter (only applies to paired devices with status)
        -- Priority: offline/sleeping status takes precedence over errors
        AND (
            sqlc.narg('status_filter') IS NULL
            OR (
                ds.status IN (sqlc.slice('status_values'))
                AND (
                    -- For offline/sleeping/needs-pool filters: include devices regardless of errors
                    ds.status IN ('OFFLINE', 'MAINTENANCE', 'INACTIVE', 'NEEDS_MINING_POOL')
                    -- For active status: exclude devices with errors (only show truly healthy)
                    OR (ds.status = 'ACTIVE' AND open_errors.device_id IS NULL)
                    -- For error status: include devices with errors
                    OR (sqlc.narg('needs_attention_filter') = TRUE)
                )
            )
            -- For needs attention filter: only include auth needed devices that are not offline/sleeping/needs pool
            OR (sqlc.narg('needs_attention_filter') = TRUE
                AND dp.pairing_status = 'AUTHENTICATION_NEEDED'
                AND ds.status NOT IN ('OFFLINE', 'MAINTENANCE', 'INACTIVE', 'NEEDS_MINING_POOL')
                AND ds.status IS NOT NULL)
            -- For needs attention filter: only include devices with errors that are not offline/sleeping/needs pool
            -- Note: This catches ACTIVE devices with errors. They don't match the first branch (line 379)
            -- because ds.status IN ('ERROR') would be FALSE (status is 'ACTIVE', not 'ERROR').
            -- Instead, they're caught here by checking open_errors.device_id IS NOT NULL.
            OR (sqlc.narg('needs_attention_filter') = TRUE
                AND open_errors.device_id IS NOT NULL
                AND ds.status NOT IN ('OFFLINE', 'MAINTENANCE', 'INACTIVE', 'NEEDS_MINING_POOL')
                AND ds.status IS NOT NULL)
        )
        -- Type filter
        AND (
            sqlc.narg('type_filter') IS NULL
            OR dd.type IN (sqlc.slice('type_values'))
        )
        -- Component error filter - only include devices with matching errors when filter is provided
        AND (
            sqlc.narg('error_component_types_filter') IS NULL
            OR e.id IS NOT NULL
        )
) AS devices_with_status
WHERE
    -- Pairing status filter - if no filter provided (NULL), return all
    (
        sqlc.narg('pairing_status_filter') IS NULL
        OR pairing_status IN (sqlc.slice('pairing_status_values'))
    );

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
WHERE d.device_identifier IN (sqlc.slice('device_identifiers'))
  AND d.deleted_at IS NULL
  AND d.org_id = ?
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
WHERE d.org_id = ?
  AND d.deleted_at IS NULL
  AND dp.pairing_status = 'PAIRED';

