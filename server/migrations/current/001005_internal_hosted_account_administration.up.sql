-- requires-shared: 1000
-- Customer identity affiliation grants no organization role or permissions.
ALTER TABLE user_organization ALTER COLUMN role_id DROP NOT NULL;
ALTER TABLE hosted_account ADD COLUMN sites_restricted BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE hosted_account ADD COLUMN site_ceiling_restricted BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE hosted_account ADD COLUMN authority_revision BIGINT NOT NULL DEFAULT 1 CHECK (authority_revision > 0);

CREATE TABLE hosted_account_site (
    organization_id BIGINT NOT NULL,
    account_id BIGINT NOT NULL,
    site_id BIGINT NOT NULL,
    parent_controlled BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY (organization_id, account_id, parent_controlled, site_id),
    FOREIGN KEY (organization_id, account_id) REFERENCES hosted_account(organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (site_id, organization_id) REFERENCES site(id, org_id) ON DELETE CASCADE
);

CREATE TABLE hosted_account_ancestor_grant (
    organization_id BIGINT NOT NULL,
    account_id BIGINT NOT NULL,
    ancestor_id BIGINT NOT NULL,
    capabilities BIGINT NOT NULL CHECK (capabilities BETWEEN 1 AND 31),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (organization_id, account_id, ancestor_id),
    FOREIGN KEY (organization_id, account_id) REFERENCES hosted_account(organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, ancestor_id) REFERENCES hosted_account(organization_id, id) ON DELETE CASCADE,
    CHECK (account_id <> ancestor_id)
);

CREATE FUNCTION check_hosted_account_ancestor_grant() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM hosted_account target
        JOIN hosted_account ancestor ON ancestor.organization_id = target.organization_id
            AND ancestor.id = NEW.ancestor_id
        WHERE target.organization_id = NEW.organization_id AND target.id = NEW.account_id
            AND (ancestor.depth = 0 OR target.parent_id = ancestor.id)
            AND ancestor.depth < target.depth
    ) THEN
        RAISE EXCEPTION 'grant requires a strict same-organization ancestor' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER hosted_account_ancestor_grant_hierarchy BEFORE INSERT OR UPDATE
    ON hosted_account_ancestor_grant FOR EACH ROW EXECUTE FUNCTION check_hosted_account_ancestor_grant();

CREATE TABLE hosted_account_invitation (
    invitation_id VARCHAR(36) PRIMARY KEY,
    organization_id BIGINT NOT NULL,
    account_id BIGINT NOT NULL,
    inviter_id BIGINT NOT NULL,
    authority_fingerprint VARCHAR(64) NOT NULL,
    token_hash VARCHAR(64) NOT NULL UNIQUE,
    role VARCHAR(32) NOT NULL CHECK (role IN ('viewer', 'pool_operator', 'account_administrator')),
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    accepted_at TIMESTAMPTZ,
    accepted_user_id BIGINT REFERENCES "user"(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (organization_id, account_id) REFERENCES hosted_account(organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (inviter_id, organization_id) REFERENCES user_organization(user_id, organization_id) ON DELETE CASCADE,
    CHECK (expires_at > created_at),
    CHECK ((accepted_at IS NULL) = (accepted_user_id IS NULL))
);
CREATE INDEX hosted_account_invitation_account ON hosted_account_invitation(organization_id, account_id, created_at);
