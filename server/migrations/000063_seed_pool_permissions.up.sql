-- Seed the pool:read and pool:manage permission rows and backfill them
-- onto existing ADMIN roles. The catalog reconciler upserts new permission
-- rows on startup, but it does NOT re-assert seed permissions onto
-- already-seeded ADMIN/FIELD_TECH roles (additive mode, see reconcile.go).
-- Without this migration, deployments upgraded from any release prior to
-- this one would never grant ADMIN the new pool keys, so the newly-gated
-- PoolsService endpoints would silently deny.
--
-- SUPER_ADMIN is reconciled in full mode at boot and converges on its
-- own. FIELD_TECH does not receive pool permissions by design (operators
-- opt in via the role editor).

INSERT INTO permission (key, description) VALUES
    ('pool:read',   'View saved mining pool configurations.'),
    ('pool:manage', 'Create, edit, and delete saved mining pool configurations.')
ON CONFLICT (key) DO UPDATE SET description = EXCLUDED.description;

-- Scoped to roles with builtin_key='ADMIN' so operator-created custom
-- roles aren't touched. ON CONFLICT makes this safe to replay against
-- orgs that already hold either key.
INSERT INTO role_permission (role_id, permission_id)
SELECT r.id, p.id
FROM role r, permission p
WHERE r.builtin_key = 'ADMIN'
  AND r.deleted_at IS NULL
  AND p.key IN ('pool:read', 'pool:manage')
ON CONFLICT (role_id, permission_id) DO NOTHING;
