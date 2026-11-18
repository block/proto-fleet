-- requires-shared: 1074
-- CT1 module power-feed control (turn on, turn off, cycle, clear override).
-- Startup reconciliation grants it to SUPER_ADMIN and seeds it for new ADMIN
-- roles; existing ADMIN roles receive it here. FIELD_TECH and custom roles
-- receive it only when granted explicitly.
INSERT INTO permission (key, description) VALUES
    ('container:power_feed', 'Turn a CT1 module''s power feed on or off, cycle it, and clear its operator override. Cutting a feed removes power from the module''s boards without stopping mining first.')
ON CONFLICT (key) DO NOTHING;
INSERT INTO role_permission (role_id, permission_id)
SELECT r.id, p.id FROM role r CROSS JOIN permission p
WHERE r.builtin_key = 'ADMIN' AND r.deleted_at IS NULL AND p.key = 'container:power_feed'
ON CONFLICT (role_id, permission_id) DO NOTHING;
