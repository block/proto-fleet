-- Backfill missing user:read and user:manage on existing ADMIN roles.
-- The ADMIN seed formula now includes both keys (only role:manage is
-- excluded). Without this backfill, deployments seeded by migration
-- 000053 (which excluded user:read and user:manage from ADMIN) would
-- keep those exclusions forever — the additive reconciler does not
-- re-assert seed permissions onto existing role rows, so newly-gated
-- user-management endpoints would silently deny ADMIN on upgraded orgs.
--
-- Scoped to roles with builtin_key='ADMIN' so operator-created custom
-- roles aren't touched. The ON CONFLICT clause makes this safe to
-- replay against orgs that already hold either key.
INSERT INTO role_permission (role_id, permission_id)
SELECT r.id, p.id
FROM role r, permission p
WHERE r.builtin_key = 'ADMIN'
  AND r.deleted_at IS NULL
  AND p.key IN ('user:read', 'user:manage')
ON CONFLICT (role_id, permission_id) DO NOTHING;
