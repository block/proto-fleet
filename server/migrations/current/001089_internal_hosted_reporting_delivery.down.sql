LOCK TABLE hosted_report_queue, hosted_report_queue_event, hosted_account_analysis IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM hosted_account_analysis WHERE request_digest<>'') OR EXISTS(SELECT 1 FROM hosted_report_queue WHERE cardinality(delivered_channel_ids)>0 OR notification_status IN ('unrouted','exhausted','blocked')) OR EXISTS(SELECT 1 FROM hosted_report_queue_event WHERE kind IN ('unrouted','exhausted','blocked')) THEN
  RAISE EXCEPTION 'reporting notification history prevents rollback';
 END IF;
END $$;
ALTER TABLE hosted_report_queue DROP CONSTRAINT ck_hosted_report_queue_notification;
ALTER TABLE hosted_report_queue ADD CONSTRAINT ck_hosted_report_queue_notification CHECK(notification_status IN ('pending','sent','failed','acknowledged','quiet'));
ALTER TABLE hosted_report_queue_event DROP CONSTRAINT ck_hosted_report_queue_event_kind;
ALTER TABLE hosted_report_queue_event ADD CONSTRAINT ck_hosted_report_queue_event_kind CHECK(kind IN ('changed','attempted','sent','failed','acknowledged','retry'));
ALTER TABLE hosted_report_queue DROP COLUMN delivered_channel_ids;

DROP INDEX uq_hosted_account_analysis_request;
ALTER TABLE hosted_account_analysis DROP COLUMN request_key, DROP COLUMN request_digest;

DROP INDEX idx_hosted_report_queue_active_page;
