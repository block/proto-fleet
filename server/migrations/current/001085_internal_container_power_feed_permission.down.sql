DELETE FROM role_permission WHERE permission_id IN (SELECT id FROM permission WHERE key = 'container:power_feed');
DELETE FROM permission WHERE key = 'container:power_feed';
