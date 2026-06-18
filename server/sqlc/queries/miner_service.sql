-- name: GetDeviceWithCredentialsAndIPByDeviceIdentifier :one
SELECT
    d.id,
    d.device_identifier,
    dd.model,
    dd.manufacturer,
    dd.driver_name,
    d.org_id,
    d.serial_number,
    d.mac_address,
    mc.username_enc,
    mc.password_enc,
    dd.ip_address,
    dd.port,
    dd.url_scheme,
    d.site_id
FROM device d
JOIN discovered_device dd ON d.discovered_device_id = dd.id
JOIN device_pairing dp ON d.id = dp.device_id
LEFT JOIN miner_credentials mc ON d.id = mc.device_id
WHERE d.device_identifier = $1
    AND d.deleted_at IS NULL
    AND dp.pairing_status IN ('PAIRED', 'DEFAULT_PASSWORD')
    -- Cloud dials this device directly, so exclude fleet-node-owned devices:
    -- the node owns their I/O and the cloud has no route to them.
    AND NOT EXISTS (
        SELECT 1 FROM fleet_node_device fnd
        WHERE fnd.device_id = d.id AND fnd.org_id = d.org_id
    )
LIMIT 1;

-- name: GetActiveFleetNodeForDevice :one
-- Resolve the active fleet node a device is paired to, with the connection
-- coordinates the node needs to reach the LAN miner. The miner service calls this
-- first so commands for a fleet-node-paired device route over the ControlStream
-- instead of being dialed directly. Requires a paired-like pairing status so a
-- device merely bound to a node but not yet paired/authenticated cannot receive
-- commands. Returns no rows otherwise, so cloud-dialed and not-yet-paired
-- devices fall through to the direct path.
SELECT
    fnd.fleet_node_id,
    d.org_id,
    d.site_id,
    d.device_identifier,
    d.serial_number,
    d.mac_address,
    dd.driver_name,
    dd.ip_address,
    dd.port,
    dd.url_scheme
FROM fleet_node_device fnd
JOIN device d ON d.id = fnd.device_id AND d.org_id = fnd.org_id AND d.deleted_at IS NULL
JOIN device_pairing dp ON dp.device_id = d.id
JOIN fleet_node fn ON fn.id = fnd.fleet_node_id AND fn.org_id = fnd.org_id
JOIN discovered_device dd ON dd.id = d.discovered_device_id
WHERE d.device_identifier = $1
    AND dp.pairing_status IN ('PAIRED', 'DEFAULT_PASSWORD')
    AND fn.deleted_at IS NULL
    AND fn.enrollment_status = 'CONFIRMED'
LIMIT 1;
