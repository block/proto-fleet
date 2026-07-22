INSERT INTO permission (key, description) VALUES
    ('cohort:read', 'View cohorts, reservations, and effective desired state.'),
    ('cohort:manage', 'Create, release, and manage cohorts and cohort memberships.')
ON CONFLICT (key) DO UPDATE SET description = EXCLUDED.description;

INSERT INTO role_permission (role_id, permission_id)
SELECT r.id, p.id
FROM role r, permission p
WHERE r.builtin_key = 'ADMIN'
  AND r.deleted_at IS NULL
  AND p.key IN ('cohort:read', 'cohort:manage')
ON CONFLICT (role_id, permission_id) DO NOTHING;
