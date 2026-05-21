DROP TRIGGER IF EXISTS update_user_organization_role_updated_at ON user_organization_role;
DROP INDEX IF EXISTS uq_user_org_role_site_scope;
DROP INDEX IF EXISTS uq_user_org_role_org_scope;
DROP INDEX IF EXISTS idx_user_organization_role_user_org;
DROP TABLE IF EXISTS user_organization_role;
DROP TABLE IF EXISTS role_permission;
DROP TABLE IF EXISTS permission;

ALTER TABLE role
    DROP CONSTRAINT IF EXISTS chk_role_builtin_key_matches_flag,
    DROP CONSTRAINT IF EXISTS uq_role_id_org_id;

DROP INDEX IF EXISTS uq_role_org_custom_name;
DROP INDEX IF EXISTS uq_role_org_builtin_key;

ALTER TABLE role
    DROP CONSTRAINT IF EXISTS fk_role_organization,
    DROP COLUMN IF EXISTS organization_id,
    DROP COLUMN IF EXISTS builtin_key,
    DROP COLUMN IF EXISTS is_builtin;

-- Restore the global name uniqueness that 000002 originally shipped.
ALTER TABLE role
    ADD CONSTRAINT uq_role_name UNIQUE (name);
