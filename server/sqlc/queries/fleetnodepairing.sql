-- name: UpsertDiscoveredDeviceFromFleetNode :execrows
-- 0 rows on conflict signals rejection. Blocks: claims from a
-- different attribution, and retargeting of locally-paired devices
-- (which have a device row but no fleet_node_device binding).
--
-- The reconciled CTE substitutes the caller's identifier with any
-- existing auto:* identifier at the same (fleet_node, ip, port)
-- endpoint. Synthesized identifiers (agent re-keys per scan) collapse
-- onto the first one; mac:/serial: identifiers pass through unchanged.
WITH input AS (
    SELECT $2::text AS device_identifier
),
reconciled AS (
    SELECT CASE
        WHEN (SELECT device_identifier FROM input) LIKE 'auto:%' THEN COALESCE((
            SELECT device_identifier
            FROM discovered_device
            WHERE org_id = $1
              AND discovered_by_fleet_node_id = $10
              AND ip_address = $3
              AND port = $4
              AND device_identifier LIKE 'auto:%'
              AND deleted_at IS NULL
            LIMIT 1
        ), (SELECT device_identifier FROM input))
        ELSE (SELECT device_identifier FROM input)
    END AS device_identifier
)
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
    discovered_by_fleet_node_id,
    is_active
)
SELECT $1, reconciled.device_identifier, $3, $4, $5, $6, $7, $8, $9, $10, TRUE
FROM reconciled
ON CONFLICT (org_id, device_identifier) WHERE deleted_at IS NULL DO UPDATE SET
    ip_address = EXCLUDED.ip_address,
    port = EXCLUDED.port,
    url_scheme = EXCLUDED.url_scheme,
    driver_name = COALESCE(discovered_device.driver_name, EXCLUDED.driver_name),
    model = EXCLUDED.model,
    manufacturer = EXCLUDED.manufacturer,
    firmware_version = EXCLUDED.firmware_version,
    discovered_by_fleet_node_id = EXCLUDED.discovered_by_fleet_node_id,
    last_seen = CURRENT_TIMESTAMP,
    is_active = TRUE
WHERE (
    discovered_device.discovered_by_fleet_node_id IS NULL
    OR discovered_device.discovered_by_fleet_node_id = EXCLUDED.discovered_by_fleet_node_id
  )
  AND NOT EXISTS (
    SELECT 1
    FROM device d
    LEFT JOIN fleet_node_device fnd
        ON fnd.device_id = d.id
       AND fnd.org_id = d.org_id
       AND fnd.fleet_node_id = EXCLUDED.discovered_by_fleet_node_id
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

-- name: ClearAttributionForFleetNode :execrows
UPDATE discovered_device
SET discovered_by_fleet_node_id = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE discovered_by_fleet_node_id = $1
  AND org_id = $2
  AND deleted_at IS NULL;

-- name: SetDiscoveredDeviceAttributionForDevice :execrows
-- Pass NULL fleet_node_id to clear; non-NULL to set.
UPDATE discovered_device dd
SET discovered_by_fleet_node_id = sqlc.narg('fleet_node_id')::bigint,
    updated_at = CURRENT_TIMESTAMP
FROM device d
WHERE d.id = $1
  AND d.org_id = $2
  AND d.deleted_at IS NULL
  AND dd.id = d.discovered_device_id
  AND dd.org_id = $2
  AND dd.deleted_at IS NULL;

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
