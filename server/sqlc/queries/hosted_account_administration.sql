-- name: ListHostedAccounts :many
SELECT * FROM hosted_account WHERE organization_id = $1 ORDER BY depth, name, account_id;

-- name: ListHostedAccountChildren :many
SELECT * FROM hosted_account WHERE organization_id = $1 AND parent_id = $2 ORDER BY name, account_id;

-- name: ListHostedAccountMembers :many
SELECT u.user_id, u.username, m.role, m.revision, m.created_at
FROM hosted_account_membership m JOIN "user" u ON u.id = m.user_id AND u.deleted_at IS NULL
JOIN user_organization uo ON uo.user_id = u.id AND uo.organization_id = m.organization_id AND uo.deleted_at IS NULL
WHERE m.organization_id = $1 AND m.account_id = $2 AND m.revoked_at IS NULL
ORDER BY u.username, u.user_id;

-- name: ListHostedAccountAncestorGrants :many
SELECT * FROM hosted_account_ancestor_grant
WHERE organization_id = $1 AND account_id = $2 AND revoked_at IS NULL;

-- name: SetHostedAccountAncestorGrant :exec
INSERT INTO hosted_account_ancestor_grant (organization_id, account_id, ancestor_id, capabilities)
VALUES ($1, $2, $3, $4)
ON CONFLICT (organization_id, account_id, ancestor_id) DO UPDATE
SET capabilities = EXCLUDED.capabilities, revision = hosted_account_ancestor_grant.revision + 1,
    revoked_at = NULL, updated_at = CURRENT_TIMESTAMP;

-- name: RevokeHostedAccountAncestorGrant :exec
UPDATE hosted_account_ancestor_grant SET revoked_at = CURRENT_TIMESTAMP,
    revision = revision + 1, updated_at = CURRENT_TIMESTAMP
WHERE organization_id = $1 AND account_id = $2 AND ancestor_id = $3 AND revoked_at IS NULL;

-- name: ListHostedAccountSites :many
SELECT hs.site_id, s.name, hs.parent_controlled FROM hosted_account_site hs
JOIN site s ON s.id = hs.site_id AND s.org_id = hs.organization_id AND s.deleted_at IS NULL
WHERE hs.organization_id = $1 AND hs.account_id = $2 ORDER BY s.name, hs.site_id;

-- name: DeleteHostedAccountSites :exec
DELETE FROM hosted_account_site WHERE organization_id = $1 AND account_id = $2 AND parent_controlled = $3;

-- name: SetHostedAccountSitesRestricted :exec
UPDATE hosted_account SET
    sites_restricted = CASE WHEN sqlc.arg(parent_controlled)::boolean THEN sites_restricted ELSE sqlc.arg(restricted)::boolean END,
    site_ceiling_restricted = CASE WHEN sqlc.arg(parent_controlled)::boolean THEN sqlc.arg(restricted)::boolean ELSE site_ceiling_restricted END,
    revision = revision + 1,
    authority_revision = authority_revision + 1, updated_at = CURRENT_TIMESTAMP
WHERE organization_id = sqlc.arg(organization_id) AND id = sqlc.arg(id);

-- name: AddHostedAccountSite :execrows
INSERT INTO hosted_account_site (organization_id, account_id, site_id, parent_controlled)
SELECT $1, $2, s.id, sqlc.arg(parent_controlled)::boolean FROM site s WHERE s.id = $3 AND s.org_id = $1 AND s.deleted_at IS NULL
ON CONFLICT DO NOTHING;

-- name: AddHostedAccountUserAffiliation :exec
INSERT INTO user_organization (user_id, organization_id, role_id) VALUES ($1, $2, NULL);

-- name: CreateHostedAccountInvitation :one
INSERT INTO hosted_account_invitation
    (invitation_id, organization_id, account_id, inviter_id, authority_fingerprint, token_hash, role, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING *;

-- name: GetHostedAccountInvitationByHash :one
SELECT i.*, a.account_id AS external_account_id, a.name AS account_name
FROM hosted_account_invitation i JOIN hosted_account a ON a.id = i.account_id AND a.organization_id = i.organization_id
WHERE i.token_hash = $1;

-- name: ListHostedAccountInvitations :many
SELECT invitation_id, role, expires_at, revoked_at, accepted_at, created_at
FROM hosted_account_invitation WHERE organization_id = $1 AND account_id = $2
ORDER BY created_at DESC, invitation_id;

-- name: RevokeHostedAccountInvitation :execrows
UPDATE hosted_account_invitation SET revoked_at = CURRENT_TIMESTAMP
WHERE organization_id = $1 AND account_id = $2 AND invitation_id = $3
    AND revoked_at IS NULL AND accepted_at IS NULL;

-- name: ConsumeHostedAccountInvitation :execrows
UPDATE hosted_account_invitation SET accepted_at = CURRENT_TIMESTAMP, accepted_user_id = $1
WHERE invitation_id = $2 AND revoked_at IS NULL AND accepted_at IS NULL AND expires_at > CURRENT_TIMESTAMP;

-- name: CountHostedAccountsWithoutAdministrator :one
-- Administration must remain recoverable after suspension is lifted. Check
-- live identities and capability ceilings even for currently suspended accounts.
SELECT COUNT(*) FROM hosted_account a
JOIN hosted_account root ON root.organization_id = a.organization_id AND root.depth = 0
LEFT JOIN hosted_account parent ON parent.organization_id = a.organization_id AND parent.id = a.parent_id
WHERE a.organization_id = $1 AND a.depth > 0 AND NOT EXISTS (
    SELECT 1 FROM hosted_account_membership m
    JOIN "user" u ON u.id = m.user_id AND u.deleted_at IS NULL
    JOIN user_organization uo ON uo.user_id = u.id AND uo.organization_id = m.organization_id AND uo.deleted_at IS NULL
    WHERE m.organization_id = a.organization_id AND m.account_id = a.id
        AND m.revoked_at IS NULL AND m.role = 'account_administrator'
        AND (a.capabilities & COALESCE(parent.capabilities, 31) & root.capabilities & 8) <> 0
        AND u.id <> sqlc.arg(excluded_user_id)
) AND NOT EXISTS (
    SELECT 1 FROM hosted_account_ancestor_grant g
    WHERE g.organization_id = a.organization_id AND g.account_id = a.id AND g.revoked_at IS NULL
        AND (g.capabilities & a.capabilities & COALESCE(parent.capabilities, 31) & root.capabilities & 8) <> 0
        AND (
            (g.ancestor_id = root.id AND EXISTS (
                SELECT 1 FROM user_organization_role ur
                JOIN "user" u ON u.id = ur.user_id AND u.deleted_at IS NULL
                JOIN user_organization uo ON uo.user_id = u.id AND uo.organization_id = ur.organization_id AND uo.deleted_at IS NULL
                JOIN role r ON r.id = ur.role_id AND r.organization_id = ur.organization_id AND r.deleted_at IS NULL
                JOIN role_permission rp ON rp.role_id = r.id JOIN permission p ON p.id = rp.permission_id
                WHERE ur.organization_id = a.organization_id AND ur.scope_type = 'org' AND ur.deleted_at IS NULL
                    AND p.key = 'account:manage' AND u.id <> sqlc.arg(excluded_user_id)
            )) OR EXISTS (
                SELECT 1 FROM hosted_account_membership m
                JOIN "user" u ON u.id = m.user_id AND u.deleted_at IS NULL
                JOIN user_organization uo ON uo.user_id = u.id AND uo.organization_id = m.organization_id AND uo.deleted_at IS NULL
                WHERE m.organization_id = a.organization_id AND m.account_id = g.ancestor_id
                    AND m.role = 'account_administrator' AND m.revoked_at IS NULL AND u.id <> sqlc.arg(excluded_user_id)
            )
        )
);
