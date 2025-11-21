-- Add fields to support user management features

ALTER TABLE user
ADD COLUMN last_login_at TIMESTAMP(6) NULL,
ADD COLUMN requires_password_change BOOLEAN NOT NULL DEFAULT FALSE;

-- Create ADMIN role for multi-user accounts
-- SUPER_ADMIN is created during initial onboarding, but ADMIN role needs to exist for creating additional users
INSERT INTO role (name, description, created_at, updated_at)
VALUES ('ADMIN', 'Admin role with full permissions except managing SUPER_ADMIN', NOW(), NOW())
ON DUPLICATE KEY UPDATE description = VALUES(description);
