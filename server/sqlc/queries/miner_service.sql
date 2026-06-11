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
    AND dp.pairing_status = 'PAIRED'
    -- Cloud dials this device directly, so exclude fleet-node-owned devices:
    -- the node owns their I/O and the cloud has no route to them.
    AND NOT EXISTS (
        SELECT 1 FROM fleet_node_device fnd
        WHERE fnd.device_id = d.id AND fnd.org_id = d.org_id
    )
LIMIT 1;

-- name: GetDeviceWithCredentialsAndIPByID :one
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
WHERE d.id = $1
    AND d.deleted_at IS NULL
    AND dp.pairing_status = 'PAIRED'
    -- Cloud dials this device directly, so exclude fleet-node-owned devices:
    -- the node owns their I/O and the cloud has no route to them.
    AND NOT EXISTS (
        SELECT 1 FROM fleet_node_device fnd
        WHERE fnd.device_id = d.id AND fnd.org_id = d.org_id
    )
LIMIT 1;
