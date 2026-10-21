-- name: ConsumeHostedInvitationRateLimit :execrows
-- Each client has an independent one-minute window, shared across replicas.
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
