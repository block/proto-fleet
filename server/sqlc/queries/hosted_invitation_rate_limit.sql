-- name: ConsumeHostedInvitationRateLimit :execrows
-- Each client has independent one-minute and one-hour windows, shared across
-- replicas, so no single client can spend the whole deployment allowance. A
-- request the minute window rejects never reaches the hourly row. The hourly
-- key's prefix keeps it apart from minute keys, which are SHA-256 hex.
WITH minute AS (
    INSERT INTO hosted_invitation_rate_limit (client_hash, window_started_at, attempts)
    VALUES (sqlc.arg('client_hash'), statement_timestamp(), 1)
    ON CONFLICT (client_hash) DO UPDATE
    SET window_started_at = CASE
            WHEN hosted_invitation_rate_limit.window_started_at <= statement_timestamp() - interval '1 minute'
            THEN statement_timestamp() ELSE hosted_invitation_rate_limit.window_started_at END,
        attempts = CASE
            WHEN hosted_invitation_rate_limit.window_started_at <= statement_timestamp() - interval '1 minute'
            THEN 1 ELSE hosted_invitation_rate_limit.attempts + 1 END
    WHERE hosted_invitation_rate_limit.window_started_at <= statement_timestamp() - interval '1 minute'
        OR hosted_invitation_rate_limit.attempts < sqlc.arg('minute_limit')::integer
    RETURNING 1
)
INSERT INTO hosted_invitation_rate_limit (client_hash, window_started_at, attempts)
SELECT 'hourly:' || sqlc.arg('client_hash')::text, statement_timestamp(), 1
FROM minute
ON CONFLICT (client_hash) DO UPDATE
SET window_started_at = CASE
        WHEN hosted_invitation_rate_limit.window_started_at <= statement_timestamp() - interval '1 hour'
        THEN statement_timestamp() ELSE hosted_invitation_rate_limit.window_started_at END,
    attempts = CASE
        WHEN hosted_invitation_rate_limit.window_started_at <= statement_timestamp() - interval '1 hour'
        THEN 1 ELSE hosted_invitation_rate_limit.attempts + 1 END
WHERE hosted_invitation_rate_limit.window_started_at <= statement_timestamp() - interval '1 hour'
    OR hosted_invitation_rate_limit.attempts < sqlc.arg('hourly_limit')::integer;

-- name: ConsumeHostedInvitationDeploymentRateLimit :execrows
-- One hourly window shared by every client and replica bounds total code
-- guesses. The key cannot collide with client keys, which are SHA-256 hex.
INSERT INTO hosted_invitation_rate_limit (client_hash, window_started_at, attempts)
VALUES ('deployment', statement_timestamp(), 1)
ON CONFLICT (client_hash) DO UPDATE
SET window_started_at = CASE
        WHEN hosted_invitation_rate_limit.window_started_at <= statement_timestamp() - interval '1 hour'
        THEN statement_timestamp() ELSE hosted_invitation_rate_limit.window_started_at END,
    attempts = CASE
        WHEN hosted_invitation_rate_limit.window_started_at <= statement_timestamp() - interval '1 hour'
        THEN 1 ELSE hosted_invitation_rate_limit.attempts + 1 END
WHERE hosted_invitation_rate_limit.window_started_at <= statement_timestamp() - interval '1 hour'
    OR hosted_invitation_rate_limit.attempts < sqlc.arg('attempt_limit')::integer;

-- name: PruneHostedInvitationRateLimit :exec
-- Reclaim idle clients in bounded batches without waiting on active requests.
DELETE FROM hosted_invitation_rate_limit
WHERE client_hash IN (
    SELECT client_hash FROM hosted_invitation_rate_limit
    WHERE window_started_at <= statement_timestamp() - interval '1 hour'
    ORDER BY window_started_at
    LIMIT 100
    FOR UPDATE SKIP LOCKED
);
