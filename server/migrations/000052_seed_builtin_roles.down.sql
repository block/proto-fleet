-- Reverse the seed. role_permission.permission_id is ON DELETE
-- RESTRICT (000051), so every role_permission row must be cleared
-- before any permission rows are deleted. Truncating the join is
-- safe — the next-down migration drops the table outright.

DELETE FROM role_permission;

-- Re-point user_organization rows back to the legacy global rows so
-- the down migration leaves the auth path in the pre-000052 state.
-- The legacy rows are still present (we soft-deleted them, didn't
-- DELETE), and a partial unique index keyed on is_builtin=FALSE means
-- they share name uniqueness only with other customs, not built-ins.
UPDATE role
SET deleted_at = NULL
WHERE organization_id IS NULL
  AND is_builtin = FALSE
  AND name IN ('SUPER_ADMIN', 'ADMIN');

UPDATE user_organization uo
SET role_id = legacy.id
FROM role per_org
JOIN role legacy
    ON legacy.name = per_org.builtin_key
    AND legacy.organization_id IS NULL
    AND legacy.is_builtin = FALSE
WHERE uo.role_id = per_org.id
  AND per_org.organization_id IS NOT NULL
  AND per_org.is_builtin = TRUE;

-- Drop the per-org built-in rows.
DELETE FROM role
WHERE is_builtin = TRUE
  AND organization_id IS NOT NULL;

DELETE FROM permission;
