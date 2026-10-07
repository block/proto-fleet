DROP TRIGGER organization_hosted_account_root ON organization;
DROP FUNCTION create_hosted_account_root();
DROP TABLE hosted_account_membership;
DROP TABLE hosted_account;
DROP FUNCTION check_hosted_account_hierarchy();
DELETE FROM role_permission WHERE permission_id IN (SELECT id FROM permission WHERE key = 'account:manage');
DELETE FROM permission WHERE key = 'account:manage';
