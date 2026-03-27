-- name: InsertActivityLog :exec
INSERT INTO activity_log (
    event_id,
    event_category, event_type, description,
    result, error_message,
    scope_type, scope_label, scope_count,
    actor_type, user_id, username,
    organization_id, metadata
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
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
    created_at, metadata
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
