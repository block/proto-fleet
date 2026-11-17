LOCK TABLE organization IN SHARE ROW EXCLUSIVE MODE;
LOCK TABLE hosted_account, hosted_account_ancestor_grant IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM hosted_account WHERE (capabilities & 32)=0) THEN
  RAISE EXCEPTION 'disabled report generation prevents rollback';
 END IF;
END $$;

CREATE OR REPLACE FUNCTION create_hosted_account_root() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO hosted_account(account_id,organization_id,depth,name,capabilities)
 VALUES(gen_random_uuid()::text,NEW.id,0,COALESCE(NULLIF(btrim(NEW.name),''),'Host ' || NEW.id::text),31);
 RETURN NEW;
END;
$$;

UPDATE hosted_account SET capabilities=capabilities & 31,
 revision=revision+1, authority_revision=authority_revision+1, updated_at=CURRENT_TIMESTAMP;
UPDATE hosted_account_ancestor_grant SET capabilities=capabilities & 31,
 revision=revision+1, updated_at=CURRENT_TIMESTAMP WHERE (capabilities & 32)<>0;
ALTER TABLE hosted_account DROP CONSTRAINT hosted_account_capabilities_check;
ALTER TABLE hosted_account ADD CONSTRAINT hosted_account_capabilities_check CHECK (capabilities BETWEEN 1 AND 31);
ALTER TABLE hosted_account_ancestor_grant DROP CONSTRAINT hosted_account_ancestor_grant_capabilities_check;
ALTER TABLE hosted_account_ancestor_grant ADD CONSTRAINT hosted_account_ancestor_grant_capabilities_check CHECK (capabilities BETWEEN 1 AND 31);
