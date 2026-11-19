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
WITH discovery_mode AS (SELECT sqlc.arg('direct_discovery')::boolean AS direct)
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
    rpc_tls,
    serial_number,
    mac_address, endpoint_key, endpoints
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
    $11, sqlc.arg('rpc_port'), sqlc.arg('rpc_tls'),
    $12,
    $13, sqlc.arg(endpoint_key), COALESCE(sqlc.arg(endpoints)::jsonb, '{}'::jsonb)
)
ON CONFLICT (org_id, device_identifier) WHERE deleted_at IS NULL DO UPDATE SET
    model = EXCLUDED.model,
    manufacturer = EXCLUDED.manufacturer,
    device_class = COALESCE(NULLIF(EXCLUDED.device_class, 0), discovered_device.device_class),
    -- Metadata saves keep the current grouped routes. Only discovery replaces them.
    (ip_address, port, url_scheme, rpc_port, rpc_tls, endpoint_key, endpoints, serial_number, mac_address) = (
        SELECT discovered_device.ip_address, discovered_device.port, discovered_device.url_scheme,
               discovered_device.rpc_port, discovered_device.rpc_tls, discovered_device.endpoint_key, discovered_device.endpoints,
               CASE WHEN discovered_device.endpoint_key = EXCLUDED.endpoint_key AND discovered_device.endpoints = EXCLUDED.endpoints
                    THEN EXCLUDED.serial_number ELSE discovered_device.serial_number END,
               CASE WHEN discovered_device.endpoint_key = EXCLUDED.endpoint_key AND discovered_device.endpoints = EXCLUDED.endpoints
                    THEN EXCLUDED.mac_address ELSE discovered_device.mac_address END
        WHERE NOT (SELECT direct FROM discovery_mode) AND discovered_device.endpoint_key <> ''
        UNION ALL
        SELECT EXCLUDED.ip_address,
               -- Native rediscovery of an ungrouped container keeps its web settings.
               CASE WHEN discovered_device.device_class = 2 AND EXCLUDED.rpc_port > 0
                         AND discovered_device.endpoint_key = '' AND EXCLUDED.endpoint_key = ''
                    THEN discovered_device.port ELSE EXCLUDED.port END,
               CASE WHEN discovered_device.device_class = 2 AND EXCLUDED.rpc_port > 0
                         AND discovered_device.endpoint_key = '' AND EXCLUDED.endpoint_key = ''
                    THEN discovered_device.url_scheme ELSE EXCLUDED.url_scheme END,
               EXCLUDED.rpc_port, EXCLUDED.rpc_tls, EXCLUDED.endpoint_key, EXCLUDED.endpoints,
               EXCLUDED.serial_number, EXCLUDED.mac_address
        WHERE (SELECT direct FROM discovery_mode) OR discovered_device.endpoint_key = ''
    ),
    firmware_version = EXCLUDED.firmware_version,
    is_active = EXCLUDED.is_active,
    -- Keep existing driver_name if already set (prevent discovery flip-flop)
    driver_name = COALESCE(discovered_device.driver_name, EXCLUDED.driver_name),
    last_seen = CURRENT_TIMESTAMP
WHERE ((NOT sqlc.arg('direct_discovery')::boolean AND discovered_device.endpoint_key <> '' AND discovered_device.driver_name = EXCLUDED.driver_name)
    OR discovered_device.endpoint_key = ''
    OR (discovered_device.endpoint_key = EXCLUDED.endpoint_key AND discovered_device.driver_name = EXCLUDED.driver_name))
-- A web-only scan cannot erase a saved native RPC route.
AND (NOT sqlc.arg('direct_discovery')::boolean OR EXCLUDED.endpoint_key <> '' OR EXCLUDED.rpc_port > 0
    OR (discovered_device.rpc_port = 0 AND NOT discovered_device.rpc_tls))
AND (NOT sqlc.arg('direct_discovery')::boolean OR (
    discovered_device.discovered_by_fleet_node_id IS NULL
    AND NOT EXISTS (
        SELECT 1 FROM device d JOIN fleet_node_device fnd ON fnd.device_id=d.id AND fnd.org_id=d.org_id
        WHERE d.discovered_device_id=discovered_device.id AND d.org_id=discovered_device.org_id AND d.deleted_at IS NULL
    )
))
RETURNING id;

-- name: ActivateDirectDiscoveredDevice :execrows
UPDATE discovered_device dd SET is_active = TRUE, last_seen = CURRENT_TIMESTAMP
WHERE dd.org_id = sqlc.arg(org_id) AND dd.device_identifier = sqlc.arg(device_identifier)
AND dd.deleted_at IS NULL AND dd.discovered_by_fleet_node_id IS NULL
AND NOT EXISTS (
    SELECT 1 FROM device d JOIN fleet_node_device fnd ON fnd.device_id = d.id AND fnd.org_id = d.org_id
    WHERE d.discovered_device_id = dd.id AND d.org_id = dd.org_id AND d.deleted_at IS NULL
);

-- name: CountActiveUnpairedDiscoveredDevices :one
-- Excludes remote-fleet-node-reported rows: server-local pairing
-- dials these IPs, agent-reported rows route via PairDeviceToFleetNode.
SELECT COUNT(*) as total
FROM discovered_device dd
LEFT JOIN device d ON dd.id = d.discovered_device_id AND d.deleted_at IS NULL
WHERE dd.org_id = $1 AND dd.is_active = TRUE AND dd.deleted_at IS NULL
    AND d.id IS NULL AND dd.discovered_by_fleet_node_id IS NULL;

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

-- name: LockEndpointDiscovery :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(lock_key)::text, 0));

-- name: LockEndpointDiscoveryDevices :many
-- Pairing locks device rows before writing their discovered inventory.
SELECT d.id FROM device d
WHERE d.org_id = sqlc.arg(org_id) AND d.discovered_device_id = sqlc.arg(discovered_device_id)
AND d.deleted_at IS NULL
ORDER BY d.id FOR UPDATE;

-- name: LockDiscoveredDeviceForEndpointDiscovery :one
-- FOR UPDATE also waits for an in-flight first pairing's foreign-key lock.
SELECT dd.* FROM discovered_device dd
WHERE dd.org_id = sqlc.arg(org_id) AND dd.id = sqlc.arg(id) AND dd.deleted_at IS NULL
FOR UPDATE;

-- name: EndpointDiscoveryHasPairing :one
-- Read after taking inventory locks so a pairing that won the wait is visible.
SELECT EXISTS (
    SELECT 1 FROM device d
    WHERE d.org_id = sqlc.arg(org_id) AND d.discovered_device_id = sqlc.arg(discovered_device_id)
    AND d.deleted_at IS NULL
    AND (EXISTS (
        SELECT 1 FROM device_pairing dp WHERE dp.device_id = d.id
        AND dp.pairing_status IN ('PAIRED', 'DEFAULT_PASSWORD', 'AUTHENTICATION_NEEDED')
    ) OR EXISTS (
        SELECT 1 FROM fleet_node_device fnd WHERE fnd.device_id = d.id AND fnd.org_id = d.org_id
    ))
);

-- name: FindEndpointDiscovery :many
SELECT dd.* FROM discovered_device dd
WHERE dd.org_id = sqlc.arg(org_id) AND dd.deleted_at IS NULL
AND dd.driver_name = sqlc.arg(driver_name)::text
AND (dd.discovered_by_fleet_node_id IS NOT DISTINCT FROM sqlc.narg(fleet_node_id)::bigint
    OR (sqlc.narg(fleet_node_id)::bigint IS NOT NULL
        AND dd.discovered_by_fleet_node_id IS NOT NULL
        AND NOT EXISTS (
            SELECT 1 FROM fleet_node fn
            WHERE fn.id = dd.discovered_by_fleet_node_id AND fn.org_id = dd.org_id AND fn.deleted_at IS NULL
        )))
AND (dd.endpoint_key = sqlc.arg(group_key) OR
    (dd.endpoint_key = '' AND dd.ip_address = sqlc.arg(scanned_host)::text
        AND dd.rpc_port = sqlc.arg(scanned_rpc_port)::integer
        AND (sqlc.arg(scanned_rpc_port)::integer <> 0 OR dd.port = sqlc.arg(scanned_port)::integer::text)) OR
    (dd.endpoint_key <> '' AND EXISTS (
        SELECT 1 FROM jsonb_each(CASE WHEN jsonb_typeof(dd.endpoints)='object' THEN dd.endpoints ELSE '{}'::jsonb END) endpoint
        WHERE endpoint.value->>'Host' = sqlc.arg(scanned_host)::text
          AND endpoint.value->>'RPCPort' = sqlc.arg(scanned_rpc_port)::integer::text
          AND (sqlc.arg(scanned_rpc_port)::integer <> 0 OR endpoint.value->>'Port' = sqlc.arg(scanned_port)::integer::text)
    )))
AND NOT EXISTS (
 SELECT 1 FROM device d JOIN fleet_node_device fnd ON fnd.device_id=d.id AND fnd.org_id=d.org_id
 WHERE d.discovered_device_id=dd.id AND d.org_id=dd.org_id AND d.deleted_at IS NULL
 AND fnd.fleet_node_id IS DISTINCT FROM sqlc.narg(fleet_node_id)::bigint
)
ORDER BY (dd.discovered_by_fleet_node_id IS NOT DISTINCT FROM sqlc.narg(fleet_node_id)::bigint) DESC, dd.id LIMIT 2;

-- name: EndpointDiscoveryHasSavedGroupAtSocket :one
-- A keyless fallback must not create another row for a known group's peer.
SELECT EXISTS (
    SELECT 1 FROM discovered_device dd
    WHERE dd.org_id=sqlc.arg(org_id) AND dd.driver_name=sqlc.arg(driver_name)::text
    AND dd.deleted_at IS NULL AND dd.endpoint_key <> ''
    AND dd.discovered_by_fleet_node_id IS NOT DISTINCT FROM sqlc.narg(fleet_node_id)::bigint
    AND NOT EXISTS (
        SELECT 1 FROM device d JOIN fleet_node_device fnd ON fnd.device_id=d.id AND fnd.org_id=d.org_id
        WHERE d.discovered_device_id=dd.id AND d.org_id=dd.org_id AND d.deleted_at IS NULL
        AND fnd.fleet_node_id IS DISTINCT FROM sqlc.narg(fleet_node_id)::bigint
    )
    AND ((dd.ip_address=sqlc.arg(host)::text AND dd.rpc_port=sqlc.arg(rpc_port)::integer
          AND (sqlc.arg(rpc_port)::integer <> 0 OR dd.port=sqlc.arg(port)::text))
      OR EXISTS (
        SELECT 1 FROM jsonb_each(CASE WHEN jsonb_typeof(dd.endpoints)='object' THEN dd.endpoints ELSE '{}'::jsonb END) endpoint
        WHERE endpoint.value->>'Host'=sqlc.arg(host)::text
        AND endpoint.value->>'RPCPort'=sqlc.arg(rpc_port)::integer::text
        AND (sqlc.arg(rpc_port)::integer <> 0 OR endpoint.value->>'Port'=sqlc.arg(port)::text)
      ))
);
