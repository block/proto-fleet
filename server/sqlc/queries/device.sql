-- name: UpsertDevice :execresult
INSERT INTO device (
    org_id,
    device_identifier,
    mac_address,
    serial_number,
    model,
    manufacturer,
    type,
    is_active
) VALUES (
    ?,
    ?,
    ?,
    ?,
    ?,
    ?,
    ?,
    ?
)
ON DUPLICATE KEY UPDATE
    serial_number = VALUES(serial_number),
    is_active = VALUES(is_active),
    last_seen = CURRENT_TIMESTAMP(6),
    deleted_at = NULL,
    model = VALUES(model),
    manufacturer = VALUES(manufacturer),
    type = VALUES(type),
    org_id = VALUES(org_id),
    id = LAST_INSERT_ID(id);

-- name: GetDeviceByIdentifier :one
SELECT id, device_identifier
FROM device
WHERE device_identifier = ?
    AND org_id = ?
LIMIT 1;

-- name: CreateInactiveDeviceIPAssignment :exec
INSERT INTO device_ip_assignment (
    device_id,
    ip_address,
    port,
    is_current
) VALUES (
    ?,
    ?,
    ?,
    FALSE
);

-- name: ActivateNewIPAssignment :exec
WITH params AS (
  SELECT
    ?  AS ip_address,
    ?  AS port,
    ?  AS device_id
)
UPDATE device_ip_assignment AS d
JOIN params AS p ON TRUE
SET
  d.is_current = (d.ip_address = p.ip_address AND d.port = p.port),
  d.assigned_at = CASE
    WHEN d.ip_address = p.ip_address
     AND d.port       = p.port
    THEN CURRENT_TIMESTAMP(6)
    ELSE d.assigned_at
  END,
  d.unassigned_at = CASE
    WHEN d.is_current = TRUE
     AND NOT (d.ip_address = p.ip_address AND d.port = p.port)
    THEN CURRENT_TIMESTAMP(6)
    ELSE d.unassigned_at
  END
WHERE d.device_id = p.device_id
  AND (d.is_current = TRUE
       OR (d.ip_address = p.ip_address AND d.port = p.port));

-- name: GetActiveDeviceIPAssignmentByDeviceID :one
SELECT *
FROM device_ip_assignment
WHERE device_id = ?
    AND is_current = TRUE
LIMIT 1;

-- name: ListPairedDevices :many
SELECT
    d.device_identifier,
    d.mac_address,
    d.serial_number,
    d.model,
    d.manufacturer,
    d.type,
    dp.id as cursor_id,
    d.id as device_id
FROM device d
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

-- name: GetTotalPairedDevices :one
SELECT COUNT(*)
FROM device d
JOIN device_pairing dp ON d.id = dp.device_id
WHERE dp.pairing_status = 'PAIRED'
    AND d.deleted_at IS NULL;

-- name: UpsertDevicePairing :execresult
INSERT INTO device_pairing (
    device_id,
    pairing_token,
    pairing_status,
    paired_at
) VALUES (
    ?,
    ?,
    ?,
    CURRENT_TIMESTAMP(6)
)
ON DUPLICATE KEY UPDATE
    pairing_status = VALUES(pairing_status),
    pairing_token = VALUES(pairing_token),
    paired_at = CURRENT_TIMESTAMP(6),
    unpaired_at = NULL;

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

-- name: GetMinerApiNetworkInfoByDeviceID :one
SELECT
    d.device_identifier,
    d.org_id,
    dia.ip_address,
    dia.port
FROM device d
JOIN device_pairing dp ON d.id = dp.device_id
JOIN device_ip_assignment dia ON d.id = dia.device_id
WHERE d.id = ?
    AND d.deleted_at IS NULL
    AND dp.pairing_status = 'PAIRED'
    AND dia.is_current = TRUE
LIMIT 1;

-- name: ListPairedMinersWithStatus :many
SELECT
    d.device_identifier,
    d.mac_address,
    d.serial_number,
    d.model,
    d.manufacturer,
    d.type,
    ds.status as device_status,
    ds.status_timestamp,
    ds.status_details,
    dia.ip_address,
    dia.port,
    dp.id as cursor_id,
    d.id as device_id
FROM device d
JOIN device_pairing dp ON d.id = dp.device_id
LEFT JOIN device_status ds ON d.id = ds.device_id
LEFT JOIN device_ip_assignment dia ON d.id = dia.device_id AND dia.is_current = TRUE
WHERE dp.pairing_status = 'PAIRED'
    AND d.deleted_at IS NULL
    AND d.org_id = ?
    AND (
        -- If cursor provided, filter by it, otherwise return all
        COALESCE(sqlc.narg('cursor_id'), 0) = 0
        OR
        (dp.id > sqlc.narg('cursor_id') OR (dp.id = sqlc.narg('cursor_id') AND d.id > sqlc.narg('device_cursor_id')))
    )
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
