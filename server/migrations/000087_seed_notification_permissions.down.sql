-- Reverses 000087_seed_notification_permissions.up.sql by removing the
-- notification permissions from every role that holds them and then
-- deleting the permission rows themselves. Rolling back the data
-- migration cleanly is impossible without provenance tracking; the
-- rollback path is rare/dev-only and assumes no operator has
-- hand-granted these keys to custom roles. SUPER_ADMIN will re-acquire
-- them at the next boot via the catalog reconciler unless catalog.go
-- is also rolled back.

DELETE FROM role_permission
WHERE permission_id IN (
    SELECT id FROM permission WHERE key IN ('notification:read', 'notification:manage')
);

DELETE FROM permission WHERE key IN ('notification:read', 'notification:manage');
