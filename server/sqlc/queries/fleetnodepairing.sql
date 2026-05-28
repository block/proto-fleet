-- name: UpsertDiscoveredDeviceFromFleetNode :execrows
-- 0 rows on conflict signals rejection. Blocks updates to a device
-- already paired with a different fleet_node so a late report from a
-- prior reporter can't redirect the operator's pairing choice.
INSERT INTO discovered_device (
    org_id,
    device_identifier,
    ip_address,
    port,
    url_scheme,
    driver_name,
    model,
    manufacturer,
    firmware_version,
    is_active
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, TRUE)
ON CONFLICT (org_id, device_identifier) WHERE deleted_at IS NULL DO UPDATE SET
    ip_address = EXCLUDED.ip_address,
    port = EXCLUDED.port,
    url_scheme = EXCLUDED.url_scheme,
    driver_name = COALESCE(discovered_device.driver_name, EXCLUDED.driver_name),
    model = EXCLUDED.model,
    manufacturer = EXCLUDED.manufacturer,
    firmware_version = EXCLUDED.firmware_version,
    last_seen = CURRENT_TIMESTAMP,
    is_active = TRUE
WHERE NOT EXISTS (
    SELECT 1
    FROM device d
    LEFT JOIN fleet_node_device fnd
        ON fnd.device_id = d.id
       AND fnd.org_id = d.org_id
       AND fnd.fleet_node_id = $10
    WHERE d.discovered_device_id = discovered_device.id
      AND d.org_id = discovered_device.org_id
      AND d.deleted_at IS NULL
      AND fnd.fleet_node_id IS NULL
);

-- name: PairDeviceToFleetNode :execrows
INSERT INTO fleet_node_device (fleet_node_id, device_id, org_id, assigned_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (device_id) DO NOTHING;

-- name: UnpairDevice :execrows
DELETE FROM fleet_node_device
WHERE device_id = $1 AND org_id = $2;

-- name: DeletePairingsForFleetNode :execrows
-- Revoke soft-deletes the fleet_node row, so ON DELETE CASCADE doesn't fire.
DELETE FROM fleet_node_device
WHERE fleet_node_id = $1 AND org_id = $2;

-- name: ListFleetNodeDevices :many
SELECT fnd.fleet_node_id,
       fnd.device_id,
       d.device_identifier,
       COALESCE(dd.driver_name, '')::text AS device_type,
       fnd.assigned_at,
       fnd.assigned_by
FROM fleet_node_device fnd
JOIN device d ON d.id = fnd.device_id AND d.org_id = fnd.org_id AND d.deleted_at IS NULL
LEFT JOIN discovered_device dd ON dd.id = d.discovered_device_id AND dd.deleted_at IS NULL
WHERE fnd.org_id = $1
  AND (sqlc.narg('fleet_node_id')::bigint IS NULL OR fnd.fleet_node_id = sqlc.narg('fleet_node_id')::bigint)
ORDER BY fnd.assigned_at DESC, fnd.device_id ASC;
