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

-- name: GetDiscoveredDeviceByIPAndPort :one
SELECT *
FROM discovered_device
WHERE org_id = ?
    AND ip_address = ?
    AND port = ?
    AND deleted_at IS NULL
LIMIT 1;

-- name: UpsertDiscoveredDevice :execresult
INSERT INTO discovered_device (
    org_id,
    device_identifier,
    model,
    manufacturer,
    type,
    firmware_version,
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
    ?,
    ?
)
ON DUPLICATE KEY UPDATE
    ip_address = VALUES(ip_address),
    port = VALUES(port),
    url_scheme = VALUES(url_scheme),
    firmware_version = VALUES(firmware_version),
    last_seen = CURRENT_TIMESTAMP(6),
    id = LAST_INSERT_ID(id);

-- name: GetActiveUnpairedDiscoveredDevices :many
SELECT dd.id, dd.org_id, dd.device_identifier, dd.model, dd.manufacturer,
       dd.type, dd.firmware_version, dd.ip_address, dd.port, dd.url_scheme, dd.discovery_metadata,
       dd.first_discovered, dd.last_seen, dd.is_active,
       dd.created_at, dd.updated_at, dd.deleted_at
FROM discovered_device dd
LEFT JOIN device d ON dd.id = d.discovered_device_id AND d.deleted_at IS NULL
WHERE dd.org_id = ?
    AND dd.is_active = TRUE
    AND dd.deleted_at IS NULL
    AND d.id IS NULL
    AND (
        -- If cursor provided, filter by it, otherwise return all
        COALESCE(sqlc.narg('cursor_id'), 0) = 0
        OR dd.id > sqlc.narg('cursor_id')
    )
ORDER BY dd.id
LIMIT ?;

-- name: CountActiveUnpairedDiscoveredDevices :one
SELECT COUNT(*) as total
FROM discovered_device dd
LEFT JOIN device d ON dd.id = d.discovered_device_id AND d.deleted_at IS NULL
WHERE dd.org_id = ?
    AND dd.is_active = TRUE
    AND dd.deleted_at IS NULL
    AND d.id IS NULL;
