DROP TABLE organization_invitation;
DROP FUNCTION team_invitation_authority_fingerprint(BIGINT, BIGINT, BIGINT);
DROP TRIGGER role_permission_revision ON role_permission;
DROP FUNCTION bump_role_permission_revision();
ALTER TABLE role DROP COLUMN permission_revision;
