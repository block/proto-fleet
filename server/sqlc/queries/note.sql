-- name: CreateNote :one
INSERT INTO note (org_id, user_id, content)
VALUES (sqlc.arg('org_id'), sqlc.arg('user_id'), sqlc.arg('content'))
RETURNING *;

-- name: GetNote :one
SELECT *
FROM note
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL;

-- name: ListNotes :many
-- Keyset pagination mirrors activity.sql: strict (created_at, id) tuple
-- descent, newest first. The "user" join supplies the display username;
-- it deliberately ignores user.deleted_at so a deactivated author still
-- attributes.
SELECT
    n.id,
    n.org_id,
    n.user_id,
    u.username AS author_username,
    n.content,
    n.created_at,
    n.updated_at
FROM note n
JOIN "user" u ON u.id = n.user_id
WHERE n.org_id = sqlc.arg('org_id')
  AND n.deleted_at IS NULL
  AND (sqlc.narg('cursor_time')::timestamptz IS NULL
       OR (n.created_at, n.id) < (sqlc.narg('cursor_time')::timestamptz, sqlc.narg('cursor_id')::bigint))
ORDER BY n.created_at DESC, n.id DESC
LIMIT sqlc.arg('page_size');

-- name: UpdateNoteContent :one
-- The author predicate lives in the WHERE so the ownership check cannot
-- race the domain layer's read; zero rows maps to NotFound at the store.
UPDATE note
SET content = sqlc.arg('content')
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND user_id = sqlc.arg('user_id')
  AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteNote :execrows
UPDATE note
SET deleted_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL;
