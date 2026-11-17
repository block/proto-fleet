-- requires-shared: 1000
CREATE TABLE hosted_account_analysis_deletion (
 analysis_id text CONSTRAINT pk_hosted_account_analysis_deletion PRIMARY KEY,
 organization_id bigint NOT NULL,
 account_id bigint NOT NULL,
 created_by bigint NOT NULL REFERENCES "user"(id),
 deleted_by bigint NOT NULL REFERENCES "user"(id),
 deleted_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 request_key text NOT NULL,
 request_digest text NOT NULL,
 CONSTRAINT fk_hosted_account_analysis_deletion_account FOREIGN KEY(organization_id,account_id) REFERENCES hosted_account(organization_id,id),
 CONSTRAINT uq_hosted_account_analysis_deletion_request UNIQUE(organization_id,account_id,request_key)
);

CREATE FUNCTION guard_hosted_account_analysis_deletion_receipt() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP <> 'INSERT' THEN
  RAISE EXCEPTION 'analysis deletion receipts are immutable';
 END IF;
 IF NOT EXISTS (
  SELECT 1 FROM hosted_account_analysis a WHERE a.analysis_id=NEW.analysis_id
   AND a.organization_id=NEW.organization_id AND a.account_id=NEW.account_id
   AND a.created_by=NEW.created_by AND a.request_key=NEW.request_key AND a.request_digest=NEW.request_digest
 ) THEN
  RAISE EXCEPTION 'analysis deletion receipt must match a saved analysis';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER hosted_account_analysis_deletion_receipt_insert BEFORE INSERT ON hosted_account_analysis_deletion
 FOR EACH ROW EXECUTE FUNCTION guard_hosted_account_analysis_deletion_receipt();
CREATE TRIGGER hosted_account_analysis_deletion_receipt_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON hosted_account_analysis_deletion
 FOR EACH STATEMENT EXECUTE FUNCTION guard_hosted_account_analysis_deletion_receipt();

CREATE FUNCTION guard_hosted_account_analysis_deletion() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP = 'INSERT' THEN
  IF EXISTS (
   SELECT 1 FROM hosted_account_analysis_deletion d WHERE d.analysis_id=NEW.analysis_id
    OR (d.organization_id=NEW.organization_id AND d.account_id=NEW.account_id AND d.request_key=NEW.request_key)
  ) THEN
   RAISE EXCEPTION 'a permanently deleted analysis cannot be recreated';
  END IF;
  RETURN NEW;
 END IF;
 IF NOT EXISTS (
  SELECT 1 FROM hosted_account_analysis_deletion d WHERE d.analysis_id=OLD.analysis_id
   AND d.organization_id=OLD.organization_id AND d.account_id=OLD.account_id
   AND d.created_by=OLD.created_by AND d.request_key=OLD.request_key AND d.request_digest=OLD.request_digest
 ) THEN
  RAISE EXCEPTION 'analysis deletion requires a retained matching receipt';
 END IF;
 RETURN OLD;
END;
$$;
DROP TRIGGER hosted_account_analysis_immutable ON hosted_account_analysis;
CREATE TRIGGER hosted_account_analysis_immutable BEFORE UPDATE OR TRUNCATE ON hosted_account_analysis
 FOR EACH STATEMENT EXECUTE FUNCTION reject_hosted_account_analysis_mutation();
CREATE TRIGGER hosted_account_analysis_delete BEFORE DELETE ON hosted_account_analysis
 FOR EACH ROW EXECUTE FUNCTION guard_hosted_account_analysis_deletion();
CREATE TRIGGER hosted_account_analysis_no_recreation BEFORE INSERT ON hosted_account_analysis
 FOR EACH ROW EXECUTE FUNCTION guard_hosted_account_analysis_deletion();
