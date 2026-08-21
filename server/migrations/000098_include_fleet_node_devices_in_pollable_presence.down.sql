CREATE OR REPLACE VIEW fleet_pollable_device_presence AS
SELECT DISTINCT d.org_id::text AS organization_id
FROM device d
JOIN device_pairing dp ON d.id = dp.device_id
WHERE dp.pairing_status IN ('PAIRED', 'DEFAULT_PASSWORD')
    AND d.deleted_at IS NULL
    AND NOT EXISTS (
        SELECT 1 FROM fleet_node_device fnd
        WHERE fnd.device_id = d.id AND fnd.org_id = d.org_id
    );
