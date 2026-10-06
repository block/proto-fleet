DROP TABLE IF EXISTS release_channel_setting;

DELETE FROM role_permission
WHERE permission_id IN (
    SELECT id FROM permission WHERE key = 'instance:update'
);

DELETE FROM permission WHERE key = 'instance:update';
