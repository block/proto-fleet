-- requires-shared: 1000
-- Hourly hosted reporting evidence: one append-only row per allocated interval
-- and closed hour, with corrections appended as revisions. Hours are UTC
-- time_bucket hours, matching hosted_account_history_hourly.

-- Stop application and scheduled writers for this upgrade. Drain source writes
-- in the 1055 order before installing correction markers on the sample table.
LOCK TABLE device_metrics,miner_state_snapshots,container_module_metrics,
 container_module_diagnostic_observation,hashboard_metrics,hashboard_diagnostic_observation,
 hosted_account_history_sample
 IN SHARE ROW EXCLUSIVE MODE;

-- Retire the raw per-poll hold from 1055 and delete its events; the feature is
-- not live. Its epoch marked the raw-hold start, so it goes too. The allocation
-- snapshot, reject_hosted_reporting_evidence_mutation() and the narrowed
-- record_hosted_hashboard_history trigger stay: hourly evidence reuses them.
-- Operators paused 1055 capture by dropping its trigger, so it may be gone.
DROP VIEW hosted_account_reporting_capture_status;
DROP TRIGGER IF EXISTS record_hosted_reporting_evidence ON hosted_account_history_sample;
DROP FUNCTION record_hosted_reporting_evidence();
DROP TABLE hosted_account_reporting_evidence;
DROP TABLE hosted_account_reporting_evidence_epoch;

-- Closer state. closing_through and closed_through are exclusive hour-end
-- watermarks: hours with bucket < closing_through are closing (writers enqueue
-- markers for them), and hours with bucket < closed_through have evidence rows
-- or a covering gap row. Hours before hold_started_at are outside the hold.
CREATE TABLE hosted_account_reporting_hour_control (
 singleton boolean CONSTRAINT pk_hosted_reporting_hour_control PRIMARY KEY DEFAULT true
  CONSTRAINT ck_hosted_reporting_hour_control_singleton CHECK(singleton),
 hold_started_at timestamptz NOT NULL,
 closing_through timestamptz NOT NULL,
 closed_through timestamptz NOT NULL,
 last_success_at timestamptz,
 CONSTRAINT ck_hosted_reporting_hour_control_aligned CHECK(time_bucket(INTERVAL '1 hour',hold_started_at)=hold_started_at
  AND time_bucket(INTERVAL '1 hour',closing_through)=closing_through
  AND time_bucket(INTERVAL '1 hour',closed_through)=closed_through),
 CONSTRAINT ck_hosted_reporting_hour_control_order CHECK(hold_started_at<=closed_through AND closed_through<=closing_through)
);
-- The hold starts at the first full hour after this migration; nothing is backfilled.
INSERT INTO hosted_account_reporting_hour_control(singleton,hold_started_at,closing_through,closed_through)
SELECT true,start,start,start FROM (SELECT time_bucket(INTERVAL '1 hour',clock_timestamp())+INTERVAL '1 hour' AS start) hold;

-- Per-metric aggregates mirror hosted_account_history_hourly. Report counts
-- include samples without a value, so "no report" (0) differs from "reported
-- a missing value" (report count > 0, count 0, NULL sum). The capture window
-- is the interval's overlap with the hour at capture time, NULL without overlap.
CREATE TABLE hosted_account_reporting_hour (
 interval_id bigint NOT NULL CONSTRAINT fk_hosted_reporting_hour_snapshot
  REFERENCES hosted_account_reporting_allocation_snapshot(interval_id),
 bucket timestamptz NOT NULL CONSTRAINT ck_hosted_reporting_hour_bucket CHECK(time_bucket(INTERVAL '1 hour',bucket)=bucket),
 revision integer NOT NULL CONSTRAINT ck_hosted_reporting_hour_revision CHECK(revision>=1),
 captured_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 capture_reason text NOT NULL CONSTRAINT ck_hosted_reporting_hour_reason CHECK(capture_reason IN ('closed','unattributable')),
 no_source_data boolean NOT NULL,
 window_start timestamptz,
 window_end timestamptz,
 hashrate_report_count integer NOT NULL,
 hashrate_count integer NOT NULL,
 hashrate_positive_count integer NOT NULL,
 hashrate_sum double precision,
 hashrate_complete boolean,
 hashrate_source_oldest_at timestamptz,
 hashrate_source_newest_at timestamptz,
 power_report_count integer NOT NULL,
 power_count integer NOT NULL,
 power_sum double precision,
 power_complete boolean,
 power_source_oldest_at timestamptz,
 power_source_newest_at timestamptz,
 temperature_report_count integer NOT NULL,
 temperature_count integer NOT NULL,
 temperature_sum double precision,
 temperature_latest_value double precision,
 temperature_latest_at timestamptz,
 temperature_source_oldest_at timestamptz,
 temperature_source_newest_at timestamptz,
 state_report_count integer NOT NULL,
 state_count integer NOT NULL,
 state_latest_value double precision,
 state_latest_at timestamptz,
 state_source_oldest_at timestamptz,
 state_source_newest_at timestamptz,
 CONSTRAINT pk_hosted_reporting_hour PRIMARY KEY(interval_id,bucket,revision),
 CONSTRAINT ck_hosted_reporting_hour_window CHECK(CASE WHEN window_start IS NULL OR window_end IS NULL
  THEN window_start IS NULL AND window_end IS NULL
  ELSE window_start>=bucket AND window_start<window_end AND window_end-bucket<=INTERVAL '1 hour' END)
);

-- Hours the closer will never write, covering bucket >= from_bucket and < to_bucket.
CREATE TABLE hosted_account_reporting_hour_gap (
 gap_id bigint GENERATED ALWAYS AS IDENTITY CONSTRAINT pk_hosted_reporting_hour_gap PRIMARY KEY,
 from_bucket timestamptz NOT NULL,
 to_bucket timestamptz NOT NULL,
 reason text NOT NULL CONSTRAINT ck_hosted_reporting_hour_gap_reason CHECK(reason<>''),
 recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CONSTRAINT ck_hosted_reporting_hour_gap_bounds CHECK(from_bucket<to_bucket
  AND time_bucket(INTERVAL '1 hour',from_bucket)=from_bucket AND time_bucket(INTERVAL '1 hour',to_bucket)=to_bucket)
);

CREATE TRIGGER hosted_reporting_hour_immutable
 BEFORE UPDATE OR DELETE OR TRUNCATE ON hosted_account_reporting_hour
 FOR EACH STATEMENT EXECUTE FUNCTION reject_hosted_reporting_evidence_mutation();
CREATE TRIGGER hosted_reporting_hour_gap_immutable
 BEFORE UPDATE OR DELETE OR TRUNCATE ON hosted_account_reporting_hour_gap
 FOR EACH STATEMENT EXECUTE FUNCTION reject_hosted_reporting_evidence_mutation();

-- Dirty interval-hours awaiting recomputation. Markers are deliberately not
-- deduplicated: every enqueue inserts its own row, and a claim deletes only the
-- rows it can see, so a writer still in flight keeps its marker for the next run.
CREATE TABLE hosted_account_reporting_hour_marker (
 marker_id bigint GENERATED ALWAYS AS IDENTITY CONSTRAINT pk_hosted_reporting_hour_marker PRIMARY KEY,
 interval_id bigint NOT NULL,
 bucket timestamptz NOT NULL CONSTRAINT ck_hosted_reporting_hour_marker_bucket CHECK(time_bucket(INTERVAL '1 hour',bucket)=bucket),
 enqueued_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
-- Lookup path for the revision claim, which deletes every marker of each claimed interval-hour.
CREATE INDEX idx_hosted_account_reporting_hour_marker_interval_bucket
 ON hosted_account_reporting_hour_marker(interval_id,bucket);

-- Shared per-(interval, hour) aggregate over normalized samples with time in
-- [from_bucket, to_bucket); pass hour-aligned bounds. A NULL interval_ids
-- summarizes every interval. Only interval-hours with samples are returned.
-- Expressions mirror hosted_account_history_hourly (which cannot call this
-- function); sums are ordered and latest values break time ties on
-- (origin_collected_at, origin_key, origin_time, source_id), so identical
-- samples always give identical rows.
CREATE FUNCTION hosted_reporting_hour_summary(from_bucket timestamptz,to_bucket timestamptz,interval_ids bigint[] DEFAULT NULL)
RETURNS TABLE(interval_id bigint,bucket timestamptz,
 hashrate_report_count integer,hashrate_count integer,hashrate_positive_count integer,hashrate_sum double precision,
 hashrate_complete boolean,hashrate_source_oldest_at timestamptz,hashrate_source_newest_at timestamptz,
 power_report_count integer,power_count integer,power_sum double precision,
 power_complete boolean,power_source_oldest_at timestamptz,power_source_newest_at timestamptz,
 temperature_report_count integer,temperature_count integer,temperature_sum double precision,
 temperature_latest_value double precision,temperature_latest_at timestamptz,
 temperature_source_oldest_at timestamptz,temperature_source_newest_at timestamptz,
 state_report_count integer,state_count integer,state_latest_value double precision,state_latest_at timestamptz,
 state_source_oldest_at timestamptz,state_source_newest_at timestamptz)
LANGUAGE sql STABLE AS $$
 SELECT s.interval_id,time_bucket(INTERVAL '1 hour',s.time),
  (count(*) FILTER(WHERE s.metric=hosted_history_metric_code('hashrate')))::integer,
  (count(s.value) FILTER(WHERE s.metric=hosted_history_metric_code('hashrate')))::integer,
  (count(s.value) FILTER(WHERE s.metric=hosted_history_metric_code('hashrate') AND s.value>0))::integer,
  sum(s.value ORDER BY s.time,s.source_id,s.origin_key COLLATE "C",s.origin_time,s.origin_collected_at)
   FILTER(WHERE s.metric=hosted_history_metric_code('hashrate')),
  bool_and(s.complete) FILTER(WHERE s.metric=hosted_history_metric_code('hashrate')),
  min(s.source_oldest_at) FILTER(WHERE s.metric=hosted_history_metric_code('hashrate')),
  max(s.source_newest_at) FILTER(WHERE s.metric=hosted_history_metric_code('hashrate')),
  (count(*) FILTER(WHERE s.metric=hosted_history_metric_code('power')))::integer,
  (count(s.value) FILTER(WHERE s.metric=hosted_history_metric_code('power')))::integer,
  sum(s.value ORDER BY s.time,s.source_id,s.origin_key COLLATE "C",s.origin_time,s.origin_collected_at)
   FILTER(WHERE s.metric=hosted_history_metric_code('power')),
  bool_and(s.complete) FILTER(WHERE s.metric=hosted_history_metric_code('power')),
  min(s.source_oldest_at) FILTER(WHERE s.metric=hosted_history_metric_code('power')),
  max(s.source_newest_at) FILTER(WHERE s.metric=hosted_history_metric_code('power')),
  (count(*) FILTER(WHERE s.metric=hosted_history_metric_code('temperature')))::integer,
  (count(s.value) FILTER(WHERE s.metric=hosted_history_metric_code('temperature')))::integer,
  sum(s.value ORDER BY s.time,s.source_id,s.origin_key COLLATE "C",s.origin_time,s.origin_collected_at)
   FILTER(WHERE s.metric=hosted_history_metric_code('temperature')),
  (array_agg(s.value ORDER BY s.time DESC,s.origin_collected_at DESC,s.origin_key COLLATE "C" DESC,s.origin_time DESC,s.source_id DESC)
   FILTER(WHERE s.metric=hosted_history_metric_code('temperature') AND s.value IS NOT NULL))[1],
  max(s.time) FILTER(WHERE s.metric=hosted_history_metric_code('temperature') AND s.value IS NOT NULL),
  min(s.source_oldest_at) FILTER(WHERE s.metric=hosted_history_metric_code('temperature')),
  max(s.source_newest_at) FILTER(WHERE s.metric=hosted_history_metric_code('temperature')),
  (count(*) FILTER(WHERE s.metric=hosted_history_metric_code('state')))::integer,
  (count(s.value) FILTER(WHERE s.metric=hosted_history_metric_code('state')))::integer,
  (array_agg(s.value ORDER BY s.time DESC,s.origin_collected_at DESC,s.origin_key COLLATE "C" DESC,s.origin_time DESC,s.source_id DESC)
   FILTER(WHERE s.metric=hosted_history_metric_code('state') AND s.value IS NOT NULL))[1],
  max(s.time) FILTER(WHERE s.metric=hosted_history_metric_code('state') AND s.value IS NOT NULL),
  min(s.source_oldest_at) FILTER(WHERE s.metric=hosted_history_metric_code('state')),
  max(s.source_newest_at) FILTER(WHERE s.metric=hosted_history_metric_code('state'))
 -- Mutually exclusive branches, so listed interval_ids are an index condition
 -- and a NULL list stays one time-range scan.
 FROM (SELECT * FROM hosted_account_history_sample WHERE interval_ids IS NULL AND time>=from_bucket AND time<to_bucket
  UNION ALL
  SELECT * FROM hosted_account_history_sample WHERE interval_id=ANY(interval_ids) AND time>=from_bucket AND time<to_bucket) s
 GROUP BY s.interval_id,time_bucket(INTERVAL '1 hour',s.time)
$$;

-- First hour still inside the correction window at as_of. Raw samples are kept
-- 10 days; nine days keeps every recomputed hour fully retained and matches the
-- hourly aggregate refresh window. Marker triggers always use the 9-day limit,
-- so a job may shorten its window but never extend it.
CREATE FUNCTION hosted_reporting_hour_window_floor(as_of timestamptz,correction_window interval DEFAULT INTERVAL '9 days')
RETURNS timestamptz LANGUAGE sql STABLE AS $$
 SELECT time_bucket(INTERVAL '1 hour',as_of-LEAST(correction_window,INTERVAL '9 days'))
$$;

-- Expected (kind, source, metric) streams match the 1044/1049 writers: miner
-- metrics/state 1/2, module metrics/state 3/4, hashboard metrics/state 5/6.
CREATE FUNCTION hosted_reporting_expected_streams()
RETURNS TABLE(kind integer,source_id smallint,metric smallint)
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
 SELECT 5,1::smallint,hosted_history_metric_code('hashrate')
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
$$;

-- Candidate evidence for every (interval, hour) in [from_bucket, to_bucket),
-- without revision metadata. A NULL interval_ids gives the closer's driver set:
-- intervals overlapping the hour plus intervals with samples in it. Listed
-- interval_ids are recomputed for every hour, overlapping or not. Silent hours
-- get zero counts and NULL values. no_source_data is true when none of the
-- interval kind's sources delivered any sample at its site, within its
-- organization, in the hour. Intervals without a site form one group per
-- organization.
CREATE FUNCTION hosted_reporting_hour_rows(from_bucket timestamptz,to_bucket timestamptz,interval_ids bigint[] DEFAULT NULL)
RETURNS TABLE(interval_id bigint,bucket timestamptz,no_source_data boolean,window_start timestamptz,window_end timestamptz,
 hashrate_report_count integer,hashrate_count integer,hashrate_positive_count integer,hashrate_sum double precision,
 hashrate_complete boolean,hashrate_source_oldest_at timestamptz,hashrate_source_newest_at timestamptz,
 power_report_count integer,power_count integer,power_sum double precision,
 power_complete boolean,power_source_oldest_at timestamptz,power_source_newest_at timestamptz,
 temperature_report_count integer,temperature_count integer,temperature_sum double precision,
 temperature_latest_value double precision,temperature_latest_at timestamptz,
 temperature_source_oldest_at timestamptz,temperature_source_newest_at timestamptz,
 state_report_count integer,state_count integer,state_latest_value double precision,state_latest_at timestamptz,
 state_source_oldest_at timestamptz,state_source_newest_at timestamptz)
LANGUAGE sql STABLE AS $$
 WITH summary AS MATERIALIZED (SELECT * FROM hosted_reporting_hour_summary(from_bucket,to_bucket,interval_ids)),
 hours AS (SELECT h AS bucket FROM generate_series(from_bucket,to_bucket-INTERVAL '1 hour',INTERVAL '1 hour') h),
 driver AS (
  SELECT i.id AS interval_id,h.bucket FROM hours h JOIN hosted_account_asset_interval i
   ON i.started_at<h.bucket+INTERVAL '1 hour' AND (i.ended_at IS NULL OR i.ended_at>GREATEST(h.bucket,i.started_at))
  WHERE interval_ids IS NULL
  UNION
  SELECT i.id,h.bucket FROM hours h JOIN hosted_account_asset_interval i ON i.id=ANY(interval_ids)
  WHERE interval_ids IS NOT NULL
  UNION
  SELECT s.interval_id,s.bucket FROM summary s
 ),
 framed AS (
  SELECT d.interval_id,d.bucket,i.organization_id AS org,i.kind,COALESCE(i.site_id,0) AS site,
   GREATEST(i.started_at,d.bucket) AS frame_start,
   LEAST(COALESCE(i.ended_at,'infinity'::timestamptz),d.bucket+INTERVAL '1 hour') AS frame_end
  FROM driver d JOIN hosted_account_asset_interval i ON i.id=d.interval_id
 ),
 coverage AS (
  SELECT g.bucket,g.org,g.site,g.kind,EXISTS(SELECT 1 FROM hosted_account_history_sample cs
   JOIN hosted_account_asset_interval ci ON ci.id=cs.interval_id
   WHERE cs.time>=g.bucket AND cs.time<g.bucket+INTERVAL '1 hour'
    AND cs.source_id=ANY(ARRAY(SELECT e.source_id FROM hosted_reporting_expected_streams() e WHERE e.kind=g.kind))
    AND ci.organization_id=g.org AND COALESCE(ci.site_id,0)=g.site) AS reported
  FROM (SELECT DISTINCT f.bucket,f.org,f.site,f.kind FROM framed f) g
 )
 SELECT f.interval_id,f.bucket,NOT c.reported,
  CASE WHEN f.frame_start<f.frame_end THEN f.frame_start END,
  CASE WHEN f.frame_start<f.frame_end THEN f.frame_end END,
  COALESCE(s.hashrate_report_count,0),COALESCE(s.hashrate_count,0),COALESCE(s.hashrate_positive_count,0),s.hashrate_sum,
  s.hashrate_complete,s.hashrate_source_oldest_at,s.hashrate_source_newest_at,
  COALESCE(s.power_report_count,0),COALESCE(s.power_count,0),s.power_sum,
  s.power_complete,s.power_source_oldest_at,s.power_source_newest_at,
  COALESCE(s.temperature_report_count,0),COALESCE(s.temperature_count,0),s.temperature_sum,
  s.temperature_latest_value,s.temperature_latest_at,s.temperature_source_oldest_at,s.temperature_source_newest_at,
  COALESCE(s.state_report_count,0),COALESCE(s.state_count,0),s.state_latest_value,s.state_latest_at,
  s.state_source_oldest_at,s.state_source_newest_at
 FROM framed f JOIN coverage c ON c.bucket=f.bucket AND c.org=f.org AND c.site=f.site AND c.kind=f.kind
 LEFT JOIN summary s ON s.interval_id=f.interval_id AND s.bucket=f.bucket
$$;

-- Correction markers. Writers only enqueue; the job owns every evidence write.
-- A marker is needed for hours that are closing or closed (bucket <
-- closing_through), inside the hold and inside the correction window. Every
-- enqueue inserts its own marker: there is no shared row for writers to queue on.
CREATE FUNCTION enqueue_hosted_reporting_hour_sample() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
 closing timestamptz;
 lower_at timestamptz;
 new_hour timestamptz;
 old_hour timestamptz;
BEGIN
 -- This runs after the row write took the sample-table lock, so it sees any
 -- closing_through that a finished phase 1 committed.
 SELECT c.closing_through,GREATEST(c.hold_started_at,hosted_reporting_hour_window_floor(now()))
 INTO closing,lower_at FROM hosted_account_reporting_hour_control c WHERE c.singleton;
 IF TG_OP<>'DELETE' THEN
  new_hour := time_bucket(INTERVAL '1 hour',NEW.time);
  IF new_hour<closing AND new_hour>=lower_at THEN
   INSERT INTO hosted_account_reporting_hour_marker(interval_id,bucket) VALUES(NEW.interval_id,new_hour);
  END IF;
 END IF;
 IF TG_OP<>'INSERT' THEN
  old_hour := time_bucket(INTERVAL '1 hour',OLD.time);
  IF old_hour<closing AND old_hour>=lower_at
   AND (TG_OP='DELETE' OR old_hour IS DISTINCT FROM new_hour OR OLD.interval_id IS DISTINCT FROM NEW.interval_id) THEN
   INSERT INTO hosted_account_reporting_hour_marker(interval_id,bucket) VALUES(OLD.interval_id,old_hour);
  END IF;
 END IF;
 RETURN NULL;
END;
$$;

-- Interval bound changes alter the capture window of the hours between the old
-- and new bound. Open ends are clamped to closing_through and starts to the hold
-- and correction window, so no series is ever unbounded.
CREATE FUNCTION enqueue_hosted_reporting_hour_interval() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
 closing timestamptz;
 lower_at timestamptz;
BEGIN
 -- This runs after the row write took the interval-table lock, so it sees any
 -- closing_through that a finished phase 1 committed.
 SELECT c.closing_through,GREATEST(c.hold_started_at,hosted_reporting_hour_window_floor(now()))
 INTO closing,lower_at FROM hosted_account_reporting_hour_control c WHERE c.singleton;
 IF lower_at>=closing THEN RETURN NULL; END IF;
 INSERT INTO hosted_account_reporting_hour_marker(interval_id,bucket)
 SELECT DISTINCT NEW.id,h FROM (VALUES
  (TG_OP='INSERT',NEW.started_at,COALESCE(NEW.ended_at,'infinity'::timestamptz)),
  (TG_OP='UPDATE',OLD.started_at,NEW.started_at),
  (TG_OP='UPDATE',COALESCE(OLD.ended_at,'infinity'::timestamptz),COALESCE(NEW.ended_at,'infinity'::timestamptz))
 ) bound(applies,a,z)
 CROSS JOIN LATERAL generate_series(time_bucket(INTERVAL '1 hour',GREATEST(LEAST(bound.a,bound.z),lower_at)),
  LEAST(GREATEST(bound.a,bound.z),closing)-INTERVAL '1 microsecond',INTERVAL '1 hour') h
 WHERE bound.applies AND bound.a IS DISTINCT FROM bound.z;
 RETURN NULL;
END;
$$;

-- Postgres rejects OLD/NEW in the WHEN clause of a trigger that also fires on
-- INSERT or DELETE, so unchanged upserts are filtered by a separate trigger.
CREATE TRIGGER enqueue_hosted_reporting_hour_sample
 AFTER INSERT OR DELETE ON hosted_account_history_sample
 FOR EACH ROW EXECUTE FUNCTION enqueue_hosted_reporting_hour_sample();
CREATE TRIGGER enqueue_hosted_reporting_hour_sample_update
 AFTER UPDATE ON hosted_account_history_sample
 FOR EACH ROW WHEN (OLD.* IS DISTINCT FROM NEW.*) EXECUTE FUNCTION enqueue_hosted_reporting_hour_sample();
CREATE TRIGGER enqueue_hosted_reporting_hour_interval
 AFTER INSERT OR UPDATE OF started_at,ended_at ON hosted_account_asset_interval
 FOR EACH ROW EXECUTE FUNCTION enqueue_hosted_reporting_hour_interval();

-- Phase 1: publish the closing boundary. The sample- and interval-table locks
-- wait out every in-flight sample and allocation writer, so writers that commit
-- later see the new boundary and leave markers. Both enqueue triggers rely on
-- this fence. The sample table goes first: telemetry batches, the only writers
-- of both tables, usually write samples before an interval, and phase 1 then
-- holds nothing while it waits out telemetry. Hours that left the correction
-- window before they could be closed get a gap row instead of evidence. Returns
-- closing_through.
CREATE FUNCTION hosted_reporting_hour_begin_close(as_of timestamptz,grace interval DEFAULT INTERVAL '30 minutes',
 correction_window interval DEFAULT INTERVAL '9 days',max_hours integer DEFAULT 24,lock_timeout_ms integer DEFAULT 1000)
RETURNS timestamptz LANGUAGE plpgsql AS $$
DECLARE
 control hosted_account_reporting_hour_control;
 effective_at timestamptz := LEAST(as_of,clock_timestamp());
 expired_before timestamptz;
 target timestamptz;
 lock_started timestamptz;
BEGIN
 IF grace IS NULL OR grace<INTERVAL '0' THEN RAISE EXCEPTION 'hourly evidence grace must not be negative, got %',grace; END IF;
 IF correction_window IS NULL OR correction_window<=INTERVAL '0' OR correction_window>INTERVAL '9 days' THEN
  RAISE EXCEPTION 'hourly evidence correction window must be positive and at most 9 days, got %',correction_window;
 END IF;
 IF max_hours IS NULL OR max_hours<1 THEN RAISE EXCEPTION 'hourly evidence max_hours must be positive, got %',max_hours; END IF;
 IF lock_timeout_ms IS NULL OR lock_timeout_ms<1 THEN RAISE EXCEPTION 'hourly evidence lock_timeout_ms must be positive, got %',lock_timeout_ms; END IF;
 SELECT * INTO STRICT control FROM hosted_account_reporting_hour_control WHERE singleton FOR UPDATE;
 expired_before := hosted_reporting_hour_window_floor(effective_at,correction_window);
 IF control.closed_through<expired_before THEN
  INSERT INTO hosted_account_reporting_hour_gap(from_bucket,to_bucket,reason)
  VALUES(control.closed_through,expired_before,'closer fell behind past the correction window');
  UPDATE hosted_account_reporting_hour_control
  SET closed_through=expired_before,closing_through=GREATEST(closing_through,expired_before)
  WHERE singleton RETURNING * INTO STRICT control;
 END IF;
 target := LEAST(time_bucket(INTERVAL '1 hour',effective_at-grace),control.closed_through+max_hours*INTERVAL '1 hour');
 IF target<=control.closing_through THEN RETURN control.closing_through; END IF;
 -- lock_timeout applies to each lock wait, so the second lock gets what is
 -- left of the budget: writers queue behind phase 1 for lock_timeout_ms at most.
 lock_started := clock_timestamp();
 PERFORM set_config('lock_timeout',lock_timeout_ms::text||'ms',true);
 LOCK TABLE hosted_account_history_sample IN SHARE ROW EXCLUSIVE MODE;
 PERFORM set_config('lock_timeout',GREATEST(1,lock_timeout_ms
  -ceil(extract(epoch FROM clock_timestamp()-lock_started)*1000)::integer)::text||'ms',true);
 LOCK TABLE hosted_account_asset_interval IN SHARE ROW EXCLUSIVE MODE;
 UPDATE hosted_account_reporting_hour_control SET closing_through=target WHERE singleton;
 RETURN target;
END;
$$;

-- Phase 2: write revision 1 for every interval-hour in [closed_through,
-- closing_through). The marker claim is its own statement: every marker it
-- deletes belongs to a committed write that the later summary statement sees.
-- Markers of writers still in flight survive for the revision step. Returns
-- the number of evidence rows written.
CREATE FUNCTION hosted_reporting_hour_finish_close(as_of timestamptz,correction_window interval DEFAULT INTERVAL '9 days')
RETURNS integer LANGUAGE plpgsql AS $$
DECLARE
 control hosted_account_reporting_hour_control;
 written integer := 0;
BEGIN
 IF correction_window IS NULL OR correction_window<=INTERVAL '0' OR correction_window>INTERVAL '9 days' THEN
  RAISE EXCEPTION 'hourly evidence correction window must be positive and at most 9 days, got %',correction_window;
 END IF;
 SELECT * INTO STRICT control FROM hosted_account_reporting_hour_control WHERE singleton FOR UPDATE;
 IF control.closed_through<control.closing_through THEN
  IF control.closed_through<hosted_reporting_hour_window_floor(LEAST(as_of,clock_timestamp()),correction_window) THEN
   RAISE EXCEPTION 'hour % has left the correction window; run hosted_reporting_hour_begin_close to record the gap',control.closed_through;
  END IF;
  DELETE FROM hosted_account_reporting_hour_marker m
  WHERE m.bucket>=control.closed_through AND m.bucket<control.closing_through;
  WITH candidate AS MATERIALIZED (
   SELECT * FROM hosted_reporting_hour_rows(control.closed_through,control.closing_through)
  ), snapshot AS (
   INSERT INTO hosted_account_reporting_allocation_snapshot(interval_id,organization_id,client_id,subclient_id,
    site_id,kind,asset_id,device_id,device_identifier,started_at)
   SELECT i.id,i.organization_id,i.client_id,i.subclient_id,i.site_id,i.kind,i.asset_id,i.device_id,i.device_identifier,i.started_at
   FROM hosted_account_asset_interval i WHERE i.id IN (SELECT c.interval_id FROM candidate c)
   ON CONFLICT(interval_id) DO NOTHING
  )
  INSERT INTO hosted_account_reporting_hour(interval_id,bucket,revision,capture_reason,no_source_data,window_start,window_end,
   hashrate_report_count,hashrate_count,hashrate_positive_count,hashrate_sum,hashrate_complete,hashrate_source_oldest_at,hashrate_source_newest_at,
   power_report_count,power_count,power_sum,power_complete,power_source_oldest_at,power_source_newest_at,
   temperature_report_count,temperature_count,temperature_sum,temperature_latest_value,temperature_latest_at,
   temperature_source_oldest_at,temperature_source_newest_at,
   state_report_count,state_count,state_latest_value,state_latest_at,state_source_oldest_at,state_source_newest_at)
  SELECT c.interval_id,c.bucket,1,'closed',c.no_source_data,c.window_start,c.window_end,
   c.hashrate_report_count,c.hashrate_count,c.hashrate_positive_count,c.hashrate_sum,c.hashrate_complete,c.hashrate_source_oldest_at,c.hashrate_source_newest_at,
   c.power_report_count,c.power_count,c.power_sum,c.power_complete,c.power_source_oldest_at,c.power_source_newest_at,
   c.temperature_report_count,c.temperature_count,c.temperature_sum,c.temperature_latest_value,c.temperature_latest_at,
   c.temperature_source_oldest_at,c.temperature_source_newest_at,
   c.state_report_count,c.state_count,c.state_latest_value,c.state_latest_at,c.state_source_oldest_at,c.state_source_newest_at
  FROM candidate c;
  GET DIAGNOSTICS written=ROW_COUNT;
 END IF;
 UPDATE hosted_account_reporting_hour_control SET closed_through=closing_through,last_success_at=clock_timestamp() WHERE singleton;
 RETURN written;
END;
$$;

-- Revision step: recompute up to batch_size marked closed interval-hours and append
-- revision N+1 only where the full row differs from the latest revision. An
-- interval-hour without a prior revision (a backdated or late-created interval)
-- gets revision 1 marked unattributable, because samples are attributed only at
-- ingest. When a recompute flips an hour's site source coverage, the other
-- intervals of that organization, kind and site are recomputed too, so their no_source_data
-- flag follows. Markers older than the window, before the hold or under a gap
-- are discarded. Returns the number of revisions appended.
CREATE FUNCTION hosted_reporting_hour_revise(as_of timestamptz,correction_window interval DEFAULT INTERVAL '9 days',
 batch_size integer DEFAULT 10000)
RETURNS integer LANGUAGE plpgsql AS $$
DECLARE
 control hosted_account_reporting_hour_control;
 expired_before timestamptz;
 claimed_ids bigint[];
 claimed_buckets timestamptz[];
 written integer;
BEGIN
 IF correction_window IS NULL OR correction_window<=INTERVAL '0' OR correction_window>INTERVAL '9 days' THEN
  RAISE EXCEPTION 'hourly evidence correction window must be positive and at most 9 days, got %',correction_window;
 END IF;
 IF batch_size IS NULL OR batch_size<1 THEN RAISE EXCEPTION 'hourly evidence revision batch must be positive, got %',batch_size; END IF;
 SELECT * INTO STRICT control FROM hosted_account_reporting_hour_control WHERE singleton FOR UPDATE;
 expired_before := GREATEST(control.hold_started_at,
  hosted_reporting_hour_window_floor(LEAST(as_of,clock_timestamp()),correction_window));
 -- Standalone claim; the recompute below runs with a later snapshot. The batch
 -- counts interval-hours, oldest marker first, and takes every visible marker
 -- of each: one corrected hour can carry thousands of duplicate markers.
 WITH batch AS (
  SELECT q.interval_id,q.bucket FROM hosted_account_reporting_hour_marker q
  WHERE q.bucket<control.closed_through
  GROUP BY q.interval_id,q.bucket ORDER BY min(q.marker_id) LIMIT batch_size
 ), claimed AS (
  DELETE FROM hosted_account_reporting_hour_marker m USING batch b
  WHERE m.interval_id=b.interval_id AND m.bucket=b.bucket
  RETURNING m.interval_id,m.bucket
 )
 SELECT array_agg(c.interval_id),array_agg(c.bucket) INTO claimed_ids,claimed_buckets
 FROM (SELECT DISTINCT claimed.interval_id,claimed.bucket FROM claimed) c
 WHERE c.bucket>=expired_before
  AND NOT EXISTS(SELECT 1 FROM hosted_account_reporting_hour_gap g WHERE c.bucket>=g.from_bucket AND c.bucket<g.to_bucket);
 IF claimed_ids IS NULL THEN RETURN 0; END IF;
 WITH claimed AS (
  SELECT p.interval_id,p.bucket FROM unnest(claimed_ids,claimed_buckets) p(interval_id,bucket)
 ), direct AS MATERIALIZED (
  SELECT r.* FROM (SELECT claimed.bucket,array_agg(claimed.interval_id) AS ids FROM claimed GROUP BY claimed.bucket) b
  CROSS JOIN LATERAL hosted_reporting_hour_rows(b.bucket,b.bucket+INTERVAL '1 hour',b.ids) r
 ), coverage AS (
  SELECT DISTINCT d.bucket,i.organization_id AS org,i.kind,COALESCE(i.site_id,0) AS site,d.no_source_data
  FROM direct d JOIN hosted_account_asset_interval i ON i.id=d.interval_id
 ), stale AS (
  SELECT g.bucket,array_agg(i.id) AS ids FROM coverage g
  JOIN hosted_account_asset_interval i ON i.organization_id=g.org AND i.kind=g.kind AND COALESCE(i.site_id,0)=g.site
  CROSS JOIN LATERAL (SELECT h.no_source_data FROM hosted_account_reporting_hour h
   WHERE h.interval_id=i.id AND h.bucket=g.bucket ORDER BY h.revision DESC LIMIT 1) l
  WHERE l.no_source_data IS DISTINCT FROM g.no_source_data
   AND NOT EXISTS(SELECT 1 FROM claimed c WHERE c.interval_id=i.id AND c.bucket=g.bucket)
  GROUP BY g.bucket
 ), candidate AS (
  SELECT * FROM direct
  UNION ALL
  SELECT r.* FROM stale s CROSS JOIN LATERAL hosted_reporting_hour_rows(s.bucket,s.bucket+INTERVAL '1 hour',s.ids) r
 ), changed AS MATERIALIZED (
  SELECT c.*,COALESCE(l.revision,0)+1 AS revision,COALESCE(l.capture_reason,'unattributable') AS capture_reason
  FROM candidate c LEFT JOIN LATERAL (SELECT h.* FROM hosted_account_reporting_hour h
   WHERE h.interval_id=c.interval_id AND h.bucket=c.bucket ORDER BY h.revision DESC LIMIT 1) l ON true
  WHERE CASE WHEN l.revision IS NULL
   THEN c.window_start IS NOT NULL OR c.hashrate_report_count+c.power_report_count+c.temperature_report_count+c.state_report_count>0
   ELSE ROW(c.no_source_data,c.window_start,c.window_end,
     c.hashrate_report_count,c.hashrate_count,c.hashrate_positive_count,c.hashrate_sum,c.hashrate_complete,c.hashrate_source_oldest_at,c.hashrate_source_newest_at,
     c.power_report_count,c.power_count,c.power_sum,c.power_complete,c.power_source_oldest_at,c.power_source_newest_at,
     c.temperature_report_count,c.temperature_count,c.temperature_sum,c.temperature_latest_value,c.temperature_latest_at,
     c.temperature_source_oldest_at,c.temperature_source_newest_at,
     c.state_report_count,c.state_count,c.state_latest_value,c.state_latest_at,c.state_source_oldest_at,c.state_source_newest_at)
    IS DISTINCT FROM ROW(l.no_source_data,l.window_start,l.window_end,
     l.hashrate_report_count,l.hashrate_count,l.hashrate_positive_count,l.hashrate_sum,l.hashrate_complete,l.hashrate_source_oldest_at,l.hashrate_source_newest_at,
     l.power_report_count,l.power_count,l.power_sum,l.power_complete,l.power_source_oldest_at,l.power_source_newest_at,
     l.temperature_report_count,l.temperature_count,l.temperature_sum,l.temperature_latest_value,l.temperature_latest_at,
     l.temperature_source_oldest_at,l.temperature_source_newest_at,
     l.state_report_count,l.state_count,l.state_latest_value,l.state_latest_at,l.state_source_oldest_at,l.state_source_newest_at)
   END
 ), snapshot AS (
  INSERT INTO hosted_account_reporting_allocation_snapshot(interval_id,organization_id,client_id,subclient_id,
   site_id,kind,asset_id,device_id,device_identifier,started_at)
  SELECT i.id,i.organization_id,i.client_id,i.subclient_id,i.site_id,i.kind,i.asset_id,i.device_id,i.device_identifier,i.started_at
  FROM hosted_account_asset_interval i WHERE i.id IN (SELECT c.interval_id FROM changed c)
  ON CONFLICT(interval_id) DO NOTHING
 )
 INSERT INTO hosted_account_reporting_hour(interval_id,bucket,revision,capture_reason,no_source_data,window_start,window_end,
  hashrate_report_count,hashrate_count,hashrate_positive_count,hashrate_sum,hashrate_complete,hashrate_source_oldest_at,hashrate_source_newest_at,
  power_report_count,power_count,power_sum,power_complete,power_source_oldest_at,power_source_newest_at,
  temperature_report_count,temperature_count,temperature_sum,temperature_latest_value,temperature_latest_at,
  temperature_source_oldest_at,temperature_source_newest_at,
  state_report_count,state_count,state_latest_value,state_latest_at,state_source_oldest_at,state_source_newest_at)
 SELECT c.interval_id,c.bucket,c.revision,c.capture_reason,c.no_source_data,c.window_start,c.window_end,
  c.hashrate_report_count,c.hashrate_count,c.hashrate_positive_count,c.hashrate_sum,c.hashrate_complete,c.hashrate_source_oldest_at,c.hashrate_source_newest_at,
  c.power_report_count,c.power_count,c.power_sum,c.power_complete,c.power_source_oldest_at,c.power_source_newest_at,
  c.temperature_report_count,c.temperature_count,c.temperature_sum,c.temperature_latest_value,c.temperature_latest_at,
  c.temperature_source_oldest_at,c.temperature_source_newest_at,
  c.state_report_count,c.state_count,c.state_latest_value,c.state_latest_at,c.state_source_oldest_at,c.state_source_newest_at
 FROM changed c;
 GET DIAGNOSTICS written=ROW_COUNT;
 RETURN written;
END;
$$;

-- Scheduled wrapper. Each phase commits on its own, so phase 1's table locks
-- are released before any aggregation, and a failed phase 2 leaves
-- closed_through behind closing_through for the next run to resume. There is no
-- exception handler: failures reach timescaledb_information.job_errors.
CREATE PROCEDURE hosted_reporting_hour_job(job_id integer,config jsonb)
LANGUAGE plpgsql AS $$
DECLARE
 as_of timestamptz := clock_timestamp();
 grace interval := COALESCE((config->>'grace')::interval,INTERVAL '30 minutes');
 correction_window interval := COALESCE((config->>'correction_window')::interval,INTERVAL '9 days');
 max_hours integer := COALESCE((config->>'max_hours')::integer,24);
 lock_timeout_ms integer := COALESCE((config->>'lock_timeout_ms')::integer,1000);
 revision_batch integer := COALESCE((config->>'revision_batch')::integer,10000);
BEGIN
 IF NOT COALESCE((config->>'enabled')::boolean,false) THEN RETURN; END IF;
 PERFORM hosted_reporting_hour_begin_close(as_of,grace,correction_window,max_hours,lock_timeout_ms);
 COMMIT;
 PERFORM hosted_reporting_hour_finish_close(as_of,correction_window);
 COMMIT;
 PERFORM hosted_reporting_hour_revise(as_of,correction_window,revision_batch);
 COMMIT;
END;
$$;
SELECT add_job('hosted_reporting_hour_job',INTERVAL '15 minutes',
 config=>'{"enabled":true,"grace":"30 minutes","correction_window":"9 days","max_hours":24,"lock_timeout_ms":1000,"revision_batch":10000}'::jsonb,
 fixed_schedule=>false);
SELECT alter_job(job_id,max_runtime=>INTERVAL '10 minutes',retry_period=>INTERVAL '5 minutes')
FROM timescaledb_information.jobs WHERE proc_name='hosted_reporting_hour_job';

-- Capture status: one row per expected (interval, source, metric) stream of
-- open intervals and intervals that ended inside the correction window.
-- closing_through and closed_through are exclusive hour ends. Observation
-- fields come from the latest revision of the most recent hour inside the
-- correction window whose latest revision has a value for the stream's metric,
-- so revising an older hour never moves stream freshness and the lookup does
-- not grow with retained evidence. missing_hours counts hours in
-- [max(hold_started_at, window floor), closed_through) that overlap the
-- interval and have neither a revision nor a covering gap. Gap hours and the
-- latest gap cover the whole interval regardless of age and are not missing.
CREATE VIEW hosted_account_reporting_capture_status AS
SELECT i.id AS interval_id,i.organization_id,i.client_id,i.subclient_id,i.site_id,i.kind,
 i.asset_id,i.device_id,i.device_identifier,i.started_at AS allocation_started_at,i.ended_at AS allocation_ended_at,
 control.hold_started_at,control.closing_through,control.closed_through,control.last_success_at AS closer_last_success_at,
 extract(epoch FROM now()-control.closed_through)::double precision AS closer_lag_seconds,
 stream.source_id,stream.metric,
 (observation.bucket IS NOT NULL)::boolean AS has_observation,
 observation.bucket AS last_observation_bucket,observation.revision AS last_observation_revision,
 observation.captured_at AS last_observation_captured_at,
 observation.source_newest_at AS last_observation_source_newest_at,
 closed.bucket AS last_closed_bucket,closed.revision AS last_closed_revision,
 closed.no_source_data AS last_closed_no_source_data,
 missing.hours AS missing_hours,gap.hours AS gap_hours,
 latest_gap.from_bucket AS last_gap_from_bucket,latest_gap.to_bucket AS last_gap_to_bucket,
 latest_gap.reason AS last_gap_reason,latest_gap.recorded_at AS last_gap_recorded_at
FROM hosted_account_asset_interval i
CROSS JOIN hosted_account_reporting_hour_control control
-- Hours the interval overlaps: [first_hour, end_hour), matching the closer's driver set.
CROSS JOIN LATERAL (
 SELECT time_bucket(INTERVAL '1 hour',i.started_at) AS first_hour,
  CASE WHEN i.ended_at IS NULL THEN 'infinity'::timestamptz
   WHEN i.ended_at<=i.started_at THEN time_bucket(INTERVAL '1 hour',i.started_at)
   ELSE time_bucket(INTERVAL '1 hour',i.ended_at-INTERVAL '1 microsecond')+INTERVAL '1 hour' END AS end_hour
) span
-- Hours checked for missing evidence: [from_hour, to_hour).
CROSS JOIN LATERAL (
 SELECT GREATEST(control.hold_started_at,hosted_reporting_hour_window_floor(now()),span.first_hour) AS from_hour,
  LEAST(control.closed_through,span.end_hour) AS to_hour
) recent
LEFT JOIN LATERAL (
 SELECT h.bucket,h.revision,h.no_source_data FROM hosted_account_reporting_hour h
 WHERE h.interval_id=i.id ORDER BY h.bucket DESC,h.revision DESC LIMIT 1
) closed ON true
-- EXCEPT, not an anti-join: the planner has picked a nested loop for the
-- anti-join, comparing every window hour with every hour row of the interval.
-- The bucket range keeps the hour-row scan inside the window, so its cost
-- does not grow with the interval's retained evidence.
CROSS JOIN LATERAL (
 SELECT count(*)::integer AS hours FROM (
  SELECT m.bucket FROM generate_series(recent.from_hour,recent.to_hour-INTERVAL '1 hour',INTERVAL '1 hour') m(bucket)
  EXCEPT SELECT h.bucket FROM hosted_account_reporting_hour h
  WHERE h.interval_id=i.id AND h.bucket>=recent.from_hour AND h.bucket<recent.to_hour
 ) m
 WHERE NOT EXISTS(SELECT 1 FROM hosted_account_reporting_hour_gap g WHERE m.bucket>=g.from_bucket AND m.bucket<g.to_bucket)
) missing
CROSS JOIN LATERAL (
 SELECT COALESCE(sum(extract(epoch FROM LEAST(g.to_bucket,span.end_hour)-GREATEST(g.from_bucket,span.first_hour))/3600),0)::integer AS hours
 FROM hosted_account_reporting_hour_gap g WHERE g.from_bucket<span.end_hour AND g.to_bucket>span.first_hour
) gap
LEFT JOIN LATERAL (
 SELECT g.from_bucket,g.to_bucket,g.reason,g.recorded_at FROM hosted_account_reporting_hour_gap g
 WHERE g.from_bucket<span.end_hour AND g.to_bucket>span.first_hour
 ORDER BY g.to_bucket DESC,g.gap_id DESC LIMIT 1
) latest_gap ON true
-- Per-stream lookups follow the per-interval ones, which run once per interval.
-- sqlc cannot see the function's result columns: name them, and the no-op
-- casts give it their types.
JOIN (SELECT e.kind,e.source_id::smallint AS source_id,e.metric::smallint AS metric
 FROM hosted_reporting_expected_streams() e(kind,source_id,metric)) stream ON stream.kind=i.kind
LEFT JOIN LATERAL (
 SELECT h.bucket,h.revision,h.captured_at,
  CASE stream.metric WHEN hosted_history_metric_code('hashrate') THEN h.hashrate_source_newest_at
   WHEN hosted_history_metric_code('power') THEN h.power_source_newest_at
   WHEN hosted_history_metric_code('temperature') THEN h.temperature_source_newest_at
   ELSE h.state_source_newest_at END AS source_newest_at
 FROM hosted_account_reporting_hour h
 WHERE h.interval_id=i.id AND h.bucket>=hosted_reporting_hour_window_floor(now())
  AND CASE stream.metric WHEN hosted_history_metric_code('hashrate') THEN h.hashrate_count
   WHEN hosted_history_metric_code('power') THEN h.power_count
   WHEN hosted_history_metric_code('temperature') THEN h.temperature_count
   ELSE h.state_count END>0
  AND NOT EXISTS(SELECT 1 FROM hosted_account_reporting_hour n
   WHERE n.interval_id=h.interval_id AND n.bucket=h.bucket AND n.revision>h.revision)
 ORDER BY h.bucket DESC LIMIT 1
) observation ON true
WHERE i.ended_at IS NULL OR i.ended_at>hosted_reporting_hour_window_floor(now());
