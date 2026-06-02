-- Reverses 000069_seed_activity_read_permission.up.sql by removing
-- activity:read from every role that holds it and then deleting the
-- permission row itself. Rolling back the data migration cleanly is
-- impossible without provenance tracking; the rollback path is rare/
-- dev-only and assumes no operator has hand-granted this key to custom
-- roles. SUPER_ADMIN will re-acquire it at the next boot via the
-- catalog reconciler unless catalog.go is also rolled back.

DELETE FROM role_permission
WHERE permission_id IN (
    SELECT id FROM permission WHERE key = 'activity:read'
);

DELETE FROM permission WHERE key = 'activity:read';
