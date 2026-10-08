-- Exclude key creators before taking the live-key snapshot; retain this lock
-- through conversion so no account credential can enter the old schema.
LOCK TABLE api_key IN ACCESS EXCLUSIVE MODE;

-- Never turn a live customer credential into an organization credential.
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM api_key WHERE subject_kind='account' AND revoked_at IS NULL) THEN
        RAISE EXCEPTION 'revoke all account API keys before rolling back account key support';
    END IF;
END $$;
DROP TABLE hosted_account_key_descendant;
DROP FUNCTION check_hosted_account_key_descendant();
DROP TABLE hosted_account_key;
DROP FUNCTION check_hosted_account_key();
ALTER TABLE api_key DROP CONSTRAINT uq_api_key_org_id;
ALTER TABLE api_key DROP CONSTRAINT ck_api_key_subject;
UPDATE api_key SET subject_kind='user' WHERE subject_kind='account';
ALTER TABLE api_key ADD CONSTRAINT ck_api_key_subject CHECK (
    (subject_kind = 'user' AND user_id IS NOT NULL AND fleet_node_id IS NULL) OR
    (subject_kind = 'fleet_node' AND user_id IS NULL AND fleet_node_id IS NOT NULL)
);
