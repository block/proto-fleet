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
-- Endpoint reconciliation is server-local; private addresses on nodes are separate namespaces.
SELECT dd.*
FROM discovered_device dd
WHERE dd.org_id = $1
    AND dd.ip_address = $2
    AND dd.port = $3
    AND dd.deleted_at IS NULL
    AND dd.discovered_by_fleet_node_id IS NULL
    AND NOT EXISTS (
        SELECT 1 FROM device d JOIN fleet_node_device fnd ON fnd.device_id=d.id AND fnd.org_id=d.org_id
        WHERE d.discovered_device_id=dd.id AND d.org_id=dd.org_id AND d.deleted_at IS NULL
    )
LIMIT 1;

-- name: FindDirectContainerByIP :many
-- Container identity follows its shared IP, independently of either service port.
-- Return two candidates so callers can reject ambiguous existing inventory.
SELECT dd.*
FROM discovered_device dd
WHERE dd.org_id = sqlc.arg('org_id')
    AND dd.ip_address = sqlc.arg('ip_address')
    AND dd.device_class = 2
    AND dd.driver_name = sqlc.arg('driver_name')::text
    AND lower(dd.model) = lower(sqlc.arg('model')::text)
    AND lower(dd.manufacturer) = lower(sqlc.arg('manufacturer')::text)
    AND dd.deleted_at IS NULL
    AND dd.discovered_by_fleet_node_id IS NULL
    AND NOT EXISTS (
        SELECT 1 FROM device d JOIN fleet_node_device fnd ON fnd.device_id=d.id AND fnd.org_id=d.org_id
        WHERE d.discovered_device_id=dd.id AND d.org_id=dd.org_id AND d.deleted_at IS NULL
    )
LIMIT 2;

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
    driver_name,
    device_class,
    rpc_port,
    rpc_tls
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
    $10,
    $11, sqlc.arg('rpc_port'), sqlc.arg('rpc_tls')
)
ON CONFLICT (org_id, device_identifier) WHERE deleted_at IS NULL DO UPDATE SET
    model = EXCLUDED.model,
    device_class = COALESCE(NULLIF(EXCLUDED.device_class, 0), discovered_device.device_class),
    manufacturer = EXCLUDED.manufacturer,
    ip_address = EXCLUDED.ip_address,
    -- Native CT1 rediscovery must retain the existing browser endpoint.
    port = CASE WHEN discovered_device.device_class = 2 AND EXCLUDED.rpc_port > 0
                THEN discovered_device.port ELSE EXCLUDED.port END,
    url_scheme = CASE WHEN discovered_device.device_class = 2 AND EXCLUDED.rpc_port > 0
                      THEN discovered_device.url_scheme ELSE EXCLUDED.url_scheme END,
    rpc_port = EXCLUDED.rpc_port,
    rpc_tls = EXCLUDED.rpc_tls,
    firmware_version = EXCLUDED.firmware_version,
    is_active = EXCLUDED.is_active,
    -- Keep existing driver_name if already set (prevent discovery flip-flop)
    driver_name = COALESCE(discovered_device.driver_name, EXCLUDED.driver_name),
    last_seen = CURRENT_TIMESTAMP
WHERE NOT sqlc.arg('direct_discovery')::boolean OR (
    discovered_device.discovered_by_fleet_node_id IS NULL
    AND NOT EXISTS (
        SELECT 1 FROM device d JOIN fleet_node_device fnd ON fnd.device_id=d.id AND fnd.org_id=d.org_id
        WHERE d.discovered_device_id=discovered_device.id AND d.org_id=discovered_device.org_id AND d.deleted_at IS NULL
    )
)
RETURNING id;

-- name: GetActiveUnpairedDiscoveredDevices :many
-- Excludes remote-fleet-node-reported rows: server-local pairing
-- dials these IPs, agent-reported rows route via PairDeviceToFleetNode.
SELECT dd.id, dd.org_id, dd.device_identifier, dd.model, dd.manufacturer,
       dd.firmware_version, dd.ip_address, dd.port, dd.url_scheme, dd.rpc_port, dd.rpc_tls, dd.discovery_metadata,
       dd.first_discovered, dd.last_seen, dd.is_active,
       dd.driver_name, dd.device_class,
       dd.created_at, dd.updated_at, dd.deleted_at
FROM discovered_device dd
LEFT JOIN device d ON dd.id = d.discovered_device_id AND d.deleted_at IS NULL
WHERE dd.org_id = $1
    AND dd.is_active = TRUE
    AND dd.deleted_at IS NULL
    AND d.id IS NULL
    AND dd.discovered_by_fleet_node_id IS NULL
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
    AND d.id IS NULL
    AND dd.discovered_by_fleet_node_id IS NULL;

-- name: SoftDeleteDiscoveredDeviceByIdentifier :exec
-- Soft-deletes a discovered_device record. Used to clean up orphaned records
-- after device reconciliation during subnet migration.
UPDATE discovered_device
SET deleted_at = CURRENT_TIMESTAMP
WHERE device_identifier = $1
  AND org_id = $2
  AND deleted_at IS NULL;

-- name: UpdateDiscoveredDeviceFirmwareVersion :exec
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
  );
