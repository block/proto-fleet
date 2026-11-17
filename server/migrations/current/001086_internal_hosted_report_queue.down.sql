LOCK TABLE hosted_report_queue,hosted_report_queue_event IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM hosted_report_queue) OR EXISTS(SELECT 1 FROM hosted_report_queue_event) THEN
  RAISE EXCEPTION 'retained report queue and notification history prevents rollback';
 END IF;
END $$;
DROP TABLE hosted_report_queue_event;
DROP FUNCTION reject_hosted_report_queue_event_mutation();
DROP TABLE hosted_report_queue;
