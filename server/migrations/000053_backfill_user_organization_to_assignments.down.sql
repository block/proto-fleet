-- Reverse the backfill: drop every assignment row that mirrors a
-- legacy user_organization row. user_organization.role_id is
-- untouched in the up migration so no repopulate step is needed.
--
-- Soft-delete is intentional: rolling back the migration shouldn't
-- silently revoke any access an admin granted via the new RPCs
-- post-backfill. Operators who need a true wipe can DELETE the rows
-- by hand after rollback.

UPDATE user_organization_role uor
SET deleted_at = CURRENT_TIMESTAMP
FROM user_organization uo
WHERE uor.user_id = uo.user_id
  AND uor.organization_id = uo.organization_id
  AND uor.role_id = uo.role_id
  AND uor.scope_type = 'org'
  AND uor.scope_id IS NULL
  AND uor.deleted_at IS NULL
  AND uo.deleted_at IS NULL;
