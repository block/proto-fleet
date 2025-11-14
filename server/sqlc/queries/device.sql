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

-- name: ListPairedDevices :many
SELECT
    d.device_identifier,
    d.mac_address,
    d.serial_number,
    dd.model,
    dd.manufacturer,
    dd.type,
    dp.id as cursor_id,
    d.id as device_id
FROM device d
JOIN discovered_device dd ON d.discovered_device_id = dd.id
JOIN device_pairing dp ON d.id = dp.device_id
WHERE dp.pairing_status = 'PAIRED'
    AND d.org_id = ?
    AND d.deleted_at IS NULL
    AND (
        -- If cursor provided, filter by it, otherwise return all
        COALESCE(sqlc.narg('cursor_id'), 0) = 0
        OR
        (dp.id > sqlc.narg('cursor_id') OR (dp.id = sqlc.narg('cursor_id') AND d.id > sqlc.narg('device_cursor_id')))
    )
ORDER BY dp.id, d.id
LIMIT ?;

-- name: GetPairedDevicesIds :many
SELECT
    d.id as device_id
from device d
JOIN device_pairing dp ON d.id = dp.device_id
WHERE dp.pairing_status = 'PAIRED'
    AND d.org_id = ?
    AND d.deleted_at IS NULL
ORDER BY dp.id, d.id;

-- name: GetTotalPairedDevices :one
SELECT COUNT(*)
FROM device d
JOIN discovered_device dd ON d.discovered_device_id = dd.id
JOIN device_pairing dp ON d.id = dp.device_id
LEFT JOIN device_status ds ON d.id = ds.device_id
WHERE dp.pairing_status = 'PAIRED'
    AND d.deleted_at IS NULL
    AND d.org_id = ?
    AND (sqlc.narg('status_filter') is null OR FIND_IN_SET(ds.status, sqlc.narg('status_filter')))
    AND (sqlc.narg('type_filter') is null OR FIND_IN_SET(dd.type, sqlc.narg('type_filter')));

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

-- name: ListPairedMinersWithStatus :many
SELECT
    d.device_identifier,
    d.mac_address,
    d.serial_number,
    dd.model,
    dd.manufacturer,
    dd.type,
    ds.status as device_status,
    ds.status_timestamp,
    ds.status_details,
    dd.ip_address,
    dd.port,
    dd.url_scheme,
    dp.id as cursor_id,
    d.id as device_id
FROM device d
JOIN discovered_device dd ON d.discovered_device_id = dd.id
JOIN device_pairing dp ON d.id = dp.device_id
LEFT JOIN device_status ds ON d.id = ds.device_id
WHERE dp.pairing_status = 'PAIRED'
    AND d.deleted_at IS NULL
    AND d.org_id = ?
    AND (
        -- If cursor provided, filter by it, otherwise return all
        COALESCE(sqlc.narg('cursor_id'), 0) = 0
        OR
        (dp.id > sqlc.narg('cursor_id') OR (dp.id = sqlc.narg('cursor_id') AND d.id > sqlc.narg('device_cursor_id')))
    )
    AND (sqlc.narg('status_filter') is null OR FIND_IN_SET(ds.status, sqlc.narg('status_filter')))
    AND (sqlc.narg('type_filter') is null OR FIND_IN_SET(dd.type, sqlc.narg('type_filter')))
ORDER BY dp.id, d.id
LIMIT ?;

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

-- name: GetAllPairedDeviceIdentifiers :many
SELECT d.device_identifier
FROM device d
JOIN device_pairing dp ON d.id = dp.device_id
WHERE dp.pairing_status = 'PAIRED'
    AND d.deleted_at IS NULL;

-- name: CountMinersByState :one
SELECT
    COUNT(CASE WHEN ds.status = 'ACTIVE' THEN 1 END) as hashing_count,
    COUNT(CASE WHEN ds.status = 'INACTIVE' THEN 1 END) as idle_count,
    COUNT(CASE WHEN ds.status = 'ERROR' THEN 1 END) as broken_count,
    COUNT(CASE WHEN ds.status = 'OFFLINE' THEN 1 END) as offline_count,
    COUNT(CASE WHEN ds.status = 'MAINTENANCE' THEN 1 END) as sleeping_count
FROM device d
JOIN discovered_device dd ON d.discovered_device_id = dd.id
JOIN device_pairing dp ON d.id = dp.device_id
LEFT JOIN device_status ds ON d.id = ds.device_id
WHERE dp.pairing_status = 'PAIRED'
  AND d.deleted_at IS NULL
  AND d.org_id = ?
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
-- Uses a subquery to calculate pairing_status once, then filters on it
-- This keeps all pairing status logic in one place (the CASE statement)
SELECT
    device_identifier,
    mac_address,
    serial_number,
    model,
    manufacturer,
    type,
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
        END as pairing_status
    FROM discovered_device dd
    LEFT JOIN device d ON dd.id = d.discovered_device_id
        AND d.deleted_at IS NULL
        AND d.org_id = sqlc.arg('org_id')
    LEFT JOIN device_pairing dp ON d.id = dp.device_id
    LEFT JOIN device_status ds ON d.id = ds.device_id
    WHERE dd.org_id = sqlc.arg('org_id')
        AND dd.is_active = TRUE
        AND dd.deleted_at IS NULL
        -- Cursor pagination (applied early for performance)
        AND (
            COALESCE(sqlc.narg('cursor_id'), 0) = 0
            OR dd.id > sqlc.narg('cursor_id')
        )
        -- Status filter (only applies to paired devices with status)
        AND (
            sqlc.narg('status_filter') IS NULL
            OR ds.status IN (sqlc.slice('status_values'))
        )
        -- Type filter
        AND (
            sqlc.narg('type_filter') IS NULL
            OR dd.type IN (sqlc.slice('type_values'))
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
-- Uses same subquery pattern as ListMinerStateSnapshots for consistency
SELECT COUNT(*) as total
FROM (
    SELECT
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
    WHERE dd.org_id = sqlc.arg('org_id')
        AND dd.is_active = TRUE
        AND dd.deleted_at IS NULL
        -- Status filter (only applies to paired devices with status)
        AND (
            sqlc.narg('status_filter') IS NULL
            OR ds.status IN (sqlc.slice('status_values'))
        )
        -- Type filter
        AND (
            sqlc.narg('type_filter') IS NULL
            OR dd.type IN (sqlc.slice('type_values'))
        )
) AS devices_with_status
WHERE
    -- Pairing status filter - if no filter provided (NULL), return all
    (
        sqlc.narg('pairing_status_filter') IS NULL
        OR pairing_status IN (sqlc.slice('pairing_status_values'))
    );

