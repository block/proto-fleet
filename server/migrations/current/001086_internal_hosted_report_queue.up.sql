-- requires-shared: 1000
CREATE TABLE hosted_report_queue (
 id bigserial CONSTRAINT pk_hosted_report_queue PRIMARY KEY,
 queue_id text NOT NULL CONSTRAINT uq_hosted_report_queue_queue_id UNIQUE,
 organization_id bigint NOT NULL,
 account_id bigint NOT NULL,
 work_key text NOT NULL,
 owner_id bigint NOT NULL REFERENCES "user"(id),
 site_ids bigint[] NOT NULL,
 projection jsonb NOT NULL,
 fingerprint text NOT NULL,
 generation bigint NOT NULL DEFAULT 1 CONSTRAINT ck_hosted_report_queue_generation CHECK(generation>0),
 notification_status text NOT NULL DEFAULT 'pending' CONSTRAINT ck_hosted_report_queue_notification CHECK(notification_status IN ('pending','sent','failed','acknowledged','quiet')),
 attempts bigint NOT NULL DEFAULT 0,
 retry_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 claim_token text,
 lease_until timestamptz,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CONSTRAINT uq_hosted_report_queue_work UNIQUE(organization_id,account_id,work_key),
 FOREIGN KEY(organization_id,account_id) REFERENCES hosted_account(organization_id,id)
);
CREATE INDEX idx_hosted_report_queue_outbox ON hosted_report_queue(retry_at,id) WHERE notification_status IN ('pending','failed');
CREATE TABLE hosted_report_queue_event (
 id bigserial CONSTRAINT pk_hosted_report_queue_event PRIMARY KEY,
 queue_internal_id bigint NOT NULL REFERENCES hosted_report_queue(id),
 generation bigint NOT NULL,
 kind text NOT NULL CONSTRAINT ck_hosted_report_queue_event_kind CHECK(kind IN ('changed','attempted','sent','failed','acknowledged','retry')),
 actor_id bigint REFERENCES "user"(id),
 reason text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX idx_hosted_report_queue_event_item ON hosted_report_queue_event(queue_internal_id,id);
CREATE FUNCTION reject_hosted_report_queue_event_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'report queue event history is append-only'; END;
$$;
CREATE TRIGGER hosted_report_queue_event_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON hosted_report_queue_event FOR EACH STATEMENT EXECUTE FUNCTION reject_hosted_report_queue_event_mutation();
