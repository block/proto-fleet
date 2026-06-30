INSERT INTO permission (key, description) VALUES
    ('cohort:read', 'View cohorts, reservations, and effective desired state.'),
    ('cohort:manage', 'Create, release, and manage cohorts and cohort memberships.')
ON CONFLICT (key) DO UPDATE SET description = EXCLUDED.description;

-- Backfill the new keys onto existing ADMIN roles. The boot reconciler runs
-- ADMIN in additive mode and does NOT re-assert seed permissions onto an
-- already-created ADMIN role (see reconcile.go), so without this migration,
-- deployments upgraded from a prior release would never grant ADMIN the cohort
-- keys and every CohortService endpoint would deny. SUPER_ADMIN is reconciled
-- in full mode at boot and converges on its own; FIELD_TECH does not receive
-- cohort permissions by design.
--
-- Scoped to roles with builtin_key='ADMIN' so operator-created custom roles
-- are not touched. ON CONFLICT makes this safe to replay against orgs that
-- already hold either key.
INSERT INTO role_permission (role_id, permission_id)
SELECT r.id, p.id
FROM role r, permission p
WHERE r.builtin_key = 'ADMIN'
  AND r.deleted_at IS NULL
  AND p.key IN ('cohort:read', 'cohort:manage')
ON CONFLICT (role_id, permission_id) DO NOTHING;
