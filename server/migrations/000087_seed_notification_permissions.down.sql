-- Reverses 000087: removes notification permissions from all roles, then deletes the permission rows (dev-only; assumes no custom-role hand-grants).

DELETE FROM role_permission
WHERE permission_id IN (
    SELECT id FROM permission WHERE key IN ('notification:read', 'notification:manage')
);

DELETE FROM permission WHERE key IN ('notification:read', 'notification:manage');
