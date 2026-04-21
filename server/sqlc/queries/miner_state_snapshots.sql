-- name: InsertMinerStateSnapshotBatch :exec
-- Single multi-row INSERT per tick; unnest expands the count arrays so every
-- org in the tick shares one round-trip and one transaction.
INSERT INTO miner_state_snapshots (
    time,
    org_id,
    hashing_count,
    broken_count,
    offline_count,
    sleeping_count
)
SELECT
    sqlc.arg('time')::timestamptz,
    UNNEST(sqlc.arg('org_ids')::bigint[]),
    UNNEST(sqlc.arg('hashing_counts')::int[]),
    UNNEST(sqlc.arg('broken_counts')::int[]),
    UNNEST(sqlc.arg('offline_counts')::int[]),
    UNNEST(sqlc.arg('sleeping_counts')::int[]);

-- name: GetMinerStateSnapshots :many
-- last() keeps the four counts from one real tuple; column-wise averaging
-- would round each independently and inflate the bucket sum past fleet size.
SELECT
    time_bucket(sqlc.arg('bucket_interval')::text::interval, time)::timestamptz AS bucket,
    last(hashing_count, time)::int  AS hashing_count,
    last(broken_count, time)::int   AS broken_count,
    last(offline_count, time)::int  AS offline_count,
    last(sleeping_count, time)::int AS sleeping_count
FROM miner_state_snapshots
WHERE org_id = sqlc.arg('org_id')
  AND time >= sqlc.arg('start_time')
  AND time <= sqlc.arg('end_time')
GROUP BY bucket
ORDER BY bucket ASC;

-- name: ListOrgIDsForSnapshots :many
SELECT id
FROM organization
WHERE deleted_at IS NULL
ORDER BY id;
