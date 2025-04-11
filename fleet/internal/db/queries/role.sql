-- name: UpsertRole :execresult
INSERT INTO role (name, description)
VALUES (?, ?)
ON DUPLICATE KEY UPDATE
    description = VALUES(description),
    deleted_at = NULL,
    id = LAST_INSERT_ID(id);

-- name: GetRoleByID :one
SELECT *
FROM role
WHERE id = ?
  AND deleted_at IS NULL;

-- name: GetRoleByName :one
SELECT * FROM role
WHERE name = ?;

-- name: ListRoles :many
SELECT *
FROM role
ORDER BY name;

-- name: UpdateRole :exec
UPDATE role
SET name        = ?,
    description = ?
WHERE id = ?;

-- name: SoftDeleteRole :exec
UPDATE role
SET deleted_at = CURRENT_TIMESTAMP(6)
WHERE id = ?;

-- name: UndeleteRole :exec
UPDATE role
SET deleted_at = NULL
WHERE id = ?;