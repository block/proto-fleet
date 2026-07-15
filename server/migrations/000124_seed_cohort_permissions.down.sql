DELETE FROM role_permission WHERE permission_id IN (
    SELECT id FROM permission WHERE key IN ('cohort:read', 'cohort:manage')
);
DELETE FROM permission WHERE key IN ('cohort:read', 'cohort:manage');
