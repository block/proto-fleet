-- name: LockHostedInvitationRateLimit :one
-- Issuance and admission both acquire this lock before inserting or looking up
-- a digest. Admission bounds this short transaction with a two-second timeout.
SELECT singleton FROM hosted_invitation_rate_limit FOR UPDATE;

-- name: ConsumeHostedInvitationRateLimit :execrows
-- Run after taking the lock, in READ COMMITTED, so the count includes every
-- issuance committed before admission. Count conservatively: even invitations
-- whose inviter lost authority cost budget until expiry/revocation/acceptance.
WITH expired AS (
    DELETE FROM hosted_invitation_rate_limit_attempt
    WHERE attempted_at <= statement_timestamp() - interval '1 hour'
), active AS (
    SELECT GREATEST(count(*), 1)::bigint AS cost
    FROM (
        SELECT invitation_id FROM hosted_account_invitation
        WHERE revoked_at IS NULL AND accepted_at IS NULL AND expires_at > statement_timestamp()
        LIMIT (sqlc.arg('exposure_limit')::bigint + 1)
    ) invitations
), recent AS (
    SELECT count(*) AS used_attempts, COALESCE(sum(exposure), 0)::bigint AS used_exposure
    FROM hosted_invitation_rate_limit_attempt
    WHERE attempted_at > statement_timestamp() - interval '1 hour'
)
INSERT INTO hosted_invitation_rate_limit_attempt (exposure)
SELECT active.cost
FROM active, recent
WHERE recent.used_attempts < sqlc.arg('attempt_limit')::bigint
    AND recent.used_exposure + active.cost <= sqlc.arg('exposure_limit')::bigint;

-- name: CountActiveHostedCustomerInvitations :one
-- Issuance runs this under the rate-limit lock, so the per-customer cap is
-- race-free. A customer's subclients share its allowance.
SELECT count(*)::bigint
FROM hosted_account_invitation i
JOIN hosted_account a ON a.organization_id = i.organization_id AND a.id = i.account_id
WHERE i.organization_id = sqlc.arg('organization_id')
    AND (a.id = sqlc.arg('customer_id') OR a.parent_id = sqlc.arg('customer_id'))
    AND i.revoked_at IS NULL AND i.accepted_at IS NULL AND i.expires_at > statement_timestamp();
