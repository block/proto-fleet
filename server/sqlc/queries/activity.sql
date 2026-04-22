-- name: InsertActivityLog :exec
-- The unique partial index on (batch_id, event_type) for '*.completed' event
-- types lets the Go layer detect idempotent re-inserts via pq unique_violation.
INSERT INTO activity_log (
    event_id,
    event_category, event_type, description,
    result, error_message,
    scope_type, scope_label, scope_count,
    actor_type, user_id, username,
    organization_id, metadata, batch_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15
);

-- name: ListActivityLogs :many
-- Array filter contract: the Go store layer must pass nil (not empty slice)
-- for inactive filters. An empty non-nil array (pq.Array([]string{})) produces
-- '{}' which matches nothing via ANY, leading to zero results.
SELECT
    id, event_id, event_category, event_type, description,
    result, error_message,
    scope_type, scope_label, scope_count,
    actor_type, user_id, username,
    created_at, metadata, batch_id
FROM activity_log
WHERE organization_id = sqlc.arg('org_id')
    AND (sqlc.narg('categories')::text[] IS NULL OR event_category = ANY(sqlc.narg('categories')::text[]))
    AND (sqlc.narg('event_types')::text[] IS NULL OR event_type = ANY(sqlc.narg('event_types')::text[]))
    AND (sqlc.narg('user_ids')::text[] IS NULL OR user_id = ANY(sqlc.narg('user_ids')::text[]))
    AND (sqlc.narg('scope_types')::text[] IS NULL OR scope_type = ANY(sqlc.narg('scope_types')::text[]))
    AND (sqlc.narg('search_pattern')::text IS NULL OR description ILIKE sqlc.narg('search_pattern') ESCAPE '\')
    AND (sqlc.narg('start_time')::timestamptz IS NULL OR created_at >= sqlc.narg('start_time'))
    AND (sqlc.narg('end_time')::timestamptz IS NULL OR created_at <= sqlc.narg('end_time'))
    AND (sqlc.narg('cursor_time')::timestamptz IS NULL OR (created_at, id) < (sqlc.narg('cursor_time')::timestamptz, sqlc.narg('cursor_id')::bigint))
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('page_size');

-- name: CountActivityLogs :one
SELECT COUNT(*)
FROM activity_log
WHERE organization_id = sqlc.arg('org_id')
    AND (sqlc.narg('categories')::text[] IS NULL OR event_category = ANY(sqlc.narg('categories')::text[]))
    AND (sqlc.narg('event_types')::text[] IS NULL OR event_type = ANY(sqlc.narg('event_types')::text[]))
    AND (sqlc.narg('user_ids')::text[] IS NULL OR user_id = ANY(sqlc.narg('user_ids')::text[]))
    AND (sqlc.narg('scope_types')::text[] IS NULL OR scope_type = ANY(sqlc.narg('scope_types')::text[]))
    AND (sqlc.narg('search_pattern')::text IS NULL OR description ILIKE sqlc.narg('search_pattern') ESCAPE '\')
    AND (sqlc.narg('start_time')::timestamptz IS NULL OR created_at >= sqlc.narg('start_time'))
    AND (sqlc.narg('end_time')::timestamptz IS NULL OR created_at <= sqlc.narg('end_time'));

-- name: GetDistinctActivityUsers :many
SELECT * FROM (
    SELECT DISTINCT ON (user_id) user_id, username
    FROM activity_log
    WHERE organization_id = sqlc.arg('org_id') AND user_id IS NOT NULL
    ORDER BY user_id, (username IS NULL) ASC, created_at DESC
) AS latest_users
ORDER BY username;

-- name: GetDistinctEventTypes :many
SELECT DISTINCT event_type, event_category
FROM activity_log
WHERE organization_id = sqlc.arg('org_id')
ORDER BY event_category, event_type;

-- name: GetDistinctScopeTypes :many
SELECT DISTINCT scope_type
FROM activity_log
WHERE organization_id = sqlc.arg('org_id') AND scope_type IS NOT NULL
ORDER BY scope_type;

-- name: DeleteActivityLogsOlderThan :execrows
-- Paginated retention delete of activity_log rows older than the cutoff.
-- Bounded by @max_rows so the cleaner keeps each transaction short; the
-- caller loops until this returns fewer rows than the limit.
DELETE FROM activity_log
WHERE id IN (
    SELECT al.id FROM activity_log al
    WHERE al.created_at < sqlc.arg('cutoff')
    ORDER BY al.created_at
    LIMIT sqlc.arg('max_rows')
);

-- name: GetLatestCompletedActivityForBatch :one
-- Returns the most recent '*.completed' activity row for a batch in the
-- caller's organization. Used by GetCommandBatchDeviceResults to render a
-- details_pruned response when the batch header in command_batch_log has
-- been retention-pruned but the activity row is still within retention
-- (defaults: BatchLogRetention=180d, ActivityLogRetention=365d, so the
-- activity row outlives its batch by up to ~6 months).
--
-- LIMIT 1 on id DESC picks the newest completion row; the partial unique
-- index on (batch_id, event_type) for '%.completed' guarantees at most
-- one row per batch anyway, but the bound keeps the query bounded even
-- if the index is ever relaxed.
SELECT event_type, result, scope_count, metadata, created_at
FROM activity_log
WHERE batch_id = $1
  AND organization_id = $2
  AND event_type LIKE '%.completed'
ORDER BY id DESC
LIMIT 1;

-- name: ListFinishedBatchesWithoutCompletion :many
-- Returns command batches that FINISHED but have no '<type>.completed' activity
-- row. Used by the reconciler to backfill completion events lost to a server
-- crash or exhausted finalizer retries.
--
-- Only batches whose creator already wrote an 'initiated' activity row are
-- returned; internally-triggered batches (e.g. worker-name reapply) therefore
-- stay out of the activity timeline.
--
-- The attribution (user_id, username, organization_id, actor_type) is sourced
-- from the initiated row so the completion event matches the original action
-- even when the session that kicked it off is long gone.
WITH first_initiated AS (
    SELECT DISTINCT ON (batch_id)
        batch_id,
        id,
        event_type,
        description,
        user_id,
        username,
        organization_id,
        actor_type
    FROM activity_log
    WHERE batch_id IS NOT NULL
      AND event_type NOT LIKE '%.completed'
    ORDER BY batch_id, id
)
SELECT
    cbl.uuid            AS batch_id,
    cbl.type            AS command_type,
    cbl.devices_count   AS devices_count,
    cbl.finished_at     AS finished_at,
    init.event_type     AS initiated_event_type,
    init.description    AS description,
    init.user_id        AS user_id,
    init.username       AS username,
    init.organization_id AS organization_id,
    init.actor_type     AS actor_type
FROM command_batch_log cbl
JOIN first_initiated init ON init.batch_id = cbl.uuid
WHERE cbl.status = 'FINISHED'
  AND cbl.finished_at IS NOT NULL
  AND cbl.finished_at < sqlc.arg('cutoff')
  AND NOT EXISTS (
    SELECT 1 FROM activity_log done
    WHERE done.batch_id = cbl.uuid
      AND done.event_type LIKE '%.completed'
  )
ORDER BY cbl.finished_at
LIMIT sqlc.arg('max_batches');
