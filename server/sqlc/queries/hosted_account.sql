-- name: LockHostedAccountOrganization :one
-- Serialize hierarchy, policy, and membership mutations for one organization.
-- Allow foreign-key KEY SHARE locks from concurrent organization role writes.
SELECT id FROM organization WHERE id = $1 AND deleted_at IS NULL FOR NO KEY UPDATE;

-- name: LockHostedAccountOrganizationForShare :one
-- Reports may overlap, while hierarchy, policy, membership and allocation
-- mutations keep their exclusive organization admission lock.
SELECT id FROM organization WHERE id = $1 AND deleted_at IS NULL FOR SHARE;

-- name: LockActiveHostedAccountUserForShare :one
SELECT u.* FROM "user" u
JOIN user_organization uo ON uo.user_id = u.id
WHERE u.id = $1 AND uo.organization_id = $2
  AND u.deleted_at IS NULL AND uo.deleted_at IS NULL
FOR SHARE OF u, uo;

-- name: GetHostedAccountPath :many
WITH RECURSIVE path AS (
    SELECT a.* FROM hosted_account a
    WHERE a.organization_id = $1 AND a.account_id = $2
    UNION ALL
    SELECT parent.* FROM hosted_account parent
    JOIN path child ON parent.id = child.parent_id AND parent.organization_id = child.organization_id
)
SELECT * FROM path ORDER BY depth;

-- name: LockActiveHostedAccountUser :one
SELECT u.* FROM "user" u
JOIN user_organization uo ON uo.user_id = u.id
WHERE u.id = $1 AND uo.organization_id = $2
  AND u.deleted_at IS NULL AND uo.deleted_at IS NULL
FOR UPDATE OF u, uo;

-- name: CreateHostedAccount :one
INSERT INTO hosted_account (account_id, organization_id, parent_id, depth, name, capabilities)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING *;

-- name: ConfigureHostedAccount :exec
UPDATE hosted_account SET capabilities = $1, suspended = $2,
    revision = revision + 1, authority_revision = authority_revision + 1, updated_at = CURRENT_TIMESTAMP
WHERE organization_id = $3 AND id = $4;

-- name: SetHostedAccountMembership :exec
INSERT INTO hosted_account_membership (organization_id, account_id, user_id, role)
VALUES ($1, $2, $3, $4)
ON CONFLICT (organization_id, account_id, user_id) DO UPDATE
SET role = EXCLUDED.role, revoked_at = NULL,
    revision = hosted_account_membership.revision + 1, updated_at = CURRENT_TIMESTAMP;

-- name: RevokeHostedAccountMembership :exec
UPDATE hosted_account_membership
SET revoked_at = CURRENT_TIMESTAMP, revision = revision + 1, updated_at = CURRENT_TIMESTAMP
WHERE organization_id = $1 AND account_id = $2 AND user_id = $3 AND revoked_at IS NULL;

-- name: GetHostedAccountMembership :one
SELECT * FROM hosted_account_membership
WHERE organization_id = $1 AND account_id = $2 AND user_id = $3 AND revoked_at IS NULL;

-- name: LockHostedAccountAdministrators :many
SELECT u.id FROM hosted_account_membership m
JOIN "user" u ON u.id = m.user_id AND u.deleted_at IS NULL
JOIN user_organization uo ON uo.user_id = u.id AND uo.organization_id = m.organization_id AND uo.deleted_at IS NULL
WHERE m.organization_id = $1 AND m.account_id = $2
    AND m.role = 'account_administrator' AND m.revoked_at IS NULL
ORDER BY u.id FOR UPDATE OF m, u, uo;

-- name: ResolveHostedAccountMembership :one
-- Resolve identity, membership, suspension, and ancestor ceilings in one snapshot.
SELECT a.account_id, m.role,
    (a.capabilities & COALESCE(parent.capabilities, 31) & root.capabilities)::bigint AS capabilities
FROM hosted_account a
JOIN hosted_account root ON root.organization_id = a.organization_id AND root.depth = 0
LEFT JOIN hosted_account parent ON parent.id = a.parent_id AND parent.organization_id = a.organization_id
JOIN hosted_account_membership m ON m.account_id = a.id AND m.organization_id = a.organization_id
JOIN "user" u ON u.id = m.user_id AND u.deleted_at IS NULL
JOIN user_organization uo ON uo.user_id = u.id AND uo.organization_id = a.organization_id AND uo.deleted_at IS NULL
JOIN organization o ON o.id = a.organization_id AND o.deleted_at IS NULL
WHERE a.organization_id = $1 AND a.account_id = $2 AND m.user_id = $3
    AND m.revoked_at IS NULL AND NOT a.suspended AND NOT root.suspended
    AND NOT COALESCE(parent.suspended, FALSE) AND a.depth > 0;
