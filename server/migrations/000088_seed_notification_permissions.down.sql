DELETE FROM role_permission
WHERE permission_id IN (
    SELECT id FROM permission WHERE key IN ('notification:read', 'notification:manage')
);

DELETE FROM permission WHERE key IN ('notification:read', 'notification:manage');
