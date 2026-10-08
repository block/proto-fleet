-- Check before any destructive rollback steps. Older servers cannot represent
-- customer-only affiliations; leave all data intact until those affiliations
-- have an explicitly assigned organization role.
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM user_organization WHERE role_id IS NULL) THEN
        RAISE EXCEPTION 'cannot roll back hosted administration while customer-only affiliations exist';
    END IF;
    -- Foundation servers cannot resolve ancestor grants. Require a live direct
    -- administrator before removing those grants; leave recovery explicit.
    IF EXISTS (
        SELECT 1 FROM hosted_account a
        JOIN organization o ON o.id=a.organization_id AND o.deleted_at IS NULL
        JOIN hosted_account root ON root.organization_id=a.organization_id AND root.depth=0
        LEFT JOIN hosted_account parent ON parent.id=a.parent_id AND parent.organization_id=a.organization_id
        WHERE a.depth>0 AND NOT EXISTS (
            SELECT 1 FROM hosted_account_membership m
            JOIN "user" u ON u.id=m.user_id AND u.deleted_at IS NULL
            JOIN user_organization uo ON uo.user_id=u.id AND uo.organization_id=m.organization_id AND uo.deleted_at IS NULL
            WHERE m.organization_id=a.organization_id AND m.account_id=a.id
            AND m.role='account_administrator' AND m.revoked_at IS NULL
            AND (a.capabilities & COALESCE(parent.capabilities,31) & root.capabilities & 9)=9
        )
    ) THEN
        RAISE EXCEPTION 'cannot roll back hosted administration until every customer account has a live direct administrator';
    END IF;
END $$;
DROP TABLE hosted_account_invitation;
DROP TABLE hosted_account_ancestor_grant;
DROP FUNCTION check_hosted_account_ancestor_grant();
DROP TABLE hosted_account_site;
ALTER TABLE hosted_account DROP COLUMN sites_restricted;
ALTER TABLE hosted_account DROP COLUMN site_ceiling_restricted;
ALTER TABLE hosted_account DROP COLUMN authority_revision;
ALTER TABLE user_organization ALTER COLUMN role_id SET NOT NULL;
