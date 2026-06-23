-- name: UpsertDiscoveredDeviceFromFleetNode :execrows
-- 0 rows on conflict signals rejection. A remote report must not redirect the
-- endpoint/credentials of a miner the cloud owns, so the update is blocked
-- unless the row is already attributed to this fleet node or to a revoked node
-- that can be reclaimed. Devices paired to the reporting node itself stay
-- refreshable, subject to the attribution guard. The agent synthesizes a stable
-- per-device identifier (mac:/serial:, else auto:<hash>), so a re-scan reuses
-- the same row.
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
    discovered_device.discovered_by_fleet_node_id = EXCLUDED.discovered_by_fleet_node_id
    -- The attributing node was revoked (soft-deleted), so a replacement node may
    -- reclaim its rows (otherwise a re-scan of the same stable mac:/serial: device
    -- is rejected forever). Attribution moves to the reporter ($10), staying
    -- non-NULL, so cloud-exclusion (discovered_by_fleet_node_id IS NULL) is never
    -- widened. Cloud-paired and live-cross-node rows remain blocked below.
    OR (
      discovered_device.discovered_by_fleet_node_id IS NOT NULL
      AND NOT EXISTS (
      SELECT 1
      FROM fleet_node fn
      WHERE fn.id = discovered_device.discovered_by_fleet_node_id
        AND fn.org_id = discovered_device.org_id
        AND fn.deleted_at IS NULL
      )
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
        (
          EXISTS (
            SELECT 1
            FROM device_pairing dp
            WHERE dp.device_id = d.id
              AND dp.pairing_status IN ('PAIRED', 'DEFAULT_PASSWORD')
          )
          -- A device paired to THIS reporting node ($10) is node-dialed, not
          -- cloud-dialed; its paired-like row must not block its own node's re-scan.
          AND NOT EXISTS (
            SELECT 1
            FROM fleet_node_device fnd
            WHERE fnd.device_id = d.id
              AND fnd.org_id = d.org_id
              AND fnd.fleet_node_id = $10
          )
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
-- True when the device is cloud-dialed: paired-like and not bound to any fleet node.
-- A device paired to a fleet node is also paired-like (so it reads as paired in
-- the UI), but the node dials it, so it is excluded here. Fleet-node pairing
-- refuses cloud-dialed devices: the upsert guard blocks refreshing them, so pairing
-- one would strand the node unable to refresh its discovery endpoint.
SELECT EXISTS (
    SELECT 1
    FROM device_pairing dp
    JOIN device d ON d.id = dp.device_id
    WHERE dp.device_id = $1
      AND d.org_id = $2
      AND d.deleted_at IS NULL
      AND dp.pairing_status IN ('PAIRED', 'DEFAULT_PASSWORD')
      AND NOT EXISTS (
        SELECT 1
        FROM fleet_node_device fnd
        WHERE fnd.device_id = d.id
          AND fnd.org_id = d.org_id
      )
);

-- name: DeviceHasActivePairing :one
-- True when the device is paired-like, regardless of whether it is cloud-dialed
-- or bound to a fleet node. Used to refuse downgrading an already paired-like
-- device to AUTHENTICATION_NEEDED on a non-PAIRED node report: between target
-- resolution and persistence, another node (or the cloud) may have paired the
-- device, and a stale AUTH_NEEDED result must not clobber that paired-like status.
SELECT EXISTS (
    SELECT 1
    FROM device_pairing dp
    JOIN device d ON d.id = dp.device_id
    WHERE dp.device_id = $1
      AND d.org_id = $2
      AND d.deleted_at IS NULL
      AND dp.pairing_status IN ('PAIRED', 'DEFAULT_PASSWORD')
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

-- name: ListFleetNodeDiscoveredDevices :many
-- Fleet-node-discovered devices not yet paired to their node. A discovered
-- device is excluded when ANY of its live device rows is already node-bound
-- (fleet_node_device) or cloud-paired-like; AUTHENTICATION_NEEDED rows (a pair
-- attempt that needs credentials) surface for retry. Inverse of
-- GetActiveUnpairedDiscoveredDevices, which excludes fleet-node rows.
-- The exclusions use NOT EXISTS so a device with more than one live row is
-- judged across all of them, not just the joined row. They match by
-- discovered_device_id OR device_identifier: a paired device whose original
-- discovery row was soft-deleted and re-created by a node keeps the same
-- identifier but a different linkage, and must not be dispatched for pairing
-- (the node would mutate a miner persistence then rejects). DISTINCT ON (dd.id) with
-- the d.id DESC tie-breaker yields one deterministic row per discovered device
-- (the latest live device's pairing_status). Paginates by ascending id; a NULL
-- limit returns all rows (the pairing batch path needs every candidate).
WITH candidate AS (
  SELECT DISTINCT ON (dd.id)
         dd.id,
         dd.org_id,
         dd.device_identifier,
         dd.discovered_by_fleet_node_id,
         dd.ip_address,
         dd.port,
         dd.url_scheme,
         dd.driver_name,
         dd.model,
         dd.manufacturer,
         dd.firmware_version,
         dd.last_seen,
         COALESCE(dp.pairing_status::text, '')::text AS pairing_status
  FROM discovered_device dd
  LEFT JOIN device d ON (
      d.discovered_device_id = dd.id
      OR (d.device_identifier = dd.device_identifier AND d.org_id = dd.org_id)
    )
    AND d.deleted_at IS NULL
  LEFT JOIN device_pairing dp ON dp.device_id = d.id
  WHERE dd.org_id = $1
    AND dd.is_active = TRUE
    AND dd.deleted_at IS NULL
    AND dd.discovered_by_fleet_node_id IS NOT NULL
    AND NOT EXISTS (
        SELECT 1
        FROM device db
        JOIN fleet_node_device fnd ON fnd.device_id = db.id AND fnd.org_id = dd.org_id
        WHERE (db.discovered_device_id = dd.id
               OR (db.device_identifier = dd.device_identifier AND db.org_id = dd.org_id))
          AND db.deleted_at IS NULL
    )
    AND NOT EXISTS (
        SELECT 1
        FROM device dpd
        JOIN device_pairing dpp ON dpp.device_id = dpd.id
        WHERE (dpd.discovered_device_id = dd.id
               OR (dpd.device_identifier = dd.device_identifier AND dpd.org_id = dd.org_id))
          AND dpd.deleted_at IS NULL
          AND dpp.pairing_status IN ('PAIRED', 'DEFAULT_PASSWORD')
    )
    AND (sqlc.narg('fleet_node_id')::bigint IS NULL OR dd.discovered_by_fleet_node_id = sqlc.narg('fleet_node_id')::bigint)
    -- pair-all without operator credentials can't satisfy AUTHENTICATION_NEEDED rows
    -- (they were already attempted and need credentials). Excluding them keeps a
    -- capped first page from filling with unsatisfiable rows and starving
    -- never-attempted devices on re-issue for nodes with more than `limit`
    -- candidates. NULL/false keeps them (listing for display, and pair-all WITH
    -- credentials, which can retry them).
    AND (
      NOT COALESCE(sqlc.narg('exclude_auth_needed')::bool, FALSE)
      OR NOT EXISTS (
        SELECT 1
        FROM device adn
        JOIN device_pairing adp ON adp.device_id = adn.id
        WHERE (adn.discovered_device_id = dd.id
               OR (adn.device_identifier = dd.device_identifier AND adn.org_id = dd.org_id))
          AND adn.deleted_at IS NULL
          AND adp.pairing_status = 'AUTHENTICATION_NEEDED'
      )
    )
    -- Explicit pairing passes the requested identifiers so only those rows are
    -- scanned, not the whole org. NULL = no filter (listing + pair-all); an empty
    -- non-nil array matches nothing (explicit selection of none).
    AND (sqlc.narg('identifiers')::text[] IS NULL OR dd.device_identifier = ANY(sqlc.narg('identifiers')::text[]))
    AND (sqlc.narg('models')::text[] IS NULL OR COALESCE(dd.model, '') = ANY(sqlc.narg('models')::text[]))
    AND (sqlc.narg('manufacturers')::text[] IS NULL OR COALESCE(dd.manufacturer, '') = ANY(sqlc.narg('manufacturers')::text[]))
    AND (sqlc.narg('cursor_id')::bigint IS NULL OR dd.id > sqlc.narg('cursor_id')::bigint)
  ORDER BY dd.id ASC, d.id DESC NULLS LAST
)
SELECT *
FROM candidate
WHERE (sqlc.narg('pairing_statuses')::text[] IS NULL OR pairing_status = ANY(sqlc.narg('pairing_statuses')::text[]))
ORDER BY id ASC
LIMIT sqlc.narg('limit')::bigint;
