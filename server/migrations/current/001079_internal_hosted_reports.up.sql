-- requires-shared: 1000
CREATE TABLE hosted_report (
 id bigserial CONSTRAINT pk_hosted_report PRIMARY KEY,
 report_id text NOT NULL CONSTRAINT uq_hosted_report_report_id UNIQUE,
 organization_id bigint NOT NULL,
 account_id bigint NOT NULL,
 series_id text NOT NULL,
 number bigint NOT NULL CONSTRAINT ck_hosted_report_number CHECK(number>0),
 correction_of bigint,
 correction_reason text NOT NULL DEFAULT '',
 kind text NOT NULL CONSTRAINT ck_hosted_report_kind CHECK(kind IN ('monthly','quarterly','annual','ad_hoc')),
 period_start timestamptz NOT NULL,
 period_end timestamptz NOT NULL,
 as_of timestamptz NOT NULL,
 timezone text NOT NULL,
 period jsonb NOT NULL,
 input_revision bigint NOT NULL CONSTRAINT ck_hosted_report_input_revision CHECK(input_revision>0),
 result jsonb NOT NULL,
 site_ids bigint[] NOT NULL,
 created_by bigint NOT NULL REFERENCES "user"(id),
 owner_id bigint NOT NULL REFERENCES "user"(id),
 status text NOT NULL DEFAULT 'draft' CONSTRAINT ck_hosted_report_status CHECK(status IN ('draft','issued')),
 revision bigint NOT NULL DEFAULT 1 CONSTRAINT ck_hosted_report_revision CHECK(revision>0),
 issued_at timestamptz,
 issued_by bigint REFERENCES "user"(id),
 incomplete_exception text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 -- Monitoring evidence is not copied. A report's evidence is, per allocated
 -- interval and hour, the highest hosted_account_reporting_hour revision with
 -- captured_at<=evidence_cutoff, plus the gaps with recorded_at<=evidence_cutoff.
 evidence_cutoff timestamptz,
 CONSTRAINT uq_hosted_report_scope_id UNIQUE(organization_id,account_id,id),
 CONSTRAINT uq_hosted_report_series_number UNIQUE(organization_id,account_id,series_id,number),
 FOREIGN KEY(organization_id,account_id) REFERENCES hosted_account(organization_id,id),
 FOREIGN KEY(organization_id,account_id,correction_of) REFERENCES hosted_report(organization_id,account_id,id),
 CONSTRAINT ck_hosted_report_period CHECK(period_start<period_end AND as_of>=period_end),
 CONSTRAINT ck_hosted_report_correction CHECK((correction_of IS NULL AND number=1 AND correction_reason='') OR (correction_of IS NOT NULL AND number>1 AND length(btrim(correction_reason)) BETWEEN 1 AND 2000)),
 CONSTRAINT ck_hosted_report_issue CHECK((status='issued' AND issued_at IS NOT NULL AND issued_by IS NOT NULL AND evidence_cutoff IS NOT NULL) OR (status<>'issued' AND issued_at IS NULL AND issued_by IS NULL))
);
CREATE INDEX idx_hosted_report_queue ON hosted_report(organization_id,account_id,status,period_start DESC,id DESC);
CREATE FUNCTION guard_hosted_report() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='TRUNCATE' THEN RAISE EXCEPTION 'report history cannot be truncated'; END IF;
 IF OLD.status='issued' THEN RAISE EXCEPTION 'issued reports are immutable'; END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 IF NEW.report_id<>OLD.report_id OR NEW.organization_id<>OLD.organization_id OR NEW.account_id<>OLD.account_id OR NEW.series_id<>OLD.series_id OR NEW.number<>OLD.number OR NEW.correction_of IS DISTINCT FROM OLD.correction_of THEN RAISE EXCEPTION 'report identity is immutable'; END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER hosted_report_guard BEFORE UPDATE OR DELETE ON hosted_report FOR EACH ROW EXECUTE FUNCTION guard_hosted_report();
CREATE TRIGGER hosted_report_truncate_guard BEFORE TRUNCATE ON hosted_report FOR EACH STATEMENT EXECUTE FUNCTION guard_hosted_report();
CREATE TABLE hosted_report_delivery (
 id bigserial CONSTRAINT pk_hosted_report_delivery PRIMARY KEY,
 report_internal_id bigint NOT NULL REFERENCES hosted_report(id),
 request_key text NOT NULL,
 channel text NOT NULL,
 status text NOT NULL CONSTRAINT ck_hosted_report_delivery_status CHECK(status IN ('attempted','failed','confirmed')),
 sent_at timestamptz,
 received_at timestamptz,
 source_ref text NOT NULL,
 evidence text NOT NULL DEFAULT '',
 failure_category text NOT NULL DEFAULT '' CONSTRAINT ck_hosted_report_delivery_failure CHECK(failure_category IN ('','unavailable','rejected','unconfirmed','other')),
 actor_id bigint NOT NULL REFERENCES "user"(id),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CONSTRAINT uq_hosted_report_delivery_request UNIQUE(report_internal_id,request_key),
 CONSTRAINT ck_hosted_report_delivery_confirmation CHECK(status<>'confirmed' OR (length(btrim(channel))>0 AND length(btrim(source_ref))>0 AND length(btrim(evidence))>0 AND sent_at IS NOT NULL)),
 CONSTRAINT ck_hosted_report_delivery_failed CHECK(status<>'failed' OR failure_category<>'')
);
CREATE UNIQUE INDEX idx_hosted_report_delivery_confirmation ON hosted_report_delivery(report_internal_id) WHERE status='confirmed';
CREATE TABLE hosted_report_request (
 id bigserial CONSTRAINT pk_hosted_report_request PRIMARY KEY,
 request_id text NOT NULL CONSTRAINT uq_hosted_report_request_request_id UNIQUE,
 organization_id bigint NOT NULL,
 account_id bigint NOT NULL,
 received_at timestamptz NOT NULL,
 source_ref text NOT NULL,
 requester text NOT NULL,
 scope text NOT NULL,
 owner_id bigint NOT NULL REFERENCES "user"(id),
 target_at timestamptz NOT NULL,
 limitations text NOT NULL,
 communication_ref text NOT NULL,
 delivery_ref text NOT NULL,
 actor_id bigint NOT NULL REFERENCES "user"(id),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 FOREIGN KEY(organization_id,account_id) REFERENCES hosted_account(organization_id,id)
);
CREATE INDEX idx_hosted_report_request_history ON hosted_report_request(organization_id,account_id,received_at DESC,id DESC);
CREATE FUNCTION reject_hosted_report_append_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'report delivery and request history is append-only'; END;
$$;
CREATE TRIGGER hosted_report_delivery_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON hosted_report_delivery FOR EACH STATEMENT EXECUTE FUNCTION reject_hosted_report_append_mutation();
CREATE TRIGGER hosted_report_request_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON hosted_report_request FOR EACH STATEMENT EXECUTE FUNCTION reject_hosted_report_append_mutation();

-- One capture time per transaction for hourly evidence and gaps: the database
-- clock at the transaction's first evidence row. The writers
-- (hosted_reporting_hour_begin_close, _finish_close and _revise) take the
-- control row FOR UPDATE before their first insert and hold it until commit, so
-- every row of a writer step carries one time taken while that step held the
-- row, and any cutoff admits the step whole or not at all.
CREATE FUNCTION hosted_reporting_evidence_step_at() RETURNS timestamptz LANGUAGE plpgsql AS $$
DECLARE micros text := current_setting('hosted_reporting.evidence_step_at',true);
BEGIN
 IF micros IS NULL OR micros='' THEN
  micros := (extract(epoch FROM clock_timestamp())*1000000)::bigint::text;
  PERFORM set_config('hosted_reporting.evidence_step_at',micros,true);
 END IF;
 RETURN timestamptz 'epoch'+micros::bigint*INTERVAL '1 microsecond';
END;
$$;
ALTER TABLE hosted_account_reporting_hour ALTER COLUMN captured_at SET DEFAULT hosted_reporting_evidence_step_at();
ALTER TABLE hosted_account_reporting_hour_gap ALTER COLUMN recorded_at SET DEFAULT hosted_reporting_evidence_step_at();

-- The evidence cutoff of one report preparation, at most as_of. Writers of
-- hourly evidence and gaps hold the control row FOR UPDATE while they insert.
-- Once this share lock is granted, every row stamped at or before the returned
-- time is committed, and the wait below makes every later writer stamp a later
-- time. An as_of earlier than that time cannot split a committed step, because
-- the step's rows share one capture time. Call it as its own statement, outside
-- the preparation transaction, so the lock is released at once. It assumes the
-- database clock never steps backwards.
CREATE FUNCTION hosted_report_evidence_cutoff(as_of timestamptz) RETURNS timestamptz LANGUAGE plpgsql AS $$
DECLARE cutoff timestamptz;
BEGIN
 PERFORM 1 FROM hosted_account_reporting_hour_control WHERE singleton FOR SHARE;
 cutoff := clock_timestamp();
 WHILE clock_timestamp()<=cutoff LOOP NULL; END LOOP;
 RETURN LEAST(cutoff,as_of);
END;
$$;
