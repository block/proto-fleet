-- name: CreateSession :exec
INSERT INTO session (session_id, user_id, organization_id, user_agent, ip_address, created_at, last_activity, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: GetSessionByID :one
SELECT * FROM session WHERE session_id = $1 AND revoked_at IS NULL;

-- name: UpdateSessionActivity :exec
UPDATE session
SET last_activity = $1, expires_at = $2
WHERE session_id = $3 AND revoked_at IS NULL;

-- name: RevokeSession :exec
UPDATE session
SET revoked_at = $1
WHERE session_id = $2;

-- name: RevokeAllSessionsByUserID :exec
UPDATE session
SET revoked_at = $1
WHERE user_id = $2 AND revoked_at IS NULL;

-- name: DeleteExpiredSessions :execresult
DELETE FROM session
WHERE expires_at < $1 OR revoked_at IS NOT NULL;
