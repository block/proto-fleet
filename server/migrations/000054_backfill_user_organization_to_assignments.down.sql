-- Reverse the backfill: hard-delete every assignment row that mirrors
-- a legacy user_organization row. user_organization.role_id is
-- untouched in the up migration so no repopulate step is needed.
--
-- Hard delete (not soft) is required because user_organization_role's
-- composite FK to role(id, organization_id) is ON DELETE RESTRICT.
-- Soft-deleted rows would still hold the FK, and 000053's down
-- migration could not then delete the per-org built-in role rows the
-- assignments reference — the rollback would get stuck partway.
-- Operators who need an audit trail of the rolled-back state should
-- snapshot the table before running the down migration.

-- Drop the legacy-paired rows regardless of user_organization's
-- deleted_at state. Any assignment row created by the up migration or
-- by post-deploy dual-writes from the onboarding/user-create paths
-- needs to be cleared; otherwise 000053's down DELETE on per-org
-- built-in roles fails the composite FK. Live customer-created
-- assignments (from PR 3+'s management RPCs) would survive this
-- filter — there are none in this PR's scope.
DELETE FROM user_organization_role uor
USING user_organization uo
WHERE uor.user_id = uo.user_id
  AND uor.organization_id = uo.organization_id
  AND uor.role_id = uo.role_id
  AND uor.scope_type = 'org'
  AND uor.scope_id IS NULL;
