-- Reverses 000060_backfill_admin_user_manage.up.sql by removing the
-- two specific keys from ADMIN roles. This will also remove the keys
-- from orgs where they were already present pre-backfill; rolling back
-- the data migration cleanly is impossible without provenance
-- tracking, and the rollback path is rare/dev-only.
DELETE FROM role_permission
WHERE permission_id IN (
  SELECT id FROM permission WHERE key IN ('user:read', 'user:manage')
)
AND role_id IN (
  SELECT id FROM role WHERE builtin_key = 'ADMIN' AND deleted_at IS NULL
);
