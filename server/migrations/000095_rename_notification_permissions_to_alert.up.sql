-- Rename the notification permission keys to alert:* after the Notifications->Alerts code rename. Data-only: no table/column renames.
INSERT INTO permission (key, description) VALUES
    ('alert:read', 'View alert channels, alert rules, silences, and delivery history.'),
    ('alert:manage', 'Create, edit, test, and delete alert channels; pause and resume alert rules; create and lift silences.')
ON CONFLICT (key) DO UPDATE SET description = EXCLUDED.description;

-- Repoint existing grants to the new keys, skipping any role that already holds the new key (PK would collide).
UPDATE role_permission rp
SET permission_id = new_p.id
FROM permission old_p, permission new_p
WHERE old_p.key = 'notification:read' AND new_p.key = 'alert:read'
  AND rp.permission_id = old_p.id
  AND NOT EXISTS (SELECT 1 FROM role_permission e WHERE e.role_id = rp.role_id AND e.permission_id = new_p.id);

UPDATE role_permission rp
SET permission_id = new_p.id
FROM permission old_p, permission new_p
WHERE old_p.key = 'notification:manage' AND new_p.key = 'alert:manage'
  AND rp.permission_id = old_p.id
  AND NOT EXISTS (SELECT 1 FROM role_permission e WHERE e.role_id = rp.role_id AND e.permission_id = new_p.id);

-- Drop any grants that couldn't be repointed, then remove the obsolete permission rows (FK is ON DELETE RESTRICT).
DELETE FROM role_permission
WHERE permission_id IN (SELECT id FROM permission WHERE key IN ('notification:read', 'notification:manage'));

DELETE FROM permission WHERE key IN ('notification:read', 'notification:manage');
