-- name: InsertDevice :one
INSERT INTO device (
    org_id,
    discovered_device_id,
    device_identifier,
    mac_address,
    serial_number
) VALUES (
    $1,
    $2,
    $3,
    $4,
    $5
)
RETURNING id;

-- name: UpdateDeviceIPAssignment :exec
-- PostgreSQL equivalent of UPDATE with INNER JOIN
UPDATE discovered_device
SET
  ip_address = $1,
  port = $2,
  url_scheme = $3
FROM device d
WHERE discovered_device.id = d.discovered_device_id
  AND d.id = $4;

-- name: GetPairedDevicesIds :many
SELECT
    d.id as device_id
from device d
JOIN device_pairing dp ON d.id = dp.device_id
WHERE dp.pairing_status = 'PAIRED'
    AND d.org_id = $1
    AND d.deleted_at IS NULL
ORDER BY dp.id, d.id;

-- name: GetTotalPairedDevices :one
SELECT COUNT(*)
FROM device d
JOIN discovered_device dd ON d.discovered_device_id = dd.id
JOIN device_pairing dp ON d.id = dp.device_id
LEFT JOIN device_status ds ON d.id = ds.device_id
WHERE dp.pairing_status IN ('PAIRED', 'AUTHENTICATION_NEEDED')
    AND d.deleted_at IS NULL
    AND d.org_id = $1
    AND dd.is_active = TRUE
    AND (sqlc.narg('status_filter')::text IS NULL OR ds.status::text = ANY(string_to_array(sqlc.narg('status_filter'), ',')))
    AND (sqlc.narg('model_filter')::text IS NULL OR dd.model = ANY(string_to_array(sqlc.narg('model_filter'), ',')));

-- name: GetTotalDevicesPendingAuth :one
SELECT COUNT(*)
FROM device d
JOIN device_pairing dp ON d.id = dp.device_id
WHERE dp.pairing_status = 'AUTHENTICATION_NEEDED'
    AND d.deleted_at IS NULL
    AND d.org_id = $1;

-- name: UpsertDevicePairing :execresult
INSERT INTO device_pairing (
    device_id,
    pairing_status,
    paired_at
) VALUES (
    $1,
    $2,
    CURRENT_TIMESTAMP
)
ON CONFLICT (device_id) DO UPDATE SET
    pairing_status = EXCLUDED.pairing_status,
    paired_at = CURRENT_TIMESTAMP,
    unpaired_at = NULL;

-- name: UpdateDevicePairingStatusByIdentifier :exec
-- PostgreSQL equivalent of UPDATE with INNER JOIN
UPDATE device_pairing
SET pairing_status = $1
FROM device d
WHERE device_pairing.device_id = d.id
  AND d.device_identifier = $2
  AND d.deleted_at IS NULL;

-- name: GetDeviceByID :one
SELECT *
FROM device
WHERE id = $1
  AND org_id = $2
  AND deleted_at IS NULL
LIMIT 1;

-- name: GetDeviceByDeviceIdentifier :one
SELECT *
FROM device
WHERE device_identifier = $1
  AND org_id = $2
  AND deleted_at IS NULL
    LIMIT 1;

-- name: UpdateDeviceInfo :exec
UPDATE device
SET
    mac_address = COALESCE(NULLIF(sqlc.arg('mac_address')::text, ''), mac_address),
    serial_number = sqlc.arg('serial_number')
WHERE device_identifier = sqlc.arg('device_identifier')
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL;

-- name: UpdateDeviceWorkerName :execrows
UPDATE device
SET worker_name = $2
WHERE device_identifier = $1
  AND deleted_at IS NULL;

-- name: UpdateDeviceWorkerNamePoolSyncStatusByID :exec
UPDATE device
SET worker_name_pool_sync_status = $2
WHERE id = $1
  AND deleted_at IS NULL;

-- name: GetDevicePairingStatusByDeviceDatabaseID :one
SELECT
    dp.pairing_status
FROM device_pairing dp
WHERE dp.device_id = $1
LIMIT 1;

-- name: GetDeviceIDByDeviceIdentifier :one
SELECT id
FROM device
WHERE device_identifier = $1
  AND deleted_at IS NULL
LIMIT 1;

-- name: GetDeviceIdentifierByID :one
SELECT device_identifier
FROM device
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1;

-- name: GetDeviceIDsByDeviceIdentifiers :many
SELECT id
FROM device
WHERE device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND deleted_at IS NULL;

-- name: GetDeviceIDsWithIdentifiers :many
-- Returns device IDs mapped to their identifiers for batch operations.
SELECT id, device_identifier
FROM device
WHERE device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND deleted_at IS NULL;

-- name: AllDevicesBelongToOrg :one
-- Returns true if all provided device identifiers belong to the specified organization.
-- Used for authorization checks - fails fast if any device is not owned by the org.
SELECT COUNT(*) = sqlc.arg('expected_count') as all_belong
FROM device
WHERE device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND org_id = $1
  AND deleted_at IS NULL;

-- name: GetAllPairedDeviceIdentifiers :many
SELECT d.device_identifier
FROM device d
JOIN device_pairing dp ON d.id = dp.device_id
WHERE dp.pairing_status = 'PAIRED'
    AND d.deleted_at IS NULL;

-- name: CountMinersByState :one
-- Counts miners by their operational state for fleet health dashboard.
-- Bucket rules must match InsertMinerStateSnapshot (miner_state_snapshots.sql);
-- the uptime chart stores history against the same classifier.
--
-- Buckets are mutually exclusive and match MinerStatus.tsx:
-- 1. Offline:  OFFLINE, or NULL status when not auth-needed
-- 2. Sleeping: INACTIVE/MAINTENANCE when not auth-needed
-- 3. Broken:   (not offline, not sleeping) AND (ERROR/NEEDS_MINING_POOL/UPDATING/REBOOT_REQUIRED
--              status, or auth-needed, or has open error with severity 1-4)
-- 4. Hashing:  ACTIVE + paired + no open errors
--
-- Auth-needed miners with NULL/INACTIVE/MAINTENANCE status land in broken, not
-- offline/sleeping, because the UI checks needsAuthentication first.
-- Only open errors (closed_at IS NULL) with severity IN (1,2,3,4) are considered;
-- UNSPECIFIED (0) is excluded (normalized at ingestion by miner_error_mapper).
SELECT
    -- Offline
    COALESCE(SUM(CASE
        WHEN ds.status = 'OFFLINE'
             OR (ds.status IS NULL AND dp.pairing_status != 'AUTHENTICATION_NEEDED')
        THEN 1
        ELSE 0
    END), 0)::bigint as offline_count,

    -- Sleeping
    COALESCE(SUM(CASE
        WHEN ds.status IN ('MAINTENANCE', 'INACTIVE')
             AND dp.pairing_status != 'AUTHENTICATION_NEEDED'
        THEN 1
        ELSE 0
    END), 0)::bigint as sleeping_count,

    -- Broken
    COALESCE(SUM(CASE
        WHEN ds.status IS DISTINCT FROM 'OFFLINE'
             AND NOT (ds.status IS NULL AND dp.pairing_status != 'AUTHENTICATION_NEEDED')
             AND NOT (ds.status IN ('MAINTENANCE', 'INACTIVE') AND dp.pairing_status != 'AUTHENTICATION_NEEDED')
             AND (ds.status IN ('ERROR', 'NEEDS_MINING_POOL', 'UPDATING', 'REBOOT_REQUIRED')
                  OR dp.pairing_status = 'AUTHENTICATION_NEEDED'
                  OR open_errors.device_id IS NOT NULL)
        THEN 1
        ELSE 0
    END), 0)::bigint as broken_count,

    -- Hashing
    COALESCE(SUM(CASE
        WHEN ds.status = 'ACTIVE'
             AND dp.pairing_status != 'AUTHENTICATION_NEEDED'
             AND open_errors.device_id IS NULL
        THEN 1
        ELSE 0
    END), 0)::bigint as hashing_count
FROM device d
JOIN discovered_device dd ON d.discovered_device_id = dd.id
JOIN device_pairing dp ON d.id = dp.device_id
LEFT JOIN device_status ds ON d.id = ds.device_id
-- Open actionable errors (severity 1-4; excludes UNSPECIFIED=0)
LEFT JOIN (
    SELECT DISTINCT device_id
    FROM errors
    WHERE errors.org_id = sqlc.arg('org_id')
      AND errors.closed_at IS NULL
      AND errors.severity IN (1, 2, 3, 4)
) open_errors ON d.id = open_errors.device_id
WHERE d.deleted_at IS NULL
  AND d.org_id = sqlc.arg('org_id')
  AND dd.is_active = TRUE
  AND dd.deleted_at IS NULL
  AND dp.pairing_status IN ('PAIRED', 'AUTHENTICATION_NEEDED')
  -- Status filter mirrors GetTotalMinerStateSnapshots so
  -- sum(bucket counts) == filtered list total for every filter value.
  -- In particular, Hashing (ACTIVE) excludes rows with open actionable errors
  -- so ACTIVE+error rows are not admitted into scope only to fall into
  -- broken_count; they stay out of both the list and the counts.
  AND (
      sqlc.narg('status_filter')::text IS NULL
      OR (
          ds.status::text = ANY(sqlc.arg('status_values')::text[])
          AND (
              ds.status IN ('OFFLINE', 'MAINTENANCE', 'INACTIVE', 'NEEDS_MINING_POOL')
              OR (ds.status = 'ACTIVE' AND NOT EXISTS (
                  SELECT 1 FROM errors
                  WHERE errors.device_id = d.id
                    AND errors.org_id = sqlc.arg('org_id')
                    AND errors.closed_at IS NULL
                    AND errors.severity IN (1, 2, 3, 4)
              ))
              OR (sqlc.narg('needs_attention_filter')::boolean = TRUE)
          )
      )
      OR (sqlc.narg('needs_attention_filter')::boolean = TRUE
          AND dp.pairing_status = 'AUTHENTICATION_NEEDED'
          AND (ds.status IS NULL OR ds.status != 'OFFLINE'))
      OR (sqlc.narg('needs_attention_filter')::boolean = TRUE
          AND EXISTS (
              SELECT 1 FROM errors
              WHERE errors.device_id = d.id
                AND errors.org_id = sqlc.arg('org_id')
                AND errors.closed_at IS NULL
                AND errors.severity IN (1, 2, 3, 4)
          )
          AND NOT (ds.status IS NULL AND dp.pairing_status = 'PAIRED')
          AND (ds.status IS NULL OR ds.status NOT IN ('OFFLINE', 'MAINTENANCE', 'INACTIVE', 'NEEDS_MINING_POOL')))
      OR (sqlc.narg('include_null_status_filter')::boolean = TRUE
          AND ds.status IS NULL
          AND dp.pairing_status = 'PAIRED')
  )
  AND (sqlc.narg('model_filter')::text IS NULL OR dd.model = ANY(sqlc.arg('model_values')::text[]))
  AND (sqlc.narg('device_identifiers_filter')::text IS NULL OR d.device_identifier = ANY(sqlc.arg('device_identifier_values')::text[]))
  AND (
      sqlc.narg('group_ids_filter')::text IS NULL
      OR EXISTS (
          SELECT 1 FROM device_set_membership dsm
          WHERE dsm.device_id = d.id
            AND dsm.org_id = sqlc.arg('org_id')
            AND dsm.device_set_type = 'group'
            AND dsm.device_set_id = ANY(sqlc.arg('group_id_values')::bigint[])
      )
  )
  AND (
      sqlc.narg('rack_ids_filter')::text IS NULL
      OR EXISTS (
          SELECT 1 FROM device_set_membership dsm
          WHERE dsm.device_id = d.id
            AND dsm.org_id = sqlc.arg('org_id')
            AND dsm.device_set_type = 'rack'
            AND dsm.device_set_id = ANY(sqlc.arg('rack_id_values')::bigint[])
      )
  );

-- name: UpsertDeviceStatus :exec
INSERT INTO device_status (
    device_id,
    status,
    status_timestamp,
    status_details
) VALUES (
    $1,
    $2,
    $3,
    $4
)
ON CONFLICT (device_id) DO UPDATE SET
    status = EXCLUDED.status,
    status_timestamp = EXCLUDED.status_timestamp,
    status_details = EXCLUDED.status_details;

-- name: GetDeviceStatus :one
SELECT
    ds.status
FROM device_status ds
WHERE ds.device_id = $1
LIMIT 1;

-- name: GetDeviceStatusByDeviceIdentifier :one
SELECT
    ds.status
FROM device_status ds
JOIN device d ON ds.device_id = d.id
WHERE d.device_identifier = $1
  AND d.deleted_at IS NULL
LIMIT 1;

-- name: GetDeviceStatusForDeviceIdentifiers :many
SELECT
    d.device_identifier,
    ds.status
FROM device_status ds
JOIN device d ON ds.device_id = d.id
WHERE d.device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND d.deleted_at IS NULL;

-- name: GetMinerModelGroups :many
SELECT
    dd.model,
    dd.manufacturer,
    COUNT(*)::int AS count
FROM device d
JOIN discovered_device dd ON d.discovered_device_id = dd.id
JOIN device_pairing dp ON d.id = dp.device_id
LEFT JOIN device_status ds ON d.id = ds.device_id
WHERE dp.pairing_status = 'PAIRED'
  AND d.deleted_at IS NULL
  AND d.org_id = @org_id
  AND dd.model IS NOT NULL
  AND dd.model != ''
  AND (sqlc.narg('model_filter')::text IS NULL OR dd.model = ANY(string_to_array(sqlc.narg('model_filter'), ',')))
  AND (sqlc.narg('status_filter')::text IS NULL OR ds.status::text = ANY(string_to_array(sqlc.narg('status_filter'), ',')))
GROUP BY dd.model, dd.manufacturer
ORDER BY dd.manufacturer, dd.model;

-- name: GetAvailableModels :many
SELECT DISTINCT dd.model
FROM device d
JOIN discovered_device dd ON d.discovered_device_id = dd.id
JOIN device_pairing dp ON d.id = dp.device_id
WHERE dp.pairing_status = 'PAIRED'
  AND d.deleted_at IS NULL
  AND d.org_id = $1
  AND dd.model IS NOT NULL
  AND dd.model != ''
ORDER BY dd.model
;

-- name: GetAvailableFirmwareVersions :many
SELECT DISTINCT dd.firmware_version
FROM device d
JOIN discovered_device dd ON d.discovered_device_id = dd.id
JOIN device_pairing dp ON d.id = dp.device_id
WHERE dp.pairing_status = 'PAIRED'
  AND d.deleted_at IS NULL
  AND d.org_id = $1
  AND dd.firmware_version IS NOT NULL
  AND dd.firmware_version != ''
ORDER BY dd.firmware_version
;

-- name: GetOfflineDevices :many
SELECT
    d.id,
    d.device_identifier,
    d.mac_address,
    d.org_id,
    dd.device_identifier AS discovered_device_identifier,
    dd.ip_address,
    dd.port,
    dd.url_scheme,
    dd.driver_name
FROM device d
JOIN device_pairing dp ON d.id = dp.device_id
JOIN device_status ds ON d.id = ds.device_id
JOIN discovered_device dd ON d.discovered_device_id = dd.id
WHERE dp.pairing_status = 'PAIRED'
  AND d.deleted_at IS NULL
  AND ds.status = 'OFFLINE'
  AND d.mac_address IS NOT NULL
  AND d.mac_address != ''
ORDER BY ds.status_timestamp DESC
LIMIT $1;

-- name: GetKnownSubnets :many
SELECT DISTINCT
    set_masklen(network(inet(dd.ip_address)), sqlc.arg('mask_bits'))::text AS subnet
FROM device d
JOIN device_pairing dp ON d.id = dp.device_id
JOIN discovered_device dd ON d.discovered_device_id = dd.id
WHERE d.org_id = sqlc.arg('org_id')
  AND d.deleted_at IS NULL
  AND dd.deleted_at IS NULL
  AND dd.ip_address IS NOT NULL
  AND dd.ip_address != ''
  AND dp.pairing_status IN ('PAIRED', 'AUTHENTICATION_NEEDED')
  AND family(inet(dd.ip_address)) = CASE WHEN sqlc.arg('is_ipv4')::boolean THEN 4 ELSE 6 END
ORDER BY subnet;

-- name: ListMinerStateSnapshots :many
-- TYPE GENERATION STUB - This query is never executed.
-- The actual list query uses a hand-written query builder in device.go
-- because sqlc cannot parameterize ORDER BY direction or dynamic columns.
-- This stub exists solely to generate the ListMinerStateSnapshotsRow type.
SELECT
    dd.device_identifier,
    COALESCE(d.mac_address, '') as mac_address,
    d.serial_number,
    dd.model,
    dd.manufacturer,
    dd.firmware_version,
    d.worker_name,
    ds.status as device_status,
    ds.status_timestamp,
    ds.status_details,
    dd.ip_address,
    dd.port,
    dd.url_scheme,
    CASE WHEN d.id IS NOT NULL THEN COALESCE(dp.pairing_status::text, 'UNPAIRED') ELSE 'UNPAIRED' END as pairing_status,
    dd.id as cursor_id,
    COALESCE(d.id, 0) as device_id,
    dd.driver_name,
    d.custom_name
FROM discovered_device dd
LEFT JOIN device d ON dd.id = d.discovered_device_id
LEFT JOIN device_pairing dp ON d.id = dp.device_id
LEFT JOIN device_status ds ON d.id = ds.device_id
WHERE FALSE;

-- name: GetDevicePropertiesForRename :many
-- Returns the device properties needed for name generation during a rename operation.
WITH latest_metrics AS (
    SELECT DISTINCT ON (device_metrics.device_identifier)
        device_metrics.device_identifier,
        device_metrics.hash_rate_hs,
        device_metrics.temp_c,
        device_metrics.power_w,
        device_metrics.efficiency_jh
    FROM device_metrics
    INNER JOIN device d2 ON device_metrics.device_identifier = d2.device_identifier
        AND d2.deleted_at IS NULL
        AND d2.org_id = sqlc.arg('org_id')
    WHERE device_metrics.time > NOW() - INTERVAL '10 minutes'
        AND device_metrics.device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
    ORDER BY device_metrics.device_identifier, device_metrics.time DESC
)
SELECT
    d.device_identifier,
    dd.id as discovered_device_id,
    COALESCE(d.custom_name, '') as custom_name,
    COALESCE(d.mac_address, '') as mac_address,
    d.serial_number,
    dd.model,
    dd.manufacturer,
    COALESCE(dd.ip_address, '') as ip_address,
    dd.firmware_version,
    d.worker_name,
    d.worker_name_pool_sync_status,
    latest_metrics.hash_rate_hs,
    latest_metrics.temp_c,
    latest_metrics.power_w,
    latest_metrics.efficiency_jh
FROM device d
JOIN discovered_device dd ON d.discovered_device_id = dd.id
LEFT JOIN latest_metrics ON d.device_identifier = latest_metrics.device_identifier
WHERE d.device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND d.org_id = sqlc.arg('org_id')
  AND d.deleted_at IS NULL;

-- name: GetDevicePropertiesForRenameWithoutTelemetry :many
-- Returns rename properties when the requested sort does not require telemetry data.
SELECT
    d.device_identifier,
    dd.id as discovered_device_id,
    COALESCE(d.custom_name, '') as custom_name,
    COALESCE(d.mac_address, '') as mac_address,
    d.serial_number,
    dd.model,
    dd.manufacturer,
    COALESCE(dd.ip_address, '') as ip_address,
    dd.firmware_version,
    d.worker_name,
    d.worker_name_pool_sync_status
FROM device d
JOIN discovered_device dd ON d.discovered_device_id = dd.id
WHERE d.device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND d.org_id = sqlc.arg('org_id')
  AND d.deleted_at IS NULL;


-- name: GetTotalMinerStateSnapshots :one
-- Unified query that supports all filters including component error filtering
-- Uses EXISTS for error checks (more efficient than LEFT JOIN + DISTINCT)
SELECT COUNT(*) as total
FROM discovered_device dd
LEFT JOIN device d ON dd.id = d.discovered_device_id
    AND d.deleted_at IS NULL
    AND d.org_id = sqlc.arg('org_id')
LEFT JOIN device_pairing dp ON d.id = dp.device_id
LEFT JOIN device_status ds ON d.id = ds.device_id
WHERE dd.org_id = sqlc.arg('org_id')
    AND dd.is_active = TRUE
    AND dd.deleted_at IS NULL
    -- Pairing status filter
    AND (
        sqlc.narg('pairing_status_filter')::text IS NULL
        OR CASE WHEN d.id IS NOT NULL THEN COALESCE(dp.pairing_status::text, 'UNPAIRED') ELSE 'UNPAIRED' END
           = ANY(sqlc.arg('pairing_status_values')::text[])
    )
    -- Model filter
    AND (sqlc.narg('model_filter')::text IS NULL OR dd.model = ANY(sqlc.arg('model_values')::text[]))
    -- Status filter with error handling
    AND (
        sqlc.narg('status_filter')::text IS NULL
        OR (
            ds.status::text = ANY(sqlc.arg('status_values')::text[])
            AND (
                ds.status IN ('OFFLINE', 'MAINTENANCE', 'INACTIVE', 'NEEDS_MINING_POOL')
                OR (ds.status = 'ACTIVE' AND NOT EXISTS (
                    SELECT 1 FROM errors
                    WHERE errors.device_id = d.id
                      AND errors.org_id = sqlc.arg('org_id')
                      AND errors.closed_at IS NULL
                      AND errors.severity IN (1, 2, 3, 4)
                ))
                OR (sqlc.narg('needs_attention_filter')::boolean = TRUE)
            )
        )
        -- Auth-needed (exclude OFFLINE only)
        OR (sqlc.narg('needs_attention_filter')::boolean = TRUE
            AND dp.pairing_status = 'AUTHENTICATION_NEEDED'
            AND (ds.status IS NULL OR ds.status != 'OFFLINE'))
        -- Devices with actionable errors. Excludes NULL-status paired miners
        -- so they stay bucketed as offline (matches CountMinersByState).
        OR (sqlc.narg('needs_attention_filter')::boolean = TRUE
            AND EXISTS (
                SELECT 1 FROM errors
                WHERE errors.device_id = d.id
                  AND errors.org_id = sqlc.arg('org_id')
                  AND errors.closed_at IS NULL
                  AND errors.severity IN (1, 2, 3, 4)
            )
            AND NOT (ds.status IS NULL AND dp.pairing_status = 'PAIRED')
            AND (ds.status IS NULL OR ds.status NOT IN ('OFFLINE', 'MAINTENANCE', 'INACTIVE', 'NEEDS_MINING_POOL')))
        -- NULL-status paired miners (counted as offline in dashboard).
        -- Scoped to PAIRED only to match CountMinersByState's WHERE clause.
        OR (sqlc.narg('include_null_status_filter')::boolean = TRUE
            AND ds.status IS NULL
            AND dp.pairing_status = 'PAIRED')
    )
    -- Component error filter
    AND (
        sqlc.narg('error_component_types_filter')::text IS NULL
        OR EXISTS (
            SELECT 1 FROM errors
            WHERE errors.device_id = d.id
              AND errors.closed_at IS NULL
              AND errors.component_type = ANY(sqlc.arg('error_component_type_values')::int[])
        )
    )
    -- Group filter
    AND (
        sqlc.narg('group_ids_filter')::text IS NULL
        OR EXISTS (
            SELECT 1 FROM device_set_membership dsm
            WHERE dsm.device_id = d.id
              AND dsm.org_id = sqlc.arg('org_id')
              AND dsm.device_set_type = 'group'
              AND dsm.device_set_id = ANY(sqlc.arg('group_id_values')::bigint[])
        )
    )
    -- Rack filter
    AND (
        sqlc.narg('rack_ids_filter')::text IS NULL
        OR EXISTS (
            SELECT 1 FROM device_set_membership dsm
            WHERE dsm.device_id = d.id
              AND dsm.org_id = sqlc.arg('org_id')
              AND dsm.device_set_type = 'rack'
              AND dsm.device_set_id = ANY(sqlc.arg('rack_id_values')::bigint[])
        )
    );

-- name: GetFilteredDeviceIds :many
-- Returns device IDs filtered by pairing status and optional device status.
-- Used for bulk command operations.
SELECT
    d.id as device_id
FROM device d
JOIN device_pairing dp ON d.id = dp.device_id
JOIN discovered_device dd ON d.discovered_device_id = dd.id
LEFT JOIN device_status ds ON d.id = ds.device_id
WHERE d.org_id = sqlc.arg('org_id')
    AND dp.pairing_status::text = COALESCE(sqlc.narg('pairing_status')::text, 'PAIRED')
    AND d.deleted_at IS NULL
    AND (sqlc.narg('device_status')::text IS NULL OR ds.status::text = sqlc.narg('device_status')::text)
    AND (sqlc.narg('model_filter')::text IS NULL OR dd.model = ANY(string_to_array(sqlc.narg('model_filter'), ',')))
    AND (sqlc.narg('manufacturer_filter')::text IS NULL OR dd.manufacturer = ANY(string_to_array(sqlc.narg('manufacturer_filter'), ',')))
ORDER BY d.id;

-- name: GetFilteredDeviceIdentifiers :many
-- Mirrors GetFilteredDeviceIds but returns device_identifier strings instead of
-- internal IDs. Used by command preflight filtering, which operates on
-- identifiers (the same primitive used by selectors and schedules).
SELECT
    d.device_identifier
FROM device d
JOIN device_pairing dp ON d.id = dp.device_id
JOIN discovered_device dd ON d.discovered_device_id = dd.id
LEFT JOIN device_status ds ON d.id = ds.device_id
WHERE d.org_id = sqlc.arg('org_id')
    AND dp.pairing_status::text = COALESCE(sqlc.narg('pairing_status')::text, 'PAIRED')
    AND d.deleted_at IS NULL
    AND (sqlc.narg('device_status')::text IS NULL OR ds.status::text = sqlc.narg('device_status')::text)
    AND (sqlc.narg('model_filter')::text IS NULL OR dd.model = ANY(string_to_array(sqlc.narg('model_filter'), ',')))
    AND (sqlc.narg('manufacturer_filter')::text IS NULL OR dd.manufacturer = ANY(string_to_array(sqlc.narg('manufacturer_filter'), ',')))
ORDER BY d.device_identifier;

-- name: GetDeviceInfoForCapabilityCheck :many
-- Returns device information needed for capability checking.
-- Used when checking if specific devices support a command.
SELECT
    d.id,
    d.device_identifier,
    dd.manufacturer,
    dd.model,
    dd.firmware_version,
    dd.driver_name
FROM device d
JOIN discovered_device dd ON d.discovered_device_id = dd.id
JOIN device_pairing dp ON d.id = dp.device_id
WHERE d.device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND d.deleted_at IS NULL
  AND d.org_id = sqlc.arg('org_id')
  AND dp.pairing_status = 'PAIRED';

-- name: GetAllDeviceInfoForCapabilityCheck :many
-- Returns device information for all paired devices in an organization.
-- Used when checking capabilities for "select all" operations.
SELECT
    d.id,
    d.device_identifier,
    dd.manufacturer,
    dd.model,
    dd.firmware_version,
    dd.driver_name
FROM device d
JOIN discovered_device dd ON d.discovered_device_id = dd.id
JOIN device_pairing dp ON d.id = dp.device_id
WHERE d.org_id = $1
  AND d.deleted_at IS NULL
  AND dp.pairing_status = 'PAIRED';

-- name: SoftDeleteDevices :execrows
-- Soft-deletes devices by setting deleted_at timestamp.
-- Returns the number of rows affected.
UPDATE device SET deleted_at = NOW()
WHERE device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL;

-- name: SoftDeleteDiscoveredDevicesForDeletedDevices :exec
-- Soft-deletes discovered_device records linked to the specified devices.
UPDATE discovered_device dd SET deleted_at = NOW()
FROM device d
WHERE dd.id = d.discovered_device_id
  AND d.device_identifier = ANY(sqlc.arg('device_identifiers')::text[])
  AND d.org_id = sqlc.arg('org_id')
  AND dd.deleted_at IS NULL;

-- name: GetPairedDeviceByMACAddress :many
-- Finds an existing paired device by MAC address for a given organization.
-- Used during discovery reconciliation to detect devices that moved to a new IP/subnet.
-- Callers pass the MAC in colon-separated uppercase format (AA:BB:CC:DD:EE:FF),
-- which matches the normalized format stored in the database.
SELECT
    d.device_identifier,
    d.mac_address,
    d.serial_number,
    dd.device_identifier AS discovered_device_identifier,
    dd.id AS discovered_device_id
FROM device d
JOIN device_pairing dp ON d.id = dp.device_id
JOIN discovered_device dd ON d.discovered_device_id = dd.id
WHERE d.mac_address = sqlc.arg('normalized_mac')
  AND d.org_id = sqlc.arg('org_id')
  AND d.deleted_at IS NULL
  AND dd.deleted_at IS NULL
  AND dp.pairing_status IN ('PAIRED', 'AUTHENTICATION_NEEDED')
ORDER BY d.id
LIMIT 2;

-- name: GetPairedDevicesByMACAddresses :many
-- Batch lookup of paired devices by MAC addresses for a given organization.
-- Returns one row per matched MAC (most recently created device wins if duplicates exist).
-- Used by Foreman import to resolve Foreman miner IDs to Fleet device identifiers in a single query.
SELECT DISTINCT ON (d.mac_address)
    d.device_identifier,
    d.mac_address,
    d.serial_number,
    dd.device_identifier AS discovered_device_identifier,
    dd.id AS discovered_device_id
FROM device d
JOIN device_pairing dp ON d.id = dp.device_id
JOIN discovered_device dd ON d.discovered_device_id = dd.id
WHERE d.mac_address = ANY(sqlc.arg('mac_addresses')::text[])
  AND d.org_id = sqlc.arg('org_id')
  AND d.deleted_at IS NULL
  AND dd.deleted_at IS NULL
  AND dp.pairing_status IN ('PAIRED', 'AUTHENTICATION_NEEDED')
ORDER BY d.mac_address, d.id DESC;

-- name: GetPairedDeviceBySerialNumber :one
-- Finds an existing paired device by serial number for a given organization.
-- Used as fallback reconciliation when MAC address is not available during re-pairing.
SELECT
    d.device_identifier,
    d.mac_address,
    d.serial_number,
    dd.device_identifier AS discovered_device_identifier,
    dd.id AS discovered_device_id
FROM device d
JOIN device_pairing dp ON d.id = dp.device_id
JOIN discovered_device dd ON d.discovered_device_id = dd.id
WHERE d.serial_number = $1
  AND d.org_id = $2
  AND d.deleted_at IS NULL
  AND dd.deleted_at IS NULL
  AND dp.pairing_status IN ('PAIRED', 'AUTHENTICATION_NEEDED')
LIMIT 1;

-- GetDeviceIdentifiersByOrgWithFilter is implemented as a dynamic query in
-- sqlstores/device.go to reuse appendFilterSQL and ensure semantic parity with
-- the list view's "needs attention" filter logic (ERROR status includes devices
-- with open actionable errors).
