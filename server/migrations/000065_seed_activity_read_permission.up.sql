-- Seed the activity:read permission row and backfill it onto every
-- existing ADMIN role. The catalog reconciler upserts new permission
-- rows on startup but does NOT re-assert seed permissions onto
-- already-seeded ADMIN/FIELD_TECH roles (additive mode, see
-- reconcile.go). Without this migration, deployments upgraded from any
-- release prior to this one would never grant ADMIN activity:read, so
-- the newly-gated ActivityService endpoints would silently deny.
--
-- SUPER_ADMIN is reconciled in full mode at boot and converges on its
-- own. FIELD_TECH does not receive activity:read by design — operators
-- opt in via the role editor.

INSERT INTO permission (key, description) VALUES
    ('activity:read', 'View the organization-wide activity log and export it as CSV.')
ON CONFLICT (key) DO UPDATE SET description = EXCLUDED.description;

-- Scoped to roles with builtin_key='ADMIN' so operator-created custom
-- roles aren't touched. ON CONFLICT makes this safe to replay against
-- orgs that already hold the key.
INSERT INTO role_permission (role_id, permission_id)
SELECT r.id, p.id
FROM role r, permission p
WHERE r.builtin_key = 'ADMIN'
  AND r.deleted_at IS NULL
  AND p.key = 'activity:read'
ON CONFLICT (role_id, permission_id) DO NOTHING;
