-- name: GetUserByUsername :one
SELECT *
FROM user
WHERE username = ?;

-- name: CreateUser :execresult
INSERT INTO user (user_id, username, password_hash, created_at)
VALUES (?, ?, ?, ?);