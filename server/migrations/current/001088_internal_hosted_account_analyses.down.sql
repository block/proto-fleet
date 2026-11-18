LOCK TABLE hosted_account_analysis IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM hosted_account_analysis) THEN
  RAISE EXCEPTION 'saved analyses prevent rollback';
 END IF;
END $$;
DROP TRIGGER hosted_account_analysis_immutable ON hosted_account_analysis;
DROP FUNCTION reject_hosted_account_analysis_mutation();
DROP TABLE hosted_account_analysis;
