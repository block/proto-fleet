-- Remove user management fields

-- Remove ADMIN role
DELETE FROM role WHERE name = 'ADMIN';

ALTER TABLE user
DROP COLUMN requires_password_change,
DROP COLUMN last_login_at;
