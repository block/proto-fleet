-- Tighten user_organization_role uniqueness so each (user, org) pair has
-- at most one live org-scope assignment. The existing partial unique
-- index uq_user_org_role_org_scope covers (user, org, role_id), which
-- accepts multiple live org-scope rows for the same user as long as
-- their role_ids differ — the application has always assumed a single
-- live org-scope row per user (CreateUserOrganizationRole and
-- UpdateUserRole both maintain that invariant), and an off-path write
-- that violated it would silently leave UpdateUserRole swapping only
-- one assignment while other live grants remained.
--
-- Constraint shape mirrors the existing partial index pattern: WHERE
-- scope_type='org' AND deleted_at IS NULL so soft-deleted rows and
-- site-scope assignments are excluded.
--
-- Uses CONCURRENTLY so the build does not take ACCESS EXCLUSIVE on
-- user_organization_role during deploy (matches 000055 / 000056).
-- golang-migrate v4's postgres driver runs each statement directly via
-- conn.ExecContext, no implicit transaction wraps the body, so
-- CREATE UNIQUE INDEX CONCURRENTLY is safe here. If a partial build
-- fails it leaves schema_migrations.dirty=true at version 74 and may
-- leave an INVALID index in pg_indexes; recovery is to DROP the
-- INVALID index and `migrate force 73` before re-deploying.
CREATE UNIQUE INDEX CONCURRENTLY uq_user_org_single_live_org_scope
    ON user_organization_role(user_id, organization_id)
    WHERE scope_type = 'org' AND deleted_at IS NULL;
