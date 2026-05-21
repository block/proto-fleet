-- Seed the permission catalog and per-org built-in roles, then repoint
-- existing user_organization rows from the legacy global ADMIN/
-- SUPER_ADMIN rows onto each user's per-org replacement.
--
-- Order matters:
--   1. Insert catalog (permission rows).
--   2. Insert one (org × builtin) role row per (active org, builtin
--      key) combination.
--   3. Repoint user_organization.role_id to the per-org row matching
--      the user's organization.
--   4. Soft-delete the legacy global ADMIN/SUPER_ADMIN role rows.
--   5. Populate role_permission for each per-org built-in row.
--
-- The Go startup reconciler in
-- server/internal/domain/authz/reconcile.go takes over after this
-- migration and keeps per-org built-ins converged on every boot.

-- ---------------------------------------------------------------
-- 1. Permission catalog.
-- ---------------------------------------------------------------
INSERT INTO permission (key, description) VALUES
    ('fleet:read',                'View dashboard, miner list, and telemetry. Required floor for any role with miner actions.'),
    ('miner:read',                'View miner detail, status snapshot, and error history. Required floor for any miner action permission.'),
    ('miner:blink_led',           'Trigger the locator LED on a miner.'),
    ('miner:reboot',              'Reboot a miner.'),
    ('miner:start_mining',        'Start mining on a miner.'),
    ('miner:stop_mining',         'Stop mining on a miner.'),
    ('miner:update_pools',        'Update a miner''s pool configuration.'),
    ('miner:update_worker_names', 'Update worker names on a miner.'),
    ('miner:rename',              'Rename a miner.'),
    ('miner:delete',              'Delete a miner.'),
    ('miner:set_cooling_mode',    'Change a miner''s cooling mode.'),
    ('miner:set_power_target',    'Change a miner''s power target.'),
    ('miner:firmware_update',     'Push a firmware update to a miner.'),
    ('miner:download_logs',       'Download diagnostic logs from a miner.'),
    ('miner:update_password',     'Change the miner''s device-local web UI password.'),
    ('miner:unpair',              'Unpair a miner from the fleet.'),
    ('miner:pair',                'Pair a new miner into the fleet.'),
    ('miner:export_csv',          'Export miner data as CSV.'),
    ('rack:read',                 'List racks at a site.'),
    ('rack:manage',               'Create, rename, delete racks and move miners between them.'),
    ('site:read',                 'View sites and buildings.'),
    ('site:manage',               'Create, edit, and delete sites and buildings.'),
    ('serverlog:read',            'View server-side logs.'),
    ('curtailment:read',          'View curtailment policies and preview impact.'),
    ('curtailment:manage',        'Create, edit, and delete curtailment policies.'),
    ('fleetnode:read',            'View fleet-node state.'),
    ('fleetnode:manage',          'Perform fleet-node admin operations.'),
    ('apikey:manage',             'List, create, and revoke API keys for the organization.'),
    ('user:read',                 'List users in the organization.'),
    ('user:manage',               'Create, reset, and deactivate users in the organization.'),
    ('role:manage',               'Create, edit, and delete custom roles and edit the ADMIN/FIELD_TECH built-ins.')
ON CONFLICT (key) DO UPDATE SET description = EXCLUDED.description;

-- ---------------------------------------------------------------
-- 2. Per-org built-in role rows. Cross-join EVERY organization (not
--    just deleted_at IS NULL) with the fixed list of built-in specs.
--    Soft-deleted orgs can still have active user_organization rows
--    (the soft delete doesn't cascade to membership), so they need
--    per-org built-ins too — otherwise step 3 cannot repoint those
--    user_organization rows to a per-org role and 000053 would
--    backfill them with a role_id pointing at the legacy global row,
--    failing the composite FK on user_organization_role.
-- ---------------------------------------------------------------
INSERT INTO role (name, description, is_builtin, builtin_key, organization_id)
SELECT b.name, b.description, TRUE, b.builtin_key, org.id
FROM organization org
CROSS JOIN (VALUES
    ('SUPER_ADMIN', 'Full system access. Immutable.', 'SUPER_ADMIN'),
    ('ADMIN',       'Org admin. Editable by a SUPER_ADMIN.', 'ADMIN'),
    ('FIELD_TECH',  'Field tech. Read fleet data, blink the locator LED, download logs, manage racks. Editable by a SUPER_ADMIN.', 'FIELD_TECH')
) AS b(name, description, builtin_key)
ON CONFLICT DO NOTHING;

-- ---------------------------------------------------------------
-- 3. Repoint user_organization rows from the legacy global ADMIN /
--    SUPER_ADMIN rows to each user's per-org equivalent. The legacy
--    rows have organization_id NULL; the new rows have it set. Match
--    by name (the legacy uniqueness key from 000002).
-- ---------------------------------------------------------------
UPDATE user_organization uo
SET role_id = new_role.id
FROM role legacy
JOIN role new_role
    ON new_role.builtin_key = legacy.name
    AND new_role.is_builtin = TRUE
    AND new_role.deleted_at IS NULL
WHERE uo.role_id = legacy.id
  AND legacy.organization_id IS NULL
  AND legacy.is_builtin = FALSE
  AND new_role.organization_id = uo.organization_id;

-- ---------------------------------------------------------------
-- 4. Soft-delete the legacy global rows. Anything that still
--    references them after step 3 was a soft-deleted user_organization
--    row that the resolver will never load.
-- ---------------------------------------------------------------
UPDATE role
SET deleted_at = CURRENT_TIMESTAMP
WHERE organization_id IS NULL
  AND is_builtin = FALSE
  AND name IN ('SUPER_ADMIN', 'ADMIN');

-- ---------------------------------------------------------------
-- 5. Seed role_permission rows per (org × built-in × seed perms).
--    SUPER_ADMIN gets every catalog key in each org.
-- ---------------------------------------------------------------
INSERT INTO role_permission (role_id, permission_id)
SELECT r.id, p.id
FROM role r
CROSS JOIN permission p
WHERE r.builtin_key = 'SUPER_ADMIN'
  AND r.is_builtin = TRUE
  AND r.deleted_at IS NULL
ON CONFLICT DO NOTHING;

-- ADMIN: every catalog key except user:read, user:manage, role:manage.
INSERT INTO role_permission (role_id, permission_id)
SELECT r.id, p.id
FROM role r
CROSS JOIN permission p
WHERE r.builtin_key = 'ADMIN'
  AND r.is_builtin = TRUE
  AND r.deleted_at IS NULL
  AND p.key NOT IN ('user:read', 'user:manage', 'role:manage')
ON CONFLICT DO NOTHING;

-- FIELD_TECH: explicit minimal set.
INSERT INTO role_permission (role_id, permission_id)
SELECT r.id, p.id
FROM role r
CROSS JOIN permission p
WHERE r.builtin_key = 'FIELD_TECH'
  AND r.is_builtin = TRUE
  AND r.deleted_at IS NULL
  AND p.key IN (
      'fleet:read',
      'miner:read',
      'miner:blink_led',
      'miner:download_logs',
      'rack:read',
      'rack:manage'
  )
ON CONFLICT DO NOTHING;
