DROP FUNCTION activity_display_label(TEXT, TEXT, TEXT, JSONB, TEXT);
ALTER FUNCTION activity_display_label_v146(TEXT, TEXT, TEXT, JSONB, TEXT)
    RENAME TO activity_display_label;

DELETE FROM role_permission
WHERE permission_id IN (
    SELECT id FROM permission
    WHERE key IN ('maintenance:read', 'maintenance:manage')
);
DELETE FROM permission
WHERE key IN ('maintenance:read', 'maintenance:manage');
