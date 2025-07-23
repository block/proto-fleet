-- name: GetDeviceWithCredentialsAndIPByDeviceIdentifier :one
SELECT 
    d.id,
    d.device_identifier,
    d.type,
    d.org_id,
    mc.username_enc,
    mc.password_enc,
    dia.ip_address,
    dia.port,
    dia.url_scheme,
    dp.pairing_token
FROM device d
JOIN device_pairing dp ON d.id = dp.device_id
LEFT JOIN miner_credentials mc ON d.id = mc.device_id
JOIN device_ip_assignment dia ON d.id = dia.device_id
WHERE d.device_identifier = ?
    AND d.deleted_at IS NULL
    AND dp.pairing_status = 'PAIRED'
    AND dia.is_current = TRUE
LIMIT 1;

-- name: GetDeviceWithCredentialsAndIPByID :one
SELECT 
    d.id,
    d.device_identifier,
    d.type,
    d.org_id,
    mc.username_enc,
    mc.password_enc,
    dia.ip_address,
    dia.port,
    dia.url_scheme,
    dp.pairing_token
FROM device d
JOIN device_pairing dp ON d.id = dp.device_id
LEFT JOIN miner_credentials mc ON d.id = mc.device_id
JOIN device_ip_assignment dia ON d.id = dia.device_id
WHERE d.id = ?
    AND d.deleted_at IS NULL
    AND dp.pairing_status = 'PAIRED'
    AND dia.is_current = TRUE
LIMIT 1;