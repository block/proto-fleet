-- name: UpsertFleetNodeAuthChallenge :exec
INSERT INTO fleet_node_auth_challenge (challenge, fleet_node_id, expires_at)
VALUES ($1, $2, $3)
ON CONFLICT (fleet_node_id) DO UPDATE
SET challenge = EXCLUDED.challenge,
    expires_at = EXCLUDED.expires_at,
    created_at = CURRENT_TIMESTAMP;

-- name: ConsumeFleetNodeAuthChallenge :one
DELETE FROM fleet_node_auth_challenge
WHERE challenge = $1 AND expires_at >= $2
RETURNING challenge, fleet_node_id, expires_at, created_at;

-- name: SweepExpiredFleetNodeAuthChallenges :execrows
DELETE FROM fleet_node_auth_challenge
WHERE expires_at < $1;

-- name: UpsertFleetNodeSession :exec
INSERT INTO fleet_node_session (token_hash, fleet_node_id, expires_at)
VALUES ($1, $2, $3)
ON CONFLICT (fleet_node_id) DO UPDATE
SET token_hash = EXCLUDED.token_hash,
    expires_at = EXCLUDED.expires_at,
    created_at = CURRENT_TIMESTAMP;

-- name: GetFleetNodeSessionByTokenHash :one
SELECT s.token_hash, s.fleet_node_id, s.expires_at, s.created_at,
       a.org_id, a.name, a.identity_pubkey
FROM fleet_node_session s
JOIN fleet_node a ON s.fleet_node_id = a.id
WHERE s.token_hash = $1
  AND s.expires_at >= $2
  AND a.deleted_at IS NULL
  AND a.enrollment_status = 'CONFIRMED';

-- name: SweepExpiredFleetNodeSessions :execrows
DELETE FROM fleet_node_session
WHERE expires_at < $1;
