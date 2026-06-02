-- Reverses 000063_seed_pool_permissions.up.sql by removing pool:read and
-- pool:manage from every role that holds them and then deleting the
-- permission rows themselves. Rolling back the data migration cleanly is
-- impossible without provenance tracking; the rollback path is rare/
-- dev-only and assumes no operator has hand-granted these keys to custom
-- roles. SUPER_ADMIN will re-acquire them at the next boot via the
-- catalog reconciler unless catalog.go is also rolled back.

DELETE FROM role_permission
WHERE permission_id IN (
    SELECT id FROM permission WHERE key IN ('pool:read', 'pool:manage')
);

DELETE FROM permission WHERE key IN ('pool:read', 'pool:manage');
