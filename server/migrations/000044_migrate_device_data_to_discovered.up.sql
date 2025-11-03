-- Insert existing devices into discovered_device with their current IP assignments
-- Only migrate devices that have valid IP assignments (INNER JOIN ensures no NULLs)
INSERT INTO discovered_device (
    id,
    org_id,
    device_identifier,
    model,
    manufacturer,
    type,
    ip_address,
    port,
    url_scheme,
    first_discovered,
    last_seen,
    is_active,
    created_at,
    updated_at,
    deleted_at
)
SELECT
    d.id,
    d.org_id,
    d.device_identifier,
    d.model,
    d.manufacturer,
    d.type,
    dia.ip_address,
    dia.port,
    dia.url_scheme,
    d.first_discovered,
    d.last_seen,
    d.is_active,
    d.created_at,
    d.updated_at,
    d.deleted_at
FROM device d
INNER JOIN device_ip_assignment dia ON d.id = dia.device_id AND dia.is_current = TRUE;
