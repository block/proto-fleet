-- Owner-privilege boolean view for the protofleet-ingest-stalled alert: mirrors GetAllPairedDeviceIdentifiers (device.sql) so grafana_ro can gate on paired-device presence without SELECT on device/device_pairing (pairing_token).
CREATE VIEW fleet_pollable_device_presence AS
SELECT EXISTS (
    SELECT 1
    FROM device d
    JOIN device_pairing dp ON d.id = dp.device_id
    WHERE dp.pairing_status IN ('PAIRED', 'DEFAULT_PASSWORD')
        AND d.deleted_at IS NULL
        AND NOT EXISTS (
            SELECT 1 FROM fleet_node_device fnd
            WHERE fnd.device_id = d.id AND fnd.org_id = d.org_id
        )
) AS has_pollable_device;
