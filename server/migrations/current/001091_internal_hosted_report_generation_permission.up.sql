-- requires-shared: 1000
LOCK TABLE organization IN SHARE ROW EXCLUSIVE MODE;
LOCK TABLE hosted_account, hosted_account_ancestor_grant IN ACCESS EXCLUSIVE MODE;

ALTER TABLE hosted_account DROP CONSTRAINT hosted_account_capabilities_check;
ALTER TABLE hosted_account ADD CONSTRAINT hosted_account_capabilities_check CHECK (capabilities BETWEEN 1 AND 63);
ALTER TABLE hosted_account_ancestor_grant DROP CONSTRAINT hosted_account_ancestor_grant_capabilities_check;
ALTER TABLE hosted_account_ancestor_grant ADD CONSTRAINT hosted_account_ancestor_grant_capabilities_check CHECK (capabilities BETWEEN 1 AND 63);

-- Preserve existing direct administrators' generation access. Complete source
-- grants continue to support the administrator role; partial grants stay exact.
-- Generation still requires direct membership and never comes from preview.
UPDATE hosted_account SET capabilities=capabilities | 32,
 revision=revision+1, authority_revision=authority_revision+1, updated_at=CURRENT_TIMESTAMP;
UPDATE hosted_account_ancestor_grant SET capabilities=63,
 revision=revision+1, updated_at=CURRENT_TIMESTAMP WHERE capabilities=31;

CREATE OR REPLACE FUNCTION create_hosted_account_root() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO hosted_account(account_id,organization_id,depth,name,capabilities)
 VALUES(gen_random_uuid()::text,NEW.id,0,COALESCE(NULLIF(btrim(NEW.name),''),'Host ' || NEW.id::text),63);
 RETURN NEW;
END;
$$;
