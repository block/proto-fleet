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

ALTER FUNCTION activity_display_label(TEXT, TEXT, TEXT, JSONB, TEXT)
    RENAME TO activity_display_label_v146;

CREATE OR REPLACE FUNCTION activity_display_label(
    event_type TEXT,
    scope_type TEXT,
    scope_label TEXT,
    metadata JSONB,
    description TEXT
) RETURNS TEXT
LANGUAGE SQL
IMMUTABLE
PARALLEL SAFE
AS $$
SELECT CASE event_type
    WHEN 'maintenance.ticket_created' THEN 'Created repair ticket'
    WHEN 'maintenance.ticket_updated' THEN 'Updated repair ticket'
    WHEN 'maintenance.ticket_deleted' THEN 'Deleted repair ticket'
    WHEN 'maintenance.ticket_bulk_update' THEN 'Bulk updated repair tickets'
    WHEN 'maintenance.comment_created' THEN 'Added repair ticket comment'
    WHEN 'maintenance.comment_deleted' THEN 'Deleted repair ticket comment'
    WHEN 'inventory.part_created' THEN 'Created inventory part'
    WHEN 'inventory.part_updated' THEN 'Updated inventory part'
    WHEN 'inventory.part_deleted' THEN 'Deleted inventory part'
    WHEN 'inventory.parts_imported' THEN 'Imported inventory parts'
    ELSE activity_display_label_v146(event_type, scope_type, scope_label, metadata, description)
END
$$;
