-- name: GetDeviceWithCredentialsAndIPByDeviceIdentifier :one
SELECT
    d.id,
    d.device_identifier,
    dd.type,
    d.org_id,
    d.serial_number,
    mc.username_enc,
    mc.password_enc,
    dd.ip_address,
    dd.port,
    dd.url_scheme
FROM device d
JOIN discovered_device dd ON d.discovered_device_id = dd.id
JOIN device_pairing dp ON d.id = dp.device_id
LEFT JOIN miner_credentials mc ON d.id = mc.device_id
WHERE d.device_identifier = ?
    AND d.deleted_at IS NULL
    AND dp.pairing_status = 'PAIRED'
LIMIT 1;

-- name: GetDeviceWithCredentialsAndIPByID :one
SELECT
    d.id,
    d.device_identifier,
    dd.type,
    d.org_id,
    d.serial_number,
    mc.username_enc,
    mc.password_enc,
    dd.ip_address,
    dd.port,
    dd.url_scheme
FROM device d
JOIN discovered_device dd ON d.discovered_device_id = dd.id
JOIN device_pairing dp ON d.id = dp.device_id
LEFT JOIN miner_credentials mc ON d.id = mc.device_id
WHERE d.id = ?
    AND d.deleted_at IS NULL
    AND dp.pairing_status = 'PAIRED'
LIMIT 1;
