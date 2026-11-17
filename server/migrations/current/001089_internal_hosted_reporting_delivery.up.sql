-- requires-shared: 1000
ALTER TABLE hosted_report_queue ADD COLUMN delivered_channel_ids bigint[] NOT NULL DEFAULT '{}';
ALTER TABLE hosted_report_queue DROP CONSTRAINT ck_hosted_report_queue_notification;
ALTER TABLE hosted_report_queue ADD CONSTRAINT ck_hosted_report_queue_notification CHECK(notification_status IN ('pending','sent','failed','acknowledged','quiet','unrouted','exhausted','blocked'));
ALTER TABLE hosted_report_queue_event DROP CONSTRAINT ck_hosted_report_queue_event_kind;
ALTER TABLE hosted_report_queue_event ADD CONSTRAINT ck_hosted_report_queue_event_kind CHECK(kind IN ('changed','attempted','sent','failed','acknowledged','retry','unrouted','exhausted','blocked'));

ALTER TABLE hosted_account_analysis ADD COLUMN request_key text NOT NULL DEFAULT gen_random_uuid()::text;
ALTER TABLE hosted_account_analysis ADD COLUMN request_digest text NOT NULL DEFAULT '';
CREATE UNIQUE INDEX uq_hosted_account_analysis_request ON hosted_account_analysis(organization_id,account_id,request_key);

CREATE INDEX idx_hosted_report_queue_active_page ON hosted_report_queue(organization_id,id DESC) WHERE notification_status<>'quiet';
