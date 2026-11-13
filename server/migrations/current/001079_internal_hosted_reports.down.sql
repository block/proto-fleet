LOCK TABLE hosted_report,hosted_report_request IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM hosted_report) OR EXISTS(SELECT 1 FROM hosted_report_request) THEN
  RAISE EXCEPTION 'cannot remove retained report history';
 END IF;
END $$;
DROP TABLE hosted_report_request,hosted_report_delivery;
DROP FUNCTION reject_hosted_report_append_mutation();
DROP TABLE hosted_report;
DROP FUNCTION guard_hosted_report();
DROP FUNCTION hosted_report_evidence_cutoff(timestamptz);
ALTER TABLE hosted_account_reporting_hour_gap ALTER COLUMN recorded_at SET DEFAULT clock_timestamp();
ALTER TABLE hosted_account_reporting_hour ALTER COLUMN captured_at SET DEFAULT clock_timestamp();
DROP FUNCTION hosted_reporting_evidence_step_at();
