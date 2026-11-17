LOCK TABLE hosted_account_analysis, hosted_account_analysis_deletion IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM hosted_account_analysis_deletion) THEN
  RAISE EXCEPTION 'analysis deletion receipts prevent rollback';
 END IF;
END $$;
DROP TRIGGER hosted_account_analysis_delete ON hosted_account_analysis;
DROP TRIGGER hosted_account_analysis_no_recreation ON hosted_account_analysis;
DROP TRIGGER hosted_account_analysis_immutable ON hosted_account_analysis;
CREATE TRIGGER hosted_account_analysis_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON hosted_account_analysis
 FOR EACH STATEMENT EXECUTE FUNCTION reject_hosted_account_analysis_mutation();
DROP FUNCTION guard_hosted_account_analysis_deletion();
DROP TABLE hosted_account_analysis_deletion;
DROP FUNCTION guard_hosted_account_analysis_deletion_receipt();
