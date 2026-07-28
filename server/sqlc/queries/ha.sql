-- name: GetHAWritableIdentity :one
SELECT
    host(inet_server_addr())::TEXT AS server_address,
    inet_server_port()::INTEGER AS server_port,
    pg_is_in_recovery() AS in_recovery,
    (pg_control_checkpoint()).timeline_id::BIGINT AS timeline;

-- name: AcquireFleetRuntimeLease :one
WITH writer_identity AS MATERIALIZED (
    SELECT
        clock_timestamp() AS database_time,
        (
            NOT pg_is_in_recovery()
            AND host(inet_server_addr()) = sqlc.arg('server_address')::TEXT
            AND inet_server_port() = sqlc.arg('server_port')::INTEGER
            AND (pg_control_checkpoint()).timeline_id::BIGINT
                = sqlc.arg('timeline')::BIGINT
        ) AS matches
),
attempted AS (
    INSERT INTO fleet_runtime_lease (
        lease_name,
        dcs_cluster_id,
        highest_writer_generation,
        lease_epoch,
        holder_id,
        expires_at,
        updated_at
    )
    SELECT
        'fleet-active',
        sqlc.arg('dcs_cluster_id'),
        sqlc.arg('writer_generation'),
        1,
        sqlc.arg('holder_id'),
        writer_identity.database_time
            + sqlc.arg('lease_duration_milliseconds')::BIGINT
            * INTERVAL '1 millisecond',
        writer_identity.database_time
    FROM writer_identity
    WHERE writer_identity.matches
    ON CONFLICT (lease_name) DO UPDATE
    SET
        highest_writer_generation = EXCLUDED.highest_writer_generation,
        lease_epoch = CASE
            WHEN
                fleet_runtime_lease.holder_id = EXCLUDED.holder_id
                AND fleet_runtime_lease.highest_writer_generation
                    = EXCLUDED.highest_writer_generation
                AND fleet_runtime_lease.expires_at
                    > (SELECT database_time FROM writer_identity)
            THEN fleet_runtime_lease.lease_epoch
            ELSE fleet_runtime_lease.lease_epoch + 1
        END,
        holder_id = EXCLUDED.holder_id,
        expires_at = (SELECT database_time FROM writer_identity)
            + sqlc.arg('lease_duration_milliseconds')::BIGINT
            * INTERVAL '1 millisecond',
        updated_at = (SELECT database_time FROM writer_identity)
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
                        <= (SELECT database_time FROM writer_identity)
                )
            )
        )
    RETURNING
        dcs_cluster_id,
        highest_writer_generation,
        lease_epoch,
        holder_id,
        expires_at,
        updated_at
),
existing AS MATERIALIZED (
    SELECT
        dcs_cluster_id,
        highest_writer_generation,
        lease_epoch,
        holder_id,
        expires_at
    FROM fleet_runtime_lease
    WHERE lease_name = 'fleet-active'
)
SELECT
    attempted.dcs_cluster_id,
    attempted.highest_writer_generation,
    attempted.lease_epoch,
    attempted.holder_id,
    attempted.expires_at,
    writer_identity.database_time::TIMESTAMPTZ AS database_time,
    TRUE AS acquired,
    TRUE AS writer_matches
FROM attempted
INNER JOIN writer_identity ON TRUE
UNION ALL
SELECT
    existing.dcs_cluster_id,
    existing.highest_writer_generation,
    existing.lease_epoch,
    existing.holder_id,
    existing.expires_at,
    writer_identity.database_time::TIMESTAMPTZ AS database_time,
    FALSE AS acquired,
    writer_identity.matches AS writer_matches
FROM existing
INNER JOIN writer_identity ON TRUE
WHERE NOT EXISTS (SELECT 1 FROM attempted)
UNION ALL
SELECT
    sqlc.arg('dcs_cluster_id')::TEXT AS dcs_cluster_id,
    sqlc.arg('writer_generation')::BIGINT AS highest_writer_generation,
    0::BIGINT AS lease_epoch,
    sqlc.arg('holder_id')::UUID AS holder_id,
    writer_identity.database_time AS expires_at,
    writer_identity.database_time::TIMESTAMPTZ AS database_time,
    FALSE AS acquired,
    FALSE AS writer_matches
FROM writer_identity
WHERE
    NOT writer_identity.matches
    AND NOT EXISTS (SELECT 1 FROM attempted)
    AND NOT EXISTS (SELECT 1 FROM existing)
LIMIT 1;

-- name: RenewFleetRuntimeLease :one
WITH writer_identity AS MATERIALIZED (
    SELECT
        clock_timestamp() AS database_time,
        (
            NOT pg_is_in_recovery()
            AND host(inet_server_addr()) = sqlc.arg('server_address')::TEXT
            AND inet_server_port() = sqlc.arg('server_port')::INTEGER
            AND (pg_control_checkpoint()).timeline_id::BIGINT
                = sqlc.arg('timeline')::BIGINT
        ) AS matches
),
renewed AS (
    UPDATE fleet_runtime_lease
    SET
        expires_at = writer_identity.database_time
            + sqlc.arg('lease_duration_milliseconds')::BIGINT
            * INTERVAL '1 millisecond',
        updated_at = writer_identity.database_time
    FROM writer_identity
    WHERE
        writer_identity.matches
        AND lease_name = 'fleet-active'
        AND dcs_cluster_id = sqlc.arg('dcs_cluster_id')
        AND highest_writer_generation = sqlc.arg('writer_generation')
        AND lease_epoch = sqlc.arg('lease_epoch')
        AND holder_id = sqlc.arg('holder_id')
        AND expires_at > writer_identity.database_time
    RETURNING
        fleet_runtime_lease.dcs_cluster_id,
        fleet_runtime_lease.highest_writer_generation,
        fleet_runtime_lease.lease_epoch,
        fleet_runtime_lease.holder_id,
        fleet_runtime_lease.expires_at
)
SELECT
    renewed.dcs_cluster_id,
    renewed.highest_writer_generation,
    renewed.lease_epoch,
    renewed.holder_id,
    renewed.expires_at,
    writer_identity.database_time::TIMESTAMPTZ AS database_time,
    TRUE AS writer_matches
FROM renewed
INNER JOIN writer_identity ON TRUE
UNION ALL
SELECT
    sqlc.arg('dcs_cluster_id')::TEXT AS dcs_cluster_id,
    sqlc.arg('writer_generation')::BIGINT AS highest_writer_generation,
    sqlc.arg('lease_epoch')::BIGINT AS lease_epoch,
    sqlc.arg('holder_id')::UUID AS holder_id,
    writer_identity.database_time AS expires_at,
    writer_identity.database_time::TIMESTAMPTZ AS database_time,
    FALSE AS writer_matches
FROM writer_identity
WHERE NOT writer_identity.matches
LIMIT 1;
