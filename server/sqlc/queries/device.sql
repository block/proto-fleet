-- name: UpsertDevice :execresult
INSERT INTO device (
    device_identifier,
    mac_address,
    serial_number,
    is_active
) VALUES (
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
    id = LAST_INSERT_ID(id);

-- name: GetDeviceByIdentifier :one
SELECT id, device_identifier
FROM device
WHERE device_identifier = ?
LIMIT 1;

-- name: UpsertDeviceIPAssignment :execresult
INSERT INTO device_ip_assignment (
    device_id,
    ip_address,
    port,
    is_current
) VALUES (
    ?,
    ?,
    ?,
    TRUE
)
ON DUPLICATE KEY UPDATE
    unassigned_at = NULL,
    is_current = TRUE;

-- name: DeactivateOldIPAssignments :exec
UPDATE device_ip_assignment
SET
    is_current = FALSE,
    unassigned_at = CURRENT_TIMESTAMP(6)
WHERE device_id = ?
    AND ip_address != ?
    AND port != ?
    AND is_current = TRUE;

-- name: ListPairedDevices :many
SELECT
    d.device_identifier,
    d.mac_address,
    d.serial_number,
    dp.id as cursor_id,
    d.id as device_id
FROM device d
JOIN device_pairing dp ON d.id = dp.device_id
WHERE dp.pairing_status = 'PAIRED'
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
AND deleted_at IS NULL
LIMIT 1;

-- name: GetDeviceByDeviceIdentifier :one
SELECT *
FROM device
WHERE device_identifier = ?
  AND deleted_at IS NULL
    LIMIT 1;
