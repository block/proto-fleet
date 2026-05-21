DROP TRIGGER IF EXISTS update_user_organization_role_updated_at ON user_organization_role;
DROP INDEX IF EXISTS uq_user_org_role_site_scope;
DROP INDEX IF EXISTS uq_user_org_role_org_scope;
DROP INDEX IF EXISTS idx_user_organization_role_user_org;
DROP TABLE IF EXISTS user_organization_role;
DROP TABLE IF EXISTS role_permission;
DROP TABLE IF EXISTS permission;

ALTER TABLE role
    DROP CONSTRAINT IF EXISTS chk_role_builtin_key_matches_flag,
    DROP CONSTRAINT IF EXISTS chk_role_custom_name_not_reserved,
    DROP CONSTRAINT IF EXISTS uq_role_id_org_id;

DROP INDEX IF EXISTS uq_role_org_custom_name;
DROP INDEX IF EXISTS uq_role_org_builtin_key;

-- Custom roles are a per-org concept introduced in this migration; they
-- have no representation in the pre-up schema. The new partial unique
-- index uq_role_org_custom_name allowed multiple orgs to share a custom
-- name, so restoring the global uq_role_name below would fail with a
-- duplicate-key error on any environment where customs exist. Hard
-- delete every custom row before the constraint is restored. (PR 1
-- ships no RPC to create customs, so this is a no-op in production
-- today; it makes the down safe once custom-role CRUD lands.)
DELETE FROM role_permission
WHERE role_id IN (SELECT id FROM role WHERE is_builtin = FALSE);

DELETE FROM role WHERE is_builtin = FALSE;

ALTER TABLE role
    DROP CONSTRAINT IF EXISTS fk_role_organization,
    DROP COLUMN IF EXISTS organization_id,
    DROP COLUMN IF EXISTS builtin_key,
    DROP COLUMN IF EXISTS is_builtin;

-- Restore the global name uniqueness that 000002 originally shipped.
ALTER TABLE role
    ADD CONSTRAINT uq_role_name UNIQUE (name);
