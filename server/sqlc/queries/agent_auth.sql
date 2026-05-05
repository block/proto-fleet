-- name: CreateAgentAuthChallenge :exec
INSERT INTO agent_auth_challenge (challenge, agent_id, expires_at)
VALUES ($1, $2, $3);

-- name: ConsumeAgentAuthChallenge :one
DELETE FROM agent_auth_challenge
WHERE challenge = $1 AND expires_at >= $2
RETURNING challenge, agent_id, expires_at, created_at;

-- name: SweepExpiredAgentAuthChallenges :execrows
DELETE FROM agent_auth_challenge
WHERE expires_at < $1;

-- name: CreateAgentSession :exec
INSERT INTO agent_session (token_hash, agent_id, expires_at)
VALUES ($1, $2, $3);

-- name: GetAgentSessionByTokenHash :one
SELECT s.token_hash, s.agent_id, s.expires_at, s.created_at,
       a.org_id, a.name, a.identity_pubkey
FROM agent_session s
JOIN agent a ON s.agent_id = a.id
WHERE s.token_hash = $1
  AND s.expires_at >= $2
  AND a.deleted_at IS NULL
  AND a.enrollment_status = 'CONFIRMED';

-- name: SweepExpiredAgentSessions :execrows
DELETE FROM agent_session
WHERE expires_at < $1;
