-- requires-shared: 1000
-- Stop application and scheduled writers for this upgrade. Drain source writes
-- before installing capture and its capture-time boundary. Do not backfill.
LOCK TABLE device_metrics,miner_state_snapshots,container_module_metrics,
 container_module_diagnostic_observation,hashboard_metrics,hashboard_diagnostic_observation,
 hosted_account_history_sample
 IN SHARE ROW EXCLUSIVE MODE;

-- Topology stamping does not change the admitted measurement or its identity.
-- Keep real value, source-window and installation corrections on the existing
-- normalization path, without retaining delete/reinsert noise on every poll.
DROP TRIGGER record_hosted_hashboard_history ON hashboard_metrics;
CREATE TRIGGER record_hosted_hashboard_history
 -- Keep this list aligned with record_hosted_hashboard_history_sample in migration 1049.
 AFTER INSERT OR UPDATE OF hashboard_id,installation_id,time,collected_at,state,state_observed_at,
  hashrate_hs,power_w,temperature_c OR DELETE ON hashboard_metrics
 FOR EACH ROW EXECUTE FUNCTION record_hosted_hashboard_history_sample('metrics');

CREATE TABLE hosted_account_reporting_evidence_epoch (
 singleton boolean CONSTRAINT pk_hosted_reporting_evidence_epoch PRIMARY KEY DEFAULT true
  CONSTRAINT ck_hosted_reporting_evidence_epoch_singleton CHECK(singleton),
 started_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
INSERT INTO hosted_account_reporting_evidence_epoch(singleton) VALUES(true);

CREATE TABLE hosted_account_reporting_allocation_snapshot (
 interval_id bigint CONSTRAINT pk_hosted_reporting_allocation_snapshot PRIMARY KEY,
 organization_id bigint NOT NULL,
 client_id bigint NOT NULL,
 subclient_id bigint,
 site_id bigint,
 kind integer NOT NULL,
 asset_id bigint NOT NULL,
 device_id bigint NOT NULL,
 device_identifier text NOT NULL,
 started_at timestamptz NOT NULL,
 captured_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

-- An ordinary table is independent of chart chunks and has no automatic expiry.
CREATE TABLE hosted_account_reporting_evidence (
 event_id bigserial CONSTRAINT pk_hosted_reporting_evidence PRIMARY KEY,
 epoch_started_at timestamptz NOT NULL,
 captured_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 transaction_id bigint NOT NULL DEFAULT txid_current(),
 operation text NOT NULL CONSTRAINT ck_hosted_reporting_evidence_operation CHECK(operation IN ('INSERT','UPDATE','DELETE')),
 image text NOT NULL CONSTRAINT ck_hosted_reporting_evidence_image CHECK(image IN ('before','after')),
 time timestamptz NOT NULL,
 interval_id bigint NOT NULL REFERENCES hosted_account_reporting_allocation_snapshot(interval_id),
 metric smallint NOT NULL,
 value double precision,
 complete boolean,
 source_oldest_at timestamptz NOT NULL,
 source_newest_at timestamptz NOT NULL,
 source_id smallint NOT NULL,
 origin_key text NOT NULL,
 origin_time timestamptz NOT NULL,
 origin_collected_at timestamptz NOT NULL
);
CREATE INDEX idx_hosted_account_reporting_evidence_stream_event
 ON hosted_account_reporting_evidence(interval_id,source_id,metric,event_id DESC);
CREATE INDEX idx_hosted_account_reporting_evidence_stream_observation
 ON hosted_account_reporting_evidence(interval_id,source_id,metric,event_id DESC)
 WHERE operation IN ('INSERT','UPDATE') AND image='after';

CREATE FUNCTION reject_hosted_reporting_evidence_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'hosted reporting evidence is append-only';
END;
$$;
CREATE TRIGGER hosted_reporting_evidence_immutable
 BEFORE UPDATE OR DELETE OR TRUNCATE ON hosted_account_reporting_evidence
 FOR EACH STATEMENT EXECUTE FUNCTION reject_hosted_reporting_evidence_mutation();
CREATE TRIGGER hosted_reporting_allocation_snapshot_immutable
 BEFORE UPDATE OR DELETE OR TRUNCATE ON hosted_account_reporting_allocation_snapshot
 FOR EACH STATEMENT EXECUTE FUNCTION reject_hosted_reporting_evidence_mutation();
CREATE TRIGGER hosted_reporting_evidence_epoch_immutable
 BEFORE UPDATE OR DELETE OR TRUNCATE ON hosted_account_reporting_evidence_epoch
 FOR EACH STATEMENT EXECUTE FUNCTION reject_hosted_reporting_evidence_mutation();

CREATE FUNCTION record_hosted_reporting_evidence() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
 sample hosted_account_history_sample;
 sample_image text;
 images text[] := CASE TG_OP WHEN 'UPDATE' THEN ARRAY['before','after']
  WHEN 'DELETE' THEN ARRAY['before'] ELSE ARRAY['after'] END;
BEGIN
 FOREACH sample_image IN ARRAY images LOOP
  IF sample_image='before' THEN sample := OLD; ELSE sample := NEW; END IF;
  INSERT INTO hosted_account_reporting_allocation_snapshot(interval_id,organization_id,client_id,subclient_id,
   site_id,kind,asset_id,device_id,device_identifier,started_at)
  SELECT id,organization_id,client_id,subclient_id,site_id,kind,asset_id,device_id,device_identifier,started_at
  FROM hosted_account_asset_interval WHERE id=sample.interval_id
  ON CONFLICT(interval_id) DO NOTHING;
  INSERT INTO hosted_account_reporting_evidence(epoch_started_at,operation,image,time,interval_id,metric,value,complete,
   source_oldest_at,source_newest_at,source_id,origin_key,origin_time,origin_collected_at)
  SELECT started_at,TG_OP,sample_image,sample.time,sample.interval_id,sample.metric,sample.value,sample.complete,
   sample.source_oldest_at,sample.source_newest_at,sample.source_id,sample.origin_key,sample.origin_time,sample.origin_collected_at
  FROM hosted_account_reporting_evidence_epoch WHERE singleton;
  IF NOT FOUND THEN RAISE EXCEPTION 'hosted reporting evidence epoch is missing'; END IF;
 END LOOP;
 RETURN NULL;
END;
$$;
-- DELETE means normalized-sample deletion. Corrections commonly delete and
-- insert in one transaction; neither deletion nor chunk expiry proves retraction.
CREATE TRIGGER record_hosted_reporting_evidence
 AFTER INSERT OR UPDATE OR DELETE ON hosted_account_history_sample
 FOR EACH ROW EXECUTE FUNCTION record_hosted_reporting_evidence();

CREATE VIEW hosted_account_reporting_capture_status AS
SELECT i.id AS interval_id,i.organization_id,i.client_id,i.subclient_id,i.site_id,i.kind,
 i.asset_id,i.device_id,i.device_identifier,i.started_at AS allocation_started_at,
 epoch.started_at AS hold_started_at,stream.source_id,stream.metric,
 latest.event_id AS last_event_id,latest.operation AS last_event_operation,
 latest.captured_at AS last_event_captured_at,
 observation.event_id AS last_observation_event_id,
 observation.captured_at AS last_observation_captured_at,
 observation.value AS last_observation_value,observation.complete AS last_observation_complete,
 observation.origin_key AS last_observation_origin_key,
 observation.origin_time AS last_observation_origin_time,
 observation.origin_collected_at AS last_observation_collected_at,
 observation.source_oldest_at AS last_observation_source_oldest_at,
 observation.source_newest_at AS last_observation_source_newest_at
FROM hosted_account_asset_interval i
CROSS JOIN hosted_account_reporting_evidence_epoch epoch
JOIN (
 SELECT 5 AS kind,1::smallint AS source_id,hosted_history_metric_code('hashrate') AS metric
 UNION ALL SELECT 5,1::smallint,hosted_history_metric_code('power')
 UNION ALL SELECT 5,1::smallint,hosted_history_metric_code('temperature')
 UNION ALL SELECT 5,2::smallint,hosted_history_metric_code('state')
 UNION ALL SELECT 6,3::smallint,hosted_history_metric_code('hashrate')
 UNION ALL SELECT 6,3::smallint,hosted_history_metric_code('power')
 UNION ALL SELECT 6,3::smallint,hosted_history_metric_code('temperature')
 UNION ALL SELECT 6,4::smallint,hosted_history_metric_code('state')
 UNION ALL SELECT 7,5::smallint,hosted_history_metric_code('hashrate')
 UNION ALL SELECT 7,5::smallint,hosted_history_metric_code('power')
 UNION ALL SELECT 7,5::smallint,hosted_history_metric_code('temperature')
 UNION ALL SELECT 7,6::smallint,hosted_history_metric_code('state')
) stream ON stream.kind=i.kind
LEFT JOIN LATERAL (
 SELECT event_id,operation,captured_at FROM hosted_account_reporting_evidence e
 WHERE e.interval_id=i.id AND e.source_id=stream.source_id AND e.metric=stream.metric
 ORDER BY event_id DESC LIMIT 1
) latest ON true
LEFT JOIN LATERAL (
 SELECT event_id,captured_at,value,complete,origin_key,origin_time,origin_collected_at,source_oldest_at,source_newest_at
 FROM hosted_account_reporting_evidence e WHERE e.interval_id=i.id
  AND e.source_id=stream.source_id AND e.metric=stream.metric
  AND operation IN ('INSERT','UPDATE') AND image='after'
 ORDER BY event_id DESC LIMIT 1
) observation ON true
WHERE i.ended_at IS NULL;
