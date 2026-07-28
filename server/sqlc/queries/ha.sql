-- name: GetConnectedPostgresIdentity :one
SELECT
    server_address,
    server_port,
    in_recovery,
    timeline
FROM connected_postgres_identity;

-- name: AcquireFleetRuntimeLease :one
WITH lease_context AS (
    SELECT
        clock_timestamp() AS database_time,
        (
            NOT connected.in_recovery
            AND connected.server_address = sqlc.arg('server_address')::TEXT
            AND connected.server_port = sqlc.arg('server_port')::INTEGER
            AND connected.timeline = sqlc.arg('timeline')::BIGINT
        ) AS matches
    FROM connected_postgres_identity AS connected
)
INSERT INTO fleet_runtime_lease (
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
WHERE lease_context.matches
ON CONFLICT (lease_name) DO UPDATE
SET
    highest_writer_generation = EXCLUDED.highest_writer_generation,
    lease_epoch = CASE
        WHEN
            fleet_runtime_lease.holder_id = EXCLUDED.holder_id
            AND fleet_runtime_lease.highest_writer_generation
                = EXCLUDED.highest_writer_generation
            AND fleet_runtime_lease.expires_at
                > (SELECT database_time FROM lease_context)
        THEN fleet_runtime_lease.lease_epoch
        ELSE fleet_runtime_lease.lease_epoch + 1
    END,
    holder_id = EXCLUDED.holder_id,
    expires_at = EXCLUDED.expires_at
WHERE
    fleet_runtime_lease.dcs_cluster_id = EXCLUDED.dcs_cluster_id
    AND (
        EXCLUDED.highest_writer_generation
            > fleet_runtime_lease.highest_writer_generation
        OR (
            EXCLUDED.highest_writer_generation
                = fleet_runtime_lease.highest_writer_generation
            AND (
                fleet_runtime_lease.holder_id = EXCLUDED.holder_id
                OR fleet_runtime_lease.expires_at
                    <= (SELECT database_time FROM lease_context)
            )
        )
    )
RETURNING
    dcs_cluster_id,
    highest_writer_generation,
    lease_epoch,
    holder_id,
    expires_at,
    (SELECT database_time FROM lease_context)::TIMESTAMPTZ AS database_time;

-- name: RenewFleetRuntimeLease :one
WITH lease_context AS (
    SELECT
        clock_timestamp() AS database_time,
        (
            NOT connected.in_recovery
            AND connected.server_address = sqlc.arg('server_address')::TEXT
            AND connected.server_port = sqlc.arg('server_port')::INTEGER
            AND connected.timeline = sqlc.arg('timeline')::BIGINT
        ) AS matches
    FROM connected_postgres_identity AS connected
)
UPDATE fleet_runtime_lease
SET
    expires_at = lease_context.database_time
        + sqlc.arg('lease_duration_milliseconds')::BIGINT
        * INTERVAL '1 millisecond'
FROM lease_context
WHERE
    lease_context.matches
    AND lease_name = 'fleet-active'
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
