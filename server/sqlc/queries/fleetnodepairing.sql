-- name: UpsertDiscoveredDeviceFromFleetNode :execrows
-- 0 rows on conflict signals rejection. A remote report must not redirect the
-- endpoint/credentials of a miner the cloud actively dials, so the update is
-- blocked when the row is promoted to a cloud-paired device (device_pairing
-- PAIRED) or one paired to a different fleet node. Bare promoted devices and
-- devices paired to the reporting node itself stay refreshable, subject to the
-- attribution guard. The agent synthesizes a stable per-device identifier
-- (mac:/serial:, else auto:<hash>), so a re-scan reuses the same row.
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
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, TRUE)
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
    -- The attributing node was revoked (soft-deleted), so a replacement node may
    -- reclaim its rows (otherwise a re-scan of the same stable mac:/serial: device
    -- is rejected forever). Attribution moves to the reporter ($10), staying
    -- non-NULL, so cloud-exclusion (discovered_by_fleet_node_id IS NULL) is never
    -- widened. Cloud-paired and live-cross-node rows remain blocked below.
    OR NOT EXISTS (
      SELECT 1
      FROM fleet_node fn
      WHERE fn.id = discovered_device.discovered_by_fleet_node_id
        AND fn.org_id = discovered_device.org_id
        AND fn.deleted_at IS NULL
    )
  )
  AND NOT EXISTS (
    -- The promotion guard (rationale in the header): blocks rows promoted to a
    -- cloud-paired device or one paired to another node; bare and own-node pass.
    SELECT 1
    FROM device d
    WHERE d.discovered_device_id = discovered_device.id
      AND d.org_id = discovered_device.org_id
      AND d.deleted_at IS NULL
      AND (
        EXISTS (
          SELECT 1
          FROM device_pairing dp
          WHERE dp.device_id = d.id
            AND dp.pairing_status = 'PAIRED'
        )
        OR EXISTS (
          SELECT 1
          FROM fleet_node_device fnd
          WHERE fnd.device_id = d.id
            AND fnd.org_id = d.org_id
            AND fnd.fleet_node_id <> $10
        )
      )
);

-- name: PairDeviceToFleetNode :execrows
INSERT INTO fleet_node_device (fleet_node_id, device_id, org_id, assigned_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (device_id) DO NOTHING;

-- name: DeviceHasActiveCloudPairing :one
-- True when the device has a PAIRED cloud device_pairing. Fleet-node pairing
-- refuses these: the upsert guard blocks refreshing a cloud-paired row, so
-- pairing one would strand the node unable to refresh its discovery endpoint.
SELECT EXISTS (
    SELECT 1
    FROM device_pairing dp
    JOIN device d ON d.id = dp.device_id
    WHERE dp.device_id = $1
      AND d.org_id = $2
      AND d.deleted_at IS NULL
      AND dp.pairing_status = 'PAIRED'
);

-- name: TransferDiscoveredDeviceAttribution :execrows
-- Pairing makes the fleet node the discovery owner so its future reports refresh
-- the row (the upsert keys refreshability on discovered_by_fleet_node_id);
-- otherwise a replacement node's reports are rejected until repaired by hand.
-- No-op (0 rows) when the device has no discovered_device origin.
UPDATE discovered_device
SET discovered_by_fleet_node_id = sqlc.arg(fleet_node_id)::bigint
FROM device
WHERE device.id = sqlc.arg(device_id)
  AND device.org_id = sqlc.arg(org_id)
  AND device.deleted_at IS NULL
  AND device.discovered_device_id = discovered_device.id
  AND discovered_device.org_id = sqlc.arg(org_id)
  AND discovered_device.deleted_at IS NULL;

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
