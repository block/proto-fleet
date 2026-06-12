-- Seed the notification:read / notification:manage permission rows and
-- backfill them onto every existing ADMIN role. The catalog reconciler
-- upserts new permission rows on startup but does NOT re-assert seed
-- permissions onto already-seeded ADMIN/FIELD_TECH roles (additive
-- mode, see reconcile.go). Without this migration, deployments
-- upgraded from any release prior to this one would never grant ADMIN
-- the new keys, so the notification endpoints would silently deny.
--
-- SUPER_ADMIN is reconciled in full mode at boot and converges on its
-- own. FIELD_TECH does not receive notification permissions by design
-- — operators opt in via the role editor.

INSERT INTO permission (key, description) VALUES
    ('notification:read', 'View notification channels, alert rules, silences, and delivery history.'),
    ('notification:manage', 'Create, edit, test, and delete notification channels; pause and resume alert rules; create and lift silences.')
ON CONFLICT (key) DO UPDATE SET description = EXCLUDED.description;

-- Scoped to roles with builtin_key='ADMIN' so operator-created custom
-- roles aren't touched. ON CONFLICT makes this safe to replay against
-- orgs that already hold the keys.
INSERT INTO role_permission (role_id, permission_id)
SELECT r.id, p.id
FROM role r, permission p
WHERE r.builtin_key = 'ADMIN'
  AND r.deleted_at IS NULL
  AND p.key IN ('notification:read', 'notification:manage')
ON CONFLICT (role_id, permission_id) DO NOTHING;
