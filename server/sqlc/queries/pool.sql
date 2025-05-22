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
ORDER BY pool_priority ASC;

-- name: CreatePool :execresult
INSERT INTO pool (org_id, pool_name, url, username, password_enc, pool_priority, pool_status, is_default, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpdatePool :exec
UPDATE pool
SET pool_name     = ?,
    url           = ?,
    username      = ?,
    password_enc = ?,
    pool_priority = ?,
    pool_status   = ?,
    is_default    = ?,
    updated_at    = ?
WHERE org_id = ?
  AND id = ?;

-- name: UpdatePoolPriority :exec
UPDATE pool
SET pool_priority = ?,
    updated_at    = ?
WHERE org_id = ?
  AND id = ?;

-- name: UnsetDefaultPool :exec
UPDATE pool
SET is_default = FALSE,
    updated_at    = ?
WHERE org_id = ?
  AND is_default = TRUE;

-- name: SoftDeletePool :exec
UPDATE pool
SET deleted_at = CURRENT_TIMESTAMP(6)
WHERE org_id = ?
  AND id = ?;

-- name: GetTotalPools :one
SELECT COUNT(*)
FROM pool
WHERE org_id = ?
  AND deleted_at IS NULL;
