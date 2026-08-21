INSERT INTO permission (key, description) VALUES
    ('notification:read', 'View notification channels, alert rules, silences, and delivery history.'),
    ('notification:manage', 'Create, edit, test, and delete notification channels; pause and resume alert rules; create and lift silences.')
ON CONFLICT (key) DO UPDATE SET description = EXCLUDED.description;

INSERT INTO role_permission (role_id, permission_id)
SELECT r.id, p.id
FROM role r, permission p
WHERE r.builtin_key = 'ADMIN'
  AND r.deleted_at IS NULL
  AND p.key IN ('notification:read', 'notification:manage')
ON CONFLICT (role_id, permission_id) DO NOTHING;
