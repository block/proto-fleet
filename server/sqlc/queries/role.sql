-- name: UpsertRole :one
-- PostgreSQL version returns the id using RETURNING
INSERT INTO role (name, description)
VALUES ($1, $2)
ON CONFLICT (name) DO UPDATE SET
    description = EXCLUDED.description,
    deleted_at = NULL
RETURNING id;

-- name: GetRoleByID :one
SELECT *
FROM role
WHERE id = $1
  AND deleted_at IS NULL;

-- name: GetRoleByName :one
SELECT * FROM role
WHERE name = $1;

-- name: ListRoles :many
SELECT *
FROM role
ORDER BY name;

-- name: UpdateRole :exec
UPDATE role
SET name        = $1,
    description = $2
WHERE id = $3;

-- name: SoftDeleteRole :exec
UPDATE role
SET deleted_at = CURRENT_TIMESTAMP
WHERE id = $1;

-- name: UndeleteRole :exec
UPDATE role
SET deleted_at = NULL
WHERE id = $1;
