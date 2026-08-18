INSERT INTO permission (key, description) VALUES
    ('channel:read', 'View software channels, releases, and channel membership.'),
    ('channel:manage', 'Create and manage software channels, releases, and channel membership.'),
    ('rollout:read', 'View firmware rollout state, members, evidence, and history.'),
    ('rollout:manage', 'Create firmware rollouts and their frozen batch plans.'),
    ('rollout:control', 'Admit, pause, resume, abort, complete, and revert firmware rollouts.')
ON CONFLICT (key) DO UPDATE SET description = EXCLUDED.description;

INSERT INTO role_permission (role_id, permission_id)
SELECT role.id, permission.id
FROM role
CROSS JOIN permission
WHERE role.builtin_key IN ('SUPER_ADMIN', 'ADMIN')
  AND role.is_builtin = TRUE
  AND role.deleted_at IS NULL
  AND permission.key IN (
      'channel:read',
      'channel:manage',
      'rollout:read',
      'rollout:manage',
      'rollout:control'
  )
ON CONFLICT (role_id, permission_id) DO NOTHING;
