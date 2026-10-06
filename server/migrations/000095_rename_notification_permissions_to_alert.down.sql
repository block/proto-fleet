-- Reverse: restore the notification:* permission keys and repoint grants back.
INSERT INTO permission (key, description) VALUES
    ('notification:read', 'View notification channels, alert rules, silences, and delivery history.'),
    ('notification:manage', 'Create, edit, test, and delete notification channels; pause and resume alert rules; create and lift silences.')
ON CONFLICT (key) DO UPDATE SET description = EXCLUDED.description;

UPDATE role_permission rp
SET permission_id = old_p.id
FROM permission new_p, permission old_p
WHERE new_p.key = 'alert:read' AND old_p.key = 'notification:read'
  AND rp.permission_id = new_p.id
  AND NOT EXISTS (SELECT 1 FROM role_permission e WHERE e.role_id = rp.role_id AND e.permission_id = old_p.id);

UPDATE role_permission rp
SET permission_id = old_p.id
FROM permission new_p, permission old_p
WHERE new_p.key = 'alert:manage' AND old_p.key = 'notification:manage'
  AND rp.permission_id = new_p.id
  AND NOT EXISTS (SELECT 1 FROM role_permission e WHERE e.role_id = rp.role_id AND e.permission_id = old_p.id);

DELETE FROM role_permission
WHERE permission_id IN (SELECT id FROM permission WHERE key IN ('alert:read', 'alert:manage'));

DELETE FROM permission WHERE key IN ('alert:read', 'alert:manage');
