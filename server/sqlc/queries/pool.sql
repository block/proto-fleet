-- name: GetPool :one
SELECT *
FROM pool
WHERE org_id = ?
  AND id = ?;

-- name: ListPools :many
SELECT *
FROM pool
WHERE org_id = ?
  AND deleted_at IS NULL
ORDER BY created_at DESC;

-- name: CreatePool :execresult
INSERT INTO pool (org_id, pool_name, url, username, password_enc, created_at)
VALUES (?, ?, ?, ?, ?, ?);

-- name: UpdatePool :exec
UPDATE pool
SET pool_name     = ?,
    url           = ?,
    username      = ?,
    password_enc = ?,
    updated_at    = ?
WHERE org_id = ?
  AND id = ?;

-- name: SoftDeletePool :exec
UPDATE pool
SET deleted_at = CURRENT_TIMESTAMP(6)
WHERE org_id = ?
  AND id = ?;

-- name: DeletePool :exec
DELETE
FROM pool
WHERE id = ?;

-- name: GetTotalPools :one
SELECT COUNT(*)
FROM pool
WHERE org_id = ?
  AND deleted_at IS NULL;
