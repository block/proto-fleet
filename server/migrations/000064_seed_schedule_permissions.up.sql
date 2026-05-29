-- Seed the schedule:read and schedule:manage permission rows and
-- backfill them onto existing ADMIN roles. The catalog reconciler
-- upserts new permission rows on startup, but it does NOT re-assert
-- seed permissions onto already-seeded ADMIN/FIELD_TECH roles
-- (additive mode, see reconcile.go). Without this migration,
-- deployments upgraded from any release prior to this one would never
-- grant ADMIN the new schedule keys, so the newly-gated
-- ScheduleService endpoints would silently deny.
--
-- SUPER_ADMIN is reconciled in full mode at boot and converges on its
-- own. FIELD_TECH does not receive schedule permissions by design
-- (operators opt in via the role editor).

INSERT INTO permission (key, description) VALUES
    ('schedule:read',   'View scheduled miner actions.'),
    ('schedule:manage', 'Create, edit, pause, resume, and delete scheduled miner actions. Requires the underlying miner action permission to schedule that action.')
ON CONFLICT (key) DO UPDATE SET description = EXCLUDED.description;

-- Scoped to roles with builtin_key='ADMIN' so operator-created custom
-- roles aren't touched. ON CONFLICT makes this safe to replay against
-- orgs that already hold either key.
INSERT INTO role_permission (role_id, permission_id)
SELECT r.id, p.id
FROM role r, permission p
WHERE r.builtin_key = 'ADMIN'
  AND r.deleted_at IS NULL
  AND p.key IN ('schedule:read', 'schedule:manage')
ON CONFLICT (role_id, permission_id) DO NOTHING;
