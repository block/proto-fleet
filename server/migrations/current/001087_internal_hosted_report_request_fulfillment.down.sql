LOCK TABLE hosted_report_request_fulfillment IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM hosted_report_request_fulfillment) THEN
  RAISE EXCEPTION 'cannot remove retained report request fulfillment';
 END IF;
END $$;
DROP TABLE hosted_report_request_fulfillment;
DROP INDEX idx_hosted_report_issued_request;
