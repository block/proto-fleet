-- name: GetDiscoveredDeviceByID :one
SELECT *
FROM discovered_device
WHERE id = $1
    AND org_id = $2
    AND deleted_at IS NULL
LIMIT 1;

-- name: GetDiscoveredDeviceByDeviceIdentifier :one
SELECT *
FROM discovered_device
WHERE device_identifier = $1
    AND org_id = $2
    AND deleted_at IS NULL
LIMIT 1;

-- name: GetDiscoveredDeviceByIPAndPort :one
SELECT *
FROM discovered_device
WHERE org_id = $1
    AND ip_address = $2
    AND port = $3
    AND deleted_at IS NULL
LIMIT 1;

-- name: UpsertDiscoveredDevice :one
-- PostgreSQL version returns the id directly using RETURNING
INSERT INTO discovered_device (
    org_id,
    device_identifier,
    model,
    manufacturer,
    firmware_version,
    ip_address,
    port,
    url_scheme,
    is_active,
    driver_name
) VALUES (
    $1,
    $2,
    $3,
    $4,
    $5,
    $6,
    $7,
    $8,
    $9,
    $10
)
ON CONFLICT (org_id, device_identifier) WHERE deleted_at IS NULL DO UPDATE SET
    model = EXCLUDED.model,
    manufacturer = EXCLUDED.manufacturer,
    ip_address = EXCLUDED.ip_address,
    port = EXCLUDED.port,
    url_scheme = EXCLUDED.url_scheme,
    firmware_version = EXCLUDED.firmware_version,
    is_active = EXCLUDED.is_active,
    -- Keep existing driver_name if already set (prevent discovery flip-flop)
    driver_name = COALESCE(discovered_device.driver_name, EXCLUDED.driver_name),
    last_seen = CURRENT_TIMESTAMP
RETURNING id;

-- name: GetActiveUnpairedDiscoveredDevices :many
SELECT dd.id, dd.org_id, dd.device_identifier, dd.model, dd.manufacturer,
       dd.firmware_version, dd.ip_address, dd.port, dd.url_scheme, dd.discovery_metadata,
       dd.first_discovered, dd.last_seen, dd.is_active,
       dd.driver_name,
       dd.created_at, dd.updated_at, dd.deleted_at
FROM discovered_device dd
LEFT JOIN device d ON dd.id = d.discovered_device_id AND d.deleted_at IS NULL
WHERE dd.org_id = $1
    AND dd.is_active = TRUE
    AND dd.deleted_at IS NULL
    AND d.id IS NULL
    AND (
        -- If cursor provided, filter by it, otherwise return all
        COALESCE(sqlc.narg('cursor_id'), 0) = 0
        OR dd.id > sqlc.narg('cursor_id')
    )
ORDER BY dd.id
LIMIT $2;

-- name: CountActiveUnpairedDiscoveredDevices :one
SELECT COUNT(*) as total
FROM discovered_device dd
LEFT JOIN device d ON dd.id = d.discovered_device_id AND d.deleted_at IS NULL
WHERE dd.org_id = $1
    AND dd.is_active = TRUE
    AND dd.deleted_at IS NULL
    AND d.id IS NULL;

-- name: SoftDeleteDiscoveredDeviceByIdentifier :exec
-- Soft-deletes a discovered_device record. Used to clean up orphaned records
-- after device reconciliation during subnet migration.
UPDATE discovered_device
SET deleted_at = CURRENT_TIMESTAMP
WHERE device_identifier = $1
  AND org_id = $2
  AND deleted_at IS NULL;

-- name: UpdateDiscoveredDeviceFirmwareVersion :one
UPDATE discovered_device dd
SET firmware_version = $2
WHERE dd.device_identifier = $1
  AND dd.deleted_at IS NULL
  AND dd.firmware_version IS DISTINCT FROM $2
  AND dd.org_id = (
    SELECT d.org_id
    FROM device d
    WHERE d.device_identifier = $1
      AND d.deleted_at IS NULL
    LIMIT 1
  )
RETURNING dd.org_id;
