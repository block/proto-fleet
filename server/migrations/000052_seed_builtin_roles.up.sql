-- One-shot seed for the RBAC v2 catalog. This file captures the v1
-- catalog as of the migration date so a fresh install has a usable
-- state before the server starts and runs its reconciliation step.
--
-- The Go startup reconciler in server/internal/domain/authz/reconcile.go
-- is the authority going forward; this migration only ensures the rows
-- exist on first run. UPSERTs and ON CONFLICT clauses make it safe to
-- re-run against an environment whose reconciler has already converged.
--
-- Note: the existing ADMIN role row from migration 000002 is preserved;
-- it gets marked is_builtin=TRUE here. SUPER_ADMIN is currently created
-- by onboarding on first user signup; if that has already run, the row
-- is marked here too. FIELD_TECH is new in this migration.

-- ---------------------------------------------------------------
-- Permission catalog rows. Keep this list in sync with the catalog
-- declared in server/internal/domain/authz/catalog.go.
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
-- Built-in role rows. ON CONFLICT (name) catches the pre-existing
-- ADMIN row from migration 000002 and any SUPER_ADMIN row already
-- created by onboarding.
-- ---------------------------------------------------------------
INSERT INTO role (name, description, is_builtin, builtin_key)
VALUES
    ('SUPER_ADMIN', 'Full system access. Cannot be modified.', TRUE, 'SUPER_ADMIN'),
    ('ADMIN',       'Org admin. Editable by a SUPER_ADMIN.',   TRUE, 'ADMIN'),
    ('FIELD_TECH',  'Field tech. Read fleet data, blink the locator LED, download logs, manage racks. Editable by a SUPER_ADMIN.', TRUE, 'FIELD_TECH')
ON CONFLICT (name) DO UPDATE SET
    is_builtin = TRUE,
    builtin_key = EXCLUDED.builtin_key,
    description = EXCLUDED.description;

-- ---------------------------------------------------------------
-- Seed role_permission rows. The Go reconciler enforces these going
-- forward; this migration only handles the first-boot case so an
-- environment that boots fast and serves a request before the
-- reconciler completes still gets the right answer.
--
-- SUPER_ADMIN: every catalog key.
-- ---------------------------------------------------------------
INSERT INTO role_permission (role_id, permission_id)
SELECT r.id, p.id
FROM role r
CROSS JOIN permission p
WHERE r.builtin_key = 'SUPER_ADMIN'
ON CONFLICT DO NOTHING;

-- ADMIN: every catalog key except user:read, user:manage, role:manage.
INSERT INTO role_permission (role_id, permission_id)
SELECT r.id, p.id
FROM role r
CROSS JOIN permission p
WHERE r.builtin_key = 'ADMIN'
  AND p.key NOT IN ('user:read', 'user:manage', 'role:manage')
ON CONFLICT DO NOTHING;

-- FIELD_TECH: explicit minimal set.
INSERT INTO role_permission (role_id, permission_id)
SELECT r.id, p.id
FROM role r
CROSS JOIN permission p
WHERE r.builtin_key = 'FIELD_TECH'
  AND p.key IN (
      'fleet:read',
      'miner:read',
      'miner:blink_led',
      'miner:download_logs',
      'rack:read',
      'rack:manage'
  )
ON CONFLICT DO NOTHING;
