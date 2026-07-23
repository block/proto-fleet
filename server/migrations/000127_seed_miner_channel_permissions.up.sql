INSERT INTO permission (key, description) VALUES
    ('miner_channel:read', 'View miner channels, reservations, and effective desired state.'),
    ('miner_channel:manage', 'Create, release, and manage miner channels and miner channel memberships.')
ON CONFLICT (key) DO UPDATE SET description = EXCLUDED.description;

INSERT INTO role_permission (role_id, permission_id)
SELECT r.id, p.id
FROM role r, permission p
WHERE r.builtin_key = 'ADMIN'
  AND r.deleted_at IS NULL
  AND p.key IN ('miner_channel:read', 'miner_channel:manage')
ON CONFLICT (role_id, permission_id) DO NOTHING;
