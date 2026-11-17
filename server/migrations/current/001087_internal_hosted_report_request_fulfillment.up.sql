-- requires-shared: 1000
CREATE TABLE hosted_report_request_fulfillment (
 id bigserial CONSTRAINT pk_hosted_report_request_fulfillment PRIMARY KEY,
 request_internal_id bigint NOT NULL REFERENCES hosted_report_request(id),
 entry_key text NOT NULL,
 delivered_at timestamptz NOT NULL,
 delivery_ref text NOT NULL,
 communication_ref text NOT NULL,
 evidence text NOT NULL,
 actor_id bigint NOT NULL REFERENCES "user"(id),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CONSTRAINT uq_hosted_report_request_fulfillment_request_entry UNIQUE(request_internal_id,entry_key)
);
CREATE TRIGGER hosted_report_request_fulfillment_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON hosted_report_request_fulfillment FOR EACH STATEMENT EXECUTE FUNCTION reject_hosted_report_append_mutation();
CREATE INDEX idx_hosted_report_issued_request
 ON hosted_report(organization_id,account_id,(period->>'requestRef'))
 WHERE kind='ad_hoc' AND status='issued';
