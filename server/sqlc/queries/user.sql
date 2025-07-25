-- name: GetUserByUsername :one
SELECT *
FROM user
WHERE username = ?;

-- name: GetUserById :one
SELECT *
FROM user
WHERE id = ?;

-- name: CreateUser :execresult
INSERT INTO user (user_id, username, password_hash, created_at)
VALUES (?, ?, ?, ?);

-- name: UpdateUserPassword :exec
UPDATE user
SET password_hash = ?,
    updated_at = ?
WHERE id = ?;

-- name: UpdateUserUsername :exec
UPDATE user
SET username = ?,
    updated_at = NOW()
WHERE id = ?;

-- name: HasUser :one
SELECT COUNT(*) > 0
FROM user;
