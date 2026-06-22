-- name: InsertActivityLog :exec
-- The unique partial index on (batch_id, event_type) for '*.completed' event
-- types lets the Go layer detect idempotent re-inserts via pq unique_violation.
INSERT INTO activity_log (
    event_id,
    event_category, event_type, description,
    result, error_message,
    scope_type, scope_label, scope_count,
    actor_type, user_id, username,
    organization_id, metadata, batch_id,
    site_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16
);

-- name: ListActivityLogs :many
-- Array filter contract: the Go store layer must pass nil (not empty slice)
-- for the narg text[] filters below. An empty non-nil array
-- (pq.Array([]string{})) produces '{}' which matches nothing via ANY, leading
-- to zero results.
--
-- The site filter (site_ids / include_unassigned / org_level_categories) is an
-- arg, not a narg: the all-sites case is detected via cardinality() = 0, so the
-- Go layer must pass an empty (non-nil) bigint[] when no site filter is active,
-- matching the ListBuildings / ListRacks / ListMiners contract.
SELECT
    a.id, a.event_id, a.event_category, a.event_type, a.description,
    a.result, a.error_message,
    a.scope_type, a.scope_label, a.scope_count,
    a.actor_type, a.user_id, a.username,
    a.created_at, a.metadata, a.batch_id
FROM activity_log a
WHERE a.organization_id = sqlc.arg('org_id')
    AND (sqlc.narg('categories')::text[] IS NULL OR a.event_category = ANY(sqlc.narg('categories')::text[]))
    AND (sqlc.narg('event_types')::text[] IS NULL OR a.event_type = ANY(sqlc.narg('event_types')::text[]))
    AND (sqlc.narg('user_ids')::text[] IS NULL OR a.user_id = ANY(sqlc.narg('user_ids')::text[]))
    AND (sqlc.narg('scope_types')::text[] IS NULL OR a.scope_type = ANY(sqlc.narg('scope_types')::text[]))
    AND (sqlc.narg('search_pattern')::text IS NULL OR a.description ILIKE sqlc.narg('search_pattern') ESCAPE '\')
    AND (sqlc.narg('start_time')::timestamptz IS NULL OR a.created_at >= sqlc.narg('start_time'))
    AND (sqlc.narg('end_time')::timestamptz IS NULL OR a.created_at <= sqlc.narg('end_time'))
    AND (sqlc.narg('cursor_time')::timestamptz IS NULL OR (a.created_at, a.id) < (sqlc.narg('cursor_time')::timestamptz, sqlc.narg('cursor_id')::bigint))
    AND (
        -- all-sites: no site filter active
        (cardinality(sqlc.arg('site_ids')::bigint[]) = 0
         AND sqlc.arg('include_unassigned')::boolean = false)

        -- direct (non-batch) events: scalar site_id, Option B unassigned bucket
        -- TODO(#538): multi-device fleet writers (rename/unpair miners,
        -- collection add/remove-devices, device building-unassign) still emit
        -- NULL site_id with a non-org-level category, so they land here in the
        -- unassigned bucket instead of /{site}. Scope-stamp (single-source) or
        -- exclude them once they carry scope metadata.
        OR (a.batch_id IS NULL AND (
                a.site_id = ANY(sqlc.arg('site_ids')::bigint[])
             OR (sqlc.arg('include_unassigned')::boolean
                 AND a.site_id IS NULL
                 AND a.event_category <> ALL(sqlc.arg('org_level_categories')::text[]))
        ))

        -- command-batch events: derive touched sites from command_on_device_log
        OR (a.batch_id IS NOT NULL AND EXISTS (
                SELECT 1
                FROM command_on_device_log codl
                JOIN command_batch_log cbl ON cbl.id = codl.command_batch_log_id
                WHERE cbl.uuid = a.batch_id
                  AND (
                        codl.site_id = ANY(sqlc.arg('site_ids')::bigint[])
                     OR (sqlc.arg('include_unassigned')::boolean AND codl.site_id IS NULL)
                  )
        ))
    )
ORDER BY a.created_at DESC, a.id DESC
LIMIT sqlc.arg('page_size');

-- name: CountActivityLogs :one
-- Site filter must stay byte-for-byte identical to ListActivityLogs so the
-- pagination total never disagrees with the rendered feed (or the CSV export,
-- which reuses ListActivityLogs).
SELECT COUNT(*)
FROM activity_log a
WHERE a.organization_id = sqlc.arg('org_id')
    AND (sqlc.narg('categories')::text[] IS NULL OR a.event_category = ANY(sqlc.narg('categories')::text[]))
    AND (sqlc.narg('event_types')::text[] IS NULL OR a.event_type = ANY(sqlc.narg('event_types')::text[]))
    AND (sqlc.narg('user_ids')::text[] IS NULL OR a.user_id = ANY(sqlc.narg('user_ids')::text[]))
    AND (sqlc.narg('scope_types')::text[] IS NULL OR a.scope_type = ANY(sqlc.narg('scope_types')::text[]))
    AND (sqlc.narg('search_pattern')::text IS NULL OR a.description ILIKE sqlc.narg('search_pattern') ESCAPE '\')
    AND (sqlc.narg('start_time')::timestamptz IS NULL OR a.created_at >= sqlc.narg('start_time'))
    AND (sqlc.narg('end_time')::timestamptz IS NULL OR a.created_at <= sqlc.narg('end_time'))
    AND (
        (cardinality(sqlc.arg('site_ids')::bigint[]) = 0
         AND sqlc.arg('include_unassigned')::boolean = false)

        OR (a.batch_id IS NULL AND (
                a.site_id = ANY(sqlc.arg('site_ids')::bigint[])
             OR (sqlc.arg('include_unassigned')::boolean
                 AND a.site_id IS NULL
                 AND a.event_category <> ALL(sqlc.arg('org_level_categories')::text[]))
        ))

        OR (a.batch_id IS NOT NULL AND EXISTS (
                SELECT 1
                FROM command_on_device_log codl
                JOIN command_batch_log cbl ON cbl.id = codl.command_batch_log_id
                WHERE cbl.uuid = a.batch_id
                  AND (
                        codl.site_id = ANY(sqlc.arg('site_ids')::bigint[])
                     OR (sqlc.arg('include_unassigned')::boolean AND codl.site_id IS NULL)
                  )
        ))
    );

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
