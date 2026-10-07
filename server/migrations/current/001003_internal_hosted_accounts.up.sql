-- requires-shared: 1000
-- Account membership is separate from organization RBAC. This migration does
-- not assign organization roles or customer memberships to existing users.
INSERT INTO permission (key, description) VALUES
    ('account:manage', 'Create customer accounts and control delegation beneath the host account. Does not grant private customer views or pool overrides.')
ON CONFLICT (key) DO UPDATE SET description = EXCLUDED.description;

-- ADMIN is seeded once; catalog growth only reaches newly created roles.
-- Match the new-role seed without changing custom or deleted roles.
INSERT INTO role_permission (role_id, permission_id)
SELECT r.id, p.id
FROM role r CROSS JOIN permission p
WHERE r.builtin_key = 'ADMIN'
  AND r.deleted_at IS NULL
  AND p.key = 'account:manage'
ON CONFLICT (role_id, permission_id) DO NOTHING;

CREATE TABLE hosted_account (
    id BIGSERIAL PRIMARY KEY,
    account_id VARCHAR(36) NOT NULL UNIQUE,
    organization_id BIGINT NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
    parent_id BIGINT,
    depth INTEGER NOT NULL CHECK (depth BETWEEN 0 AND 2),
    name VARCHAR(255) NOT NULL CHECK (length(btrim(name)) > 0),
    capabilities BIGINT NOT NULL CHECK (capabilities BETWEEN 1 AND 31),
    suspended BOOLEAN NOT NULL DEFAULT FALSE,
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (organization_id, id),
    FOREIGN KEY (organization_id, parent_id)
        REFERENCES hosted_account(organization_id, id) ON DELETE CASCADE,
    CHECK ((depth = 0 AND parent_id IS NULL) OR (depth > 0 AND parent_id IS NOT NULL))
);

CREATE UNIQUE INDEX hosted_account_one_root ON hosted_account(organization_id) WHERE depth = 0;
CREATE INDEX hosted_account_parent ON hosted_account(organization_id, parent_id);

CREATE FUNCTION check_hosted_account_hierarchy() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND
        (NEW.organization_id, NEW.parent_id, NEW.depth) IS DISTINCT FROM
        (OLD.organization_id, OLD.parent_id, OLD.depth) THEN
        RAISE EXCEPTION 'account organization and parent cannot change' USING ERRCODE = '23514';
    END IF;
    IF NEW.depth > 0 AND NOT EXISTS (
        SELECT 1 FROM hosted_account parent
        WHERE parent.organization_id = NEW.organization_id
          AND parent.id = NEW.parent_id AND parent.depth = NEW.depth - 1
    ) THEN
        RAISE EXCEPTION 'invalid account hierarchy' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER hosted_account_hierarchy
    BEFORE INSERT OR UPDATE ON hosted_account
    FOR EACH ROW EXECUTE FUNCTION check_hosted_account_hierarchy();

CREATE TABLE hosted_account_membership (
    organization_id BIGINT NOT NULL,
    account_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    role VARCHAR(32) NOT NULL CHECK (role IN ('viewer', 'pool_operator', 'account_administrator')),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (organization_id, account_id, user_id),
    FOREIGN KEY (organization_id, account_id)
        REFERENCES hosted_account(organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (user_id, organization_id)
        REFERENCES user_organization(user_id, organization_id) ON DELETE CASCADE
);

CREATE INDEX hosted_account_membership_user
    ON hosted_account_membership(organization_id, user_id) WHERE revoked_at IS NULL;

INSERT INTO hosted_account (account_id, organization_id, depth, name, capabilities)
SELECT gen_random_uuid()::text, id, 0,
    COALESCE(NULLIF(btrim(name), ''), 'Host ' || id::text), 31 FROM organization;

-- Organizations created after the migration also receive exactly one root.
CREATE FUNCTION create_hosted_account_root() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO hosted_account (account_id, organization_id, depth, name, capabilities)
    VALUES (gen_random_uuid()::text, NEW.id, 0,
        COALESCE(NULLIF(btrim(NEW.name), ''), 'Host ' || NEW.id::text), 31);
    RETURN NEW;
END;
$$;

CREATE TRIGGER organization_hosted_account_root
    AFTER INSERT ON organization
    FOR EACH ROW EXECUTE FUNCTION create_hosted_account_root();
