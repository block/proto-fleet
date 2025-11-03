CREATE VIEW v_latest_device_status AS
SELECT 
    d.id,
    d.device_identifier,
    d.mac_address,
    d.serial_number,
    ds.status,
    ds.status_timestamp,
    ds.status_details
FROM device d
LEFT JOIN device_status ds ON d.id = ds.device_id
WHERE ds.status_timestamp = (
    SELECT MAX(status_timestamp)
    FROM device_status
    WHERE device_id = d.id
);
