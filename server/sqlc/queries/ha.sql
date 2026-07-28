-- name: GetHAWritableIdentity :one
SELECT
    host(inet_server_addr())::TEXT AS server_address,
    inet_server_port()::INTEGER AS server_port,
    pg_is_in_recovery() AS in_recovery,
    (pg_control_checkpoint()).timeline_id::BIGINT AS timeline;

-- name: AcquireFleetRuntimeLease :one
WITH attempted AS (
    INSERT INTO fleet_runtime_lease (
        lease_name,
        dcs_cluster_id,
        highest_writer_generation,
        lease_epoch,
        holder_id,
        expires_at,
        updated_at
    )
    VALUES (
        'fleet-active',
        sqlc.arg('dcs_cluster_id'),
        sqlc.arg('writer_generation'),
        1,
        sqlc.arg('holder_id'),
        clock_timestamp()
            + sqlc.arg('lease_duration_milliseconds')::BIGINT
            * INTERVAL '1 millisecond',
        clock_timestamp()
    )
    ON CONFLICT (lease_name) DO UPDATE
    SET
        highest_writer_generation = EXCLUDED.highest_writer_generation,
        lease_epoch = CASE
            WHEN
                fleet_runtime_lease.holder_id = EXCLUDED.holder_id
                AND fleet_runtime_lease.highest_writer_generation
                    = EXCLUDED.highest_writer_generation
                AND fleet_runtime_lease.expires_at > clock_timestamp()
            THEN fleet_runtime_lease.lease_epoch
            ELSE fleet_runtime_lease.lease_epoch + 1
        END,
        holder_id = EXCLUDED.holder_id,
        expires_at = clock_timestamp()
            + sqlc.arg('lease_duration_milliseconds')::BIGINT
            * INTERVAL '1 millisecond',
        updated_at = clock_timestamp()
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
                    OR fleet_runtime_lease.expires_at <= clock_timestamp()
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
)
SELECT
    attempted.dcs_cluster_id,
    attempted.highest_writer_generation,
    attempted.lease_epoch,
    attempted.holder_id,
    attempted.expires_at,
    attempted.updated_at AS database_time,
    TRUE AS acquired
FROM attempted
UNION ALL
SELECT
    existing.dcs_cluster_id,
    existing.highest_writer_generation,
    existing.lease_epoch,
    existing.holder_id,
    existing.expires_at,
    existing.updated_at AS database_time,
    FALSE AS acquired
FROM fleet_runtime_lease AS existing
WHERE
    existing.lease_name = 'fleet-active'
    AND NOT EXISTS (SELECT 1 FROM attempted)
LIMIT 1;

-- name: RenewFleetRuntimeLease :one
UPDATE fleet_runtime_lease
SET
    expires_at = clock_timestamp()
        + sqlc.arg('lease_duration_milliseconds')::BIGINT
        * INTERVAL '1 millisecond',
    updated_at = clock_timestamp()
WHERE
    lease_name = 'fleet-active'
    AND dcs_cluster_id = sqlc.arg('dcs_cluster_id')
    AND highest_writer_generation = sqlc.arg('writer_generation')
    AND lease_epoch = sqlc.arg('lease_epoch')
    AND holder_id = sqlc.arg('holder_id')
    AND expires_at > clock_timestamp()
RETURNING
    dcs_cluster_id,
    highest_writer_generation,
    lease_epoch,
    holder_id,
    expires_at,
    updated_at AS database_time;
