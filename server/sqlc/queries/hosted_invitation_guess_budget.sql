-- name: LockHostedInvitationGuessBudget :one
-- Issuance and admission both acquire this lock before inserting or looking up
-- a digest. Admission bounds this short transaction with a two-second timeout.
SELECT singleton FROM hosted_invitation_guess_budget WHERE singleton FOR UPDATE;

-- name: PruneHostedInvitationGuesses :exec
DELETE FROM hosted_invitation_guess_attempt
WHERE attempted_at <= statement_timestamp() - interval '1 hour';

-- name: ChargeHostedInvitationGuess :execrows
-- Run after taking the lock, in READ COMMITTED, so the count includes every
-- issuance committed before admission. Count conservatively: even invitations
-- whose inviter lost authority cost budget until expiry/revocation/acceptance.
WITH active AS (
    SELECT GREATEST(count(*), 1)::bigint AS cost
    FROM (
        SELECT invitation_id FROM hosted_account_invitation
        WHERE revoked_at IS NULL AND accepted_at IS NULL AND expires_at > statement_timestamp()
        LIMIT (sqlc.arg('exposure_limit')::bigint + 1)
    ) invitations
), budget AS (
    SELECT count(*) AS used_attempts, COALESCE(sum(exposure), 0)::bigint AS used_exposure
    FROM hosted_invitation_guess_attempt
    WHERE attempted_at > statement_timestamp() - interval '1 hour'
)
INSERT INTO hosted_invitation_guess_attempt (exposure)
SELECT active.cost
FROM active, budget
WHERE budget.used_attempts < sqlc.arg('attempt_limit')::bigint
    AND budget.used_exposure + active.cost <= sqlc.arg('exposure_limit')::bigint;
