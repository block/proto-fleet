DELETE FROM role_permission
WHERE permission_id IN (
    SELECT id FROM permission
    WHERE key IN ('maintenance:read', 'maintenance:manage')
);

DELETE FROM permission
WHERE key IN ('maintenance:read', 'maintenance:manage');
