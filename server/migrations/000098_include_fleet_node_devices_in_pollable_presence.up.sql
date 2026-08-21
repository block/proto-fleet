CREATE OR REPLACE VIEW fleet_pollable_device_presence AS
SELECT DISTINCT organization_id
FROM (
    SELECT d.org_id::text AS organization_id
    FROM device d
    JOIN device_pairing dp ON d.id = dp.device_id
    WHERE dp.pairing_status IN ('PAIRED', 'DEFAULT_PASSWORD')
        AND d.deleted_at IS NULL
        AND NOT EXISTS (
            SELECT 1 FROM fleet_node_device fnd
            WHERE fnd.device_id = d.id AND fnd.org_id = d.org_id
        )

    UNION ALL

    SELECT d.org_id::text AS organization_id
    FROM device d
    JOIN fleet_node_device fnd ON fnd.device_id = d.id AND fnd.org_id = d.org_id
    JOIN device_pairing dp ON dp.device_id = fnd.device_id
    JOIN fleet_node fn ON fn.id = fnd.fleet_node_id AND fn.org_id = fnd.org_id
    WHERE d.deleted_at IS NULL
        AND dp.pairing_status IN ('PAIRED', 'DEFAULT_PASSWORD')
        AND fn.deleted_at IS NULL
        AND fn.enrollment_status = 'CONFIRMED'
) pollable;
