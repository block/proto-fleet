-- Clear all role_permission rows attached to built-in roles, then drop
-- the FIELD_TECH role row. SUPER_ADMIN and ADMIN are left in place
-- because external rows (user_organization, user_organization_role)
-- still reference them; the down migration only undoes what 000052
-- introduced as net-new.

DELETE FROM role_permission
WHERE role_id IN (SELECT id FROM role WHERE is_builtin = TRUE);

DELETE FROM role WHERE builtin_key = 'FIELD_TECH';

UPDATE role
SET is_builtin = FALSE,
    builtin_key = NULL
WHERE builtin_key IN ('SUPER_ADMIN', 'ADMIN');

DELETE FROM permission;
