-- Step 1: custom-role cleanup. Must run BEFORE the table drops below
-- because it references role_permission, and BEFORE the role columns
-- are dropped because the WHERE clauses filter on is_builtin and
-- organization_id.
--
-- Restricted to org-scoped customs (organization_id IS NOT NULL). The
-- legacy global rows (org_id IS NULL) are the ADMIN/SUPER_ADMIN rows
-- 000052 just restored on its own down pass; we must NOT delete them
-- here or user_organization.role_id would dangle. After this delete,
-- only legacy globals (and the built-in rows 000052 already removed)
-- remain — their names are unique, so restoring uq_role_name at the
-- bottom of this file succeeds.
--
-- PR 1 ships no RPC to create org-scoped customs, so in production
-- today this is a no-op. The cleanup is forward-safe for once
-- CreateCustomRole / UpdateCustomRole ship.
DELETE FROM role_permission
WHERE role_id IN (
    SELECT id FROM role
    WHERE is_builtin = FALSE
      AND organization_id IS NOT NULL
);

DELETE FROM role
WHERE is_builtin = FALSE
  AND organization_id IS NOT NULL;

-- Step 2: drop the tables and indexes added by 000051.
DROP TRIGGER IF EXISTS update_user_organization_role_updated_at ON user_organization_role;
DROP INDEX IF EXISTS uq_user_org_role_site_scope;
DROP INDEX IF EXISTS uq_user_org_role_org_scope;
DROP INDEX IF EXISTS idx_user_organization_role_user_org;
DROP TABLE IF EXISTS user_organization_role;
DROP TABLE IF EXISTS role_permission;
DROP TABLE IF EXISTS permission;

-- Step 3: drop the role-table additions.
DROP INDEX IF EXISTS uq_role_org_custom_name;
DROP INDEX IF EXISTS uq_role_org_builtin_key;

ALTER TABLE role
    DROP CONSTRAINT IF EXISTS chk_role_builtin_key_matches_flag,
    DROP CONSTRAINT IF EXISTS chk_role_custom_name_not_reserved,
    DROP CONSTRAINT IF EXISTS uq_role_id_org_id,
    DROP CONSTRAINT IF EXISTS fk_role_organization,
    DROP COLUMN IF EXISTS organization_id,
    DROP COLUMN IF EXISTS builtin_key,
    DROP COLUMN IF EXISTS is_builtin;

-- Step 4: restore the global name uniqueness that 000002 originally
-- shipped. Safe because only legacy global rows remain post-cleanup
-- and their names are distinct.
ALTER TABLE role
    ADD CONSTRAINT uq_role_name UNIQUE (name);
