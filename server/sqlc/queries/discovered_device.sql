-- name: GetDiscoveredDeviceByID :one
SELECT *
FROM discovered_device
WHERE id = ?
    AND org_id = ?
    AND deleted_at IS NULL
LIMIT 1;

-- name: GetDiscoveredDeviceByDeviceIdentifier :one
SELECT *
FROM discovered_device
WHERE device_identifier = ?
    AND org_id = ?
    AND deleted_at IS NULL
LIMIT 1;

-- name: UpsertDiscoveredDevice :execresult
INSERT INTO discovered_device (
    org_id,
    device_identifier,
    model,
    manufacturer,
    type,
    ip_address,
    port,
    url_scheme,
    is_active
) VALUES (
    ?,
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
    ip_address = VALUES(ip_address),
    port = VALUES(port),
    url_scheme = VALUES(url_scheme),
    is_active = VALUES(is_active),
    last_seen = CURRENT_TIMESTAMP(6),
    id = LAST_INSERT_ID(id);
