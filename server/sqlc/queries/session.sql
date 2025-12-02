-- name: CreateSession :exec
INSERT INTO session (session_id, user_id, organization_id, user_agent, ip_address, created_at, last_activity, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetSessionByID :one
SELECT * FROM session WHERE session_id = ? AND revoked_at IS NULL;

-- name: UpdateSessionActivity :exec
UPDATE session
SET last_activity = ?, expires_at = ?
WHERE session_id = ? AND revoked_at IS NULL;

-- name: RevokeSession :exec
UPDATE session
SET revoked_at = ?
WHERE session_id = ?;

-- name: DeleteExpiredSessions :execresult
DELETE FROM session
WHERE expires_at < ? OR revoked_at IS NOT NULL;
