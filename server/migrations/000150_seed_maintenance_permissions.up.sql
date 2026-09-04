-- Seed maintenance permissions and backfill them onto existing built-in
-- ADMIN and FIELD_TECH roles. Both roles are additive-reconciled, so an
-- explicit migration is required for organizations created before these
-- catalog keys existed.
INSERT INTO permission (key, description) VALUES
    ('maintenance:read', 'View repair tickets, maintenance history, and parts inventory.'),
    ('maintenance:manage', 'Create, assign, update, and close repair tickets; manage parts inventory.')
ON CONFLICT (key) DO UPDATE SET description = EXCLUDED.description;

INSERT INTO role_permission (role_id, permission_id)
SELECT r.id, p.id
FROM role r, permission p
WHERE r.builtin_key IN ('ADMIN', 'FIELD_TECH')
  AND r.deleted_at IS NULL
  AND p.key IN ('maintenance:read', 'maintenance:manage')
ON CONFLICT (role_id, permission_id) DO NOTHING;
