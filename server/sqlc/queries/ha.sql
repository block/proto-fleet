-- name: GetConnectedPostgresIdentity :one
SELECT
    server_address,
    server_port,
    in_recovery,
    timeline
FROM connected_postgres_identity;

-- name: GetHAProfileDatabaseIdentity :one
SELECT
    current_user::TEXT AS database_user,
    current_database()::TEXT AS database_name,
    COALESCE(database_role.rolsuper, FALSE)::BOOLEAN AS is_superuser,
    current_setting('proto_fleet.source_commit')::TEXT AS source_commit
FROM pg_catalog.pg_roles AS database_role
WHERE database_role.rolname = current_user;

-- name: AcquireFleetRuntimeLease :one
WITH lease_context AS (
    -- Capture database time once, and fail closed unless this is the expected writer.
    SELECT
        clock_timestamp() AS database_time
    FROM connected_postgres_identity AS connected
    WHERE
        NOT connected.in_recovery
        AND connected.server_address = sqlc.arg('server_address')::TEXT
        AND connected.server_port = sqlc.arg('server_port')::INTEGER
        AND connected.timeline = sqlc.arg('timeline')::BIGINT
)
INSERT INTO fleet_runtime_lease AS current_lease (
    lease_name,
    dcs_cluster_id,
    highest_writer_generation,
    lease_epoch,
    holder_id,
    expires_at
)
SELECT
    'fleet-active',
    sqlc.arg('dcs_cluster_id'),
    sqlc.arg('writer_generation'),
    1,
    sqlc.arg('holder_id'),
    lease_context.database_time
        + sqlc.arg('lease_duration_milliseconds')::BIGINT
        * INTERVAL '1 millisecond'
FROM lease_context
ON CONFLICT (lease_name) DO UPDATE
SET
    -- EXCLUDED is the incoming candidate; current_lease is the persisted lease.
    highest_writer_generation = EXCLUDED.highest_writer_generation,
    -- Preserve the fencing epoch only for an unexpired retry by the same holder and writer.
    lease_epoch = CASE
        WHEN
            current_lease.holder_id = EXCLUDED.holder_id
            AND current_lease.highest_writer_generation
                = EXCLUDED.highest_writer_generation
            AND current_lease.expires_at
                > (SELECT database_time FROM lease_context)
        THEN current_lease.lease_epoch
        ELSE current_lease.lease_epoch + 1
    END,
    holder_id = EXCLUDED.holder_id,
    -- Acquisition retries do not extend a live term; only renewal may do that.
    expires_at = CASE
        WHEN
            current_lease.holder_id = EXCLUDED.holder_id
            AND current_lease.highest_writer_generation
                = EXCLUDED.highest_writer_generation
            AND current_lease.expires_at
                > (SELECT database_time FROM lease_context)
        THEN current_lease.expires_at
        ELSE EXCLUDED.expires_at
    END
WHERE
    -- A newer writer may supersede a live lease; same-generation takeover waits for expiry.
    current_lease.dcs_cluster_id = EXCLUDED.dcs_cluster_id
    AND EXCLUDED.highest_writer_generation
        >= current_lease.highest_writer_generation
    AND (
        EXCLUDED.highest_writer_generation
            > current_lease.highest_writer_generation
        OR current_lease.holder_id = EXCLUDED.holder_id
        OR current_lease.expires_at
            <= (SELECT database_time FROM lease_context)
    )
RETURNING
    dcs_cluster_id,
    highest_writer_generation,
    lease_epoch,
    holder_id,
    expires_at,
    (SELECT database_time FROM lease_context)::TIMESTAMPTZ AS database_time;

-- name: ClassifyFleetRuntimeLeaseAcquisition :one
WITH lease_context AS (
    SELECT
        clock_timestamp() AS database_time,
        (
            NOT connected.in_recovery
            AND connected.server_address = sqlc.arg('server_address')::TEXT
            AND connected.server_port = sqlc.arg('server_port')::INTEGER
            AND connected.timeline = sqlc.arg('timeline')::BIGINT
        ) AS writer_matches
    FROM connected_postgres_identity AS connected
)
SELECT CASE
    WHEN NOT lease_context.writer_matches THEN 'writer_mismatch'
    WHEN current_lease.lease_name IS NULL THEN 'unavailable'
    WHEN current_lease.dcs_cluster_id IS DISTINCT FROM sqlc.arg('dcs_cluster_id') THEN 'cluster_mismatch'
    WHEN current_lease.highest_writer_generation IS DISTINCT FROM sqlc.arg('writer_generation') THEN 'writer_changed'
    WHEN
        current_lease.holder_id IS DISTINCT FROM sqlc.arg('holder_id')
        AND current_lease.expires_at > lease_context.database_time
    THEN 'contended'
    ELSE 'unavailable'
END::TEXT AS acquisition_result
FROM lease_context
LEFT JOIN fleet_runtime_lease AS current_lease
    ON current_lease.lease_name = 'fleet-active';

-- name: RenewFleetRuntimeLease :one
WITH lease_context AS (
    -- Capture database time once, and fail closed unless this is the expected writer.
    SELECT
        clock_timestamp() AS database_time
    FROM connected_postgres_identity AS connected
    WHERE
        NOT connected.in_recovery
        AND connected.server_address = sqlc.arg('server_address')::TEXT
        AND connected.server_port = sqlc.arg('server_port')::INTEGER
        AND connected.timeline = sqlc.arg('timeline')::BIGINT
)
UPDATE fleet_runtime_lease
SET
    expires_at = lease_context.database_time
        + sqlc.arg('lease_duration_milliseconds')::BIGINT
        * INTERVAL '1 millisecond'
FROM lease_context
WHERE
    -- Renewal requires the exact, unexpired ownership tuple on the expected writer.
    lease_name = 'fleet-active'
    AND dcs_cluster_id = sqlc.arg('dcs_cluster_id')
    AND highest_writer_generation = sqlc.arg('writer_generation')
    AND lease_epoch = sqlc.arg('lease_epoch')
    AND holder_id = sqlc.arg('holder_id')
    AND expires_at > lease_context.database_time
RETURNING
    fleet_runtime_lease.dcs_cluster_id,
    fleet_runtime_lease.highest_writer_generation,
    fleet_runtime_lease.lease_epoch,
    fleet_runtime_lease.holder_id,
    fleet_runtime_lease.expires_at,
    lease_context.database_time::TIMESTAMPTZ AS database_time;
