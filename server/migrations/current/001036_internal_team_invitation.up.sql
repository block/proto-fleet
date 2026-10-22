-- requires-shared: 1000
-- Host team invitations grant an organization role on acceptance.
--
-- role.permission_revision counts every write to a role's permission rows, so
-- it only grows, even when an edit restores an earlier or empty permission set.
-- Startup reconciliation inserts with ON CONFLICT DO NOTHING, which writes no
-- row for permissions a role already has and leaves the revision unchanged.
ALTER TABLE role ADD COLUMN permission_revision BIGINT NOT NULL DEFAULT 1
    CHECK (permission_revision > 0);

CREATE FUNCTION bump_role_permission_revision() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP <> 'INSERT' THEN
        UPDATE role SET permission_revision = permission_revision + 1 WHERE id = OLD.role_id;
    END IF;
    IF TG_OP <> 'DELETE' THEN
        UPDATE role SET permission_revision = permission_revision + 1 WHERE id = NEW.role_id;
    END IF;
    RETURN NULL;
END;
$$;

CREATE TRIGGER role_permission_revision AFTER INSERT OR UPDATE OR DELETE ON role_permission
    FOR EACH ROW EXECUTE FUNCTION bump_role_permission_revision();

-- The authority fingerprint witnesses the invited role's permission revision
-- and the inviter's live role assignments with each assigned role's revision.
-- Any change to either invalidates an invitation permanently: role changes
-- replace assignment rows, and revisions never repeat.
CREATE FUNCTION team_invitation_authority_fingerprint(
    target_role_id BIGINT, inviter_user_id BIGINT, target_organization_id BIGINT
) RETURNS text LANGUAGE sql STABLE AS $$
    SELECT encode(sha256(convert_to(
        'role:' || target_role_id::text || ':' || COALESCE((
            SELECT r.permission_revision::text FROM role r WHERE r.id = target_role_id
        ), '')
        || '|inviter:' || COALESCE((
            SELECT string_agg(
                ur.id::text || ':' || ur.role_id::text || ':' || ur.scope_type || ':' || COALESCE(ur.scope_id::text, '') || ':'
                    || ir.permission_revision::text,
                ';' ORDER BY ur.id)
            FROM user_organization_role ur
            JOIN role ir ON ir.id = ur.role_id
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
    ON organization_invitation (organization_id, created_at DESC, invitation_id DESC)
    WHERE accepted_at IS NULL;
