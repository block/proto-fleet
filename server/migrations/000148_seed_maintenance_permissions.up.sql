-- Seed the maintenance permission rows before per-org built-in roles are
-- created. The onboarding path resolves each built-in role's seed keys from
-- the permission table, so catalog entries must also be present in migrations.
--
-- Existing ADMIN and FIELD_TECH grants are intentionally left unchanged.
-- SUPER_ADMIN converges to the complete catalog during startup, while
-- operators retain control of additive-only roles in existing organizations.
INSERT INTO permission (key, description) VALUES
    ('maintenance:read', 'View repair tickets, the maintenance queue, and repair history.'),
    ('maintenance:manage', 'Create, assign, update, and close repair tickets.')
ON CONFLICT (key) DO UPDATE SET description = EXCLUDED.description;
