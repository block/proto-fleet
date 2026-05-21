DROP TRIGGER IF EXISTS update_user_organization_role_updated_at ON user_organization_role;
DROP INDEX IF EXISTS idx_user_organization_role_user_org;
DROP TABLE IF EXISTS user_organization_role;
DROP TABLE IF EXISTS role_permission;
DROP TABLE IF EXISTS permission;

ALTER TABLE role
    DROP CONSTRAINT IF EXISTS uq_role_builtin_key,
    DROP COLUMN IF EXISTS builtin_key,
    DROP COLUMN IF EXISTS is_builtin;
