CREATE VIEW v_current_device_ip AS
SELECT 
    d.id,
    d.device_identifier,
    d.mac_address,
    d.serial_number,
    dia.ip_address
FROM device d
LEFT JOIN device_ip_assignment dia ON d.id = dia.device_id
WHERE dia.is_current = TRUE AND d.is_active = TRUE;