-- requires-shared: 1000
-- Host team invitations grant an organization role on acceptance.
--
-- The authority fingerprint witnesses the invited role's permissions and the
-- inviter's live role assignments, including each assigned role's permissions.
-- Any change to either invalidates an invitation permanently: role changes
-- replace assignment rows, and role_permission rows are only ever inserted or
-- deleted, so a removed and restored permission returns with a new xmin.
-- Startup reconciliation updates built-in role rows but leaves existing
-- role_permission rows untouched, so restarts do not invalidate invitations.
CREATE FUNCTION team_invitation_authority_fingerprint(
    target_role_id BIGINT, inviter_user_id BIGINT, target_organization_id BIGINT
) RETURNS text LANGUAGE sql STABLE AS $$
    SELECT encode(sha256(convert_to(
        'role:' || target_role_id::text || ':' || COALESCE((
            SELECT string_agg(rp.permission_id::text || '@' || rp.xmin::text, ',' ORDER BY rp.permission_id)
            FROM role_permission rp
            WHERE rp.role_id = target_role_id
        ), '')
        || '|inviter:' || COALESCE((
            SELECT string_agg(
                ur.id::text || ':' || ur.role_id::text || ':' || ur.scope_type || ':' || COALESCE(ur.scope_id::text, '') || ':'
                    || COALESCE((
                        SELECT string_agg(irp.permission_id::text || '@' || irp.xmin::text, ',' ORDER BY irp.permission_id)
                        FROM role_permission irp
                        WHERE irp.role_id = ur.role_id
                    ), ''),
                ';' ORDER BY ur.id)
            FROM user_organization_role ur
            WHERE ur.user_id = inviter_user_id AND ur.organization_id = target_organization_id
                AND ur.deleted_at IS NULL
        ), ''),
        'UTF8')), 'hex')
$$;

CREATE TABLE organization_invitation (
    invitation_id VARCHAR(36) PRIMARY KEY,
    organization_id BIGINT NOT NULL,
    inviter_id BIGINT NOT NULL,
    role_id BIGINT NOT NULL,
    authority_fingerprint VARCHAR(64) NOT NULL,
    token_hash VARCHAR(64) NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    accepted_at TIMESTAMPTZ,
    accepted_user_id BIGINT REFERENCES "user"(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_organization_invitation_token_hash UNIQUE (token_hash),
    CONSTRAINT fk_organization_invitation_inviter FOREIGN KEY (inviter_id, organization_id)
        REFERENCES user_organization(user_id, organization_id) ON DELETE CASCADE,
    CONSTRAINT fk_organization_invitation_role FOREIGN KEY (role_id, organization_id)
        REFERENCES role(id, organization_id) ON DELETE CASCADE,
    CHECK (expires_at > created_at),
    CHECK ((accepted_at IS NULL) = (accepted_user_id IS NULL))
);

CREATE INDEX idx_organization_invitation_org_created
    ON organization_invitation (organization_id, created_at DESC, invitation_id DESC);
