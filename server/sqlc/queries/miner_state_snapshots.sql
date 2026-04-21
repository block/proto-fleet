-- name: InsertMinerStateSnapshot :exec
-- Materializes one row per paired device for a single tick. CASE mirrors
-- CountMinersByState so chart history and live legend share one classifier.
-- State encoding: 0=offline, 1=sleeping, 2=broken, 3=hashing.
INSERT INTO miner_state_snapshots (time, org_id, device_identifier, state)
SELECT
    sqlc.arg('time')::timestamptz,
    d.org_id,
    d.device_identifier,
    CASE
        WHEN ds.status = 'OFFLINE'
             OR (ds.status IS NULL AND dp.pairing_status != 'AUTHENTICATION_NEEDED')
            THEN 0
        WHEN ds.status IN ('MAINTENANCE', 'INACTIVE')
             AND dp.pairing_status != 'AUTHENTICATION_NEEDED'
            THEN 1
        WHEN ds.status IN ('ERROR', 'NEEDS_MINING_POOL', 'UPDATING', 'REBOOT_REQUIRED')
             OR dp.pairing_status = 'AUTHENTICATION_NEEDED'
             OR open_errors.device_id IS NOT NULL
            THEN 2
        ELSE 3
    END
FROM device d
JOIN discovered_device dd ON d.discovered_device_id = dd.id
JOIN device_pairing     dp ON d.id = dp.device_id
LEFT JOIN device_status ds ON d.id = ds.device_id
LEFT JOIN (
    SELECT DISTINCT device_id
    FROM errors
    WHERE closed_at IS NULL
      AND severity IN (1, 2, 3, 4)
) open_errors ON d.id = open_errors.device_id
WHERE d.deleted_at IS NULL
  AND dd.is_active = TRUE
  AND dd.deleted_at IS NULL
  AND dp.pairing_status IN ('PAIRED', 'AUTHENTICATION_NEEDED');

-- name: GetMinerStateSnapshots :many
-- DISTINCT ON picks the most recent snapshot per device per bucket so summed
-- counts always equal a real fleet size, regardless of snapshot alignment.
-- Device filter matches device_selector for scope-aware uptime.
WITH per_device_bucket AS (
    SELECT DISTINCT ON (time_bucket(sqlc.arg('bucket_interval')::text::interval, time), device_identifier)
        time_bucket(sqlc.arg('bucket_interval')::text::interval, time)::timestamptz AS bucket,
        device_identifier,
        state
    FROM miner_state_snapshots
    WHERE org_id = sqlc.arg('org_id')
      AND time >= sqlc.arg('start_time')
      AND time <= sqlc.arg('end_time')
      AND (sqlc.narg('device_identifiers_filter')::text IS NULL
           OR device_identifier = ANY(sqlc.arg('device_identifier_values')::text[]))
    ORDER BY time_bucket(sqlc.arg('bucket_interval')::text::interval, time), device_identifier, time DESC
)
SELECT
    bucket,
    SUM(CASE WHEN state = 3 THEN 1 ELSE 0 END)::int AS hashing_count,
    SUM(CASE WHEN state = 2 THEN 1 ELSE 0 END)::int AS broken_count,
    SUM(CASE WHEN state = 0 THEN 1 ELSE 0 END)::int AS offline_count,
    SUM(CASE WHEN state = 1 THEN 1 ELSE 0 END)::int AS sleeping_count
FROM per_device_bucket
GROUP BY bucket
ORDER BY bucket ASC;
