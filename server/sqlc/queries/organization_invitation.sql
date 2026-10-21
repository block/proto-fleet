-- name: CreateOrganizationInvitation :one
INSERT INTO organization_invitation
    (invitation_id, organization_id, inviter_id, role_id, authority_fingerprint, token_hash, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (token_hash) DO NOTHING RETURNING *;

-- name: InvitationTokenHashInUse :one
-- Team and customer account invitations share one code namespace, so public
-- redemption resolves each code to exactly one invitation.
SELECT (EXISTS (SELECT 1 FROM hosted_account_invitation h WHERE h.token_hash = sqlc.arg('token_hash'))
    OR EXISTS (SELECT 1 FROM organization_invitation o WHERE o.token_hash = sqlc.arg('token_hash')))::boolean AS in_use;

-- name: OrganizationInvitationExists :one
SELECT EXISTS (SELECT 1 FROM organization_invitation WHERE token_hash = $1)::boolean AS found;

-- name: GetOrganizationInvitationByHash :one
SELECT i.*, o.name AS organization_name, r.name AS role_name
FROM organization_invitation i
JOIN organization o ON o.id = i.organization_id
JOIN role r ON r.id = i.role_id
WHERE i.token_hash = $1;

-- name: GetTeamInvitationAuthorityFingerprint :one
SELECT team_invitation_authority_fingerprint(
    sqlc.arg('role_id'), sqlc.arg('inviter_id'), sqlc.arg('organization_id')
)::text AS fingerprint;

-- name: ConsumeOrganizationInvitation :execrows
UPDATE organization_invitation
SET accepted_at = CURRENT_TIMESTAMP, accepted_user_id = sqlc.arg('accepted_user_id')
WHERE invitation_id = sqlc.arg('invitation_id')
    AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > statement_timestamp();

-- name: RevokeOrganizationInvitation :execrows
UPDATE organization_invitation
SET revoked_at = CURRENT_TIMESTAMP
WHERE organization_id = sqlc.arg('organization_id') AND invitation_id = sqlc.arg('invitation_id')
    AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > statement_timestamp();

-- name: DeleteOrganizationInvitation :execrows
-- Only dead invitations can be removed; accepted rows stay as the durable link
-- to the joining login.
DELETE FROM organization_invitation
WHERE organization_id = sqlc.arg('organization_id') AND invitation_id = sqlc.arg('invitation_id')
    AND accepted_at IS NULL AND (revoked_at IS NOT NULL OR expires_at <= statement_timestamp());

-- name: ListOrganizationInvitations :many
-- Accepted invitations are excluded before paging; revoked and expired ones
-- remain until deleted. Availability reflects lifecycle, the live inviter and
-- unchanged authority; the service then applies the inviter's current hierarchy.
SELECT i.invitation_id, i.inviter_id, i.role_id, r.name AS role_name, i.expires_at, i.revoked_at, i.created_at,
    inviter.username AS inviter_username,
    (i.revoked_at IS NULL AND i.expires_at > statement_timestamp()
        AND r.deleted_at IS NULL AND inviter.deleted_at IS NULL AND uo.deleted_at IS NULL
        AND i.authority_fingerprint = team_invitation_authority_fingerprint(i.role_id, i.inviter_id, i.organization_id)
    )::boolean AS available
FROM organization_invitation i
JOIN role r ON r.id = i.role_id
JOIN "user" inviter ON inviter.id = i.inviter_id
JOIN user_organization uo ON uo.user_id = i.inviter_id AND uo.organization_id = i.organization_id
WHERE i.organization_id = sqlc.arg('organization_id')
    AND i.accepted_at IS NULL
    AND (sqlc.narg('cursor_time')::timestamptz IS NULL
        OR (i.created_at, i.invitation_id) < (sqlc.narg('cursor_time')::timestamptz, sqlc.arg('cursor_id')::varchar))
ORDER BY i.created_at DESC, i.invitation_id DESC
LIMIT sqlc.arg('page_limit')::int;

-- name: GetUserOrganizationRoleState :one
-- A user already holds team access when either the legacy role column or any
-- live role assignment exists. Customer-only logins have neither.
SELECT (uo.role_id IS NOT NULL OR EXISTS (
    SELECT 1 FROM user_organization_role uor
    WHERE uor.user_id = uo.user_id AND uor.organization_id = uo.organization_id AND uor.deleted_at IS NULL
))::boolean AS has_team_role
FROM user_organization uo
WHERE uo.user_id = $1 AND uo.organization_id = $2 AND uo.deleted_at IS NULL;
