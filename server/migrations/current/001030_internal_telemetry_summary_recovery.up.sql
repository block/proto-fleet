-- requires-shared: 1000
-- Replace independent refresh/expiration with restartable coverage and a shared
-- raw-source fence. Stop writers and database maintenance during this upgrade.
SELECT delete_job(job_id) FROM timescaledb_information.jobs
WHERE proc_name IN ('initialize_container_metric_summaries','initialize_hashboard_metric_summaries');
DROP PROCEDURE initialize_container_metric_summaries(integer,jsonb);
DROP PROCEDURE initialize_hashboard_metric_summaries(integer,jsonb);

SELECT remove_continuous_aggregate_policy(aggregate_name::regclass,if_exists=>true)
FROM unnest(ARRAY[
 'device_metrics_hourly','device_metrics_daily','device_status_hourly','device_status_daily',
 'container_metrics_hourly','container_metrics_daily','container_tank_metrics_hourly','container_tank_metrics_daily',
 'container_module_metrics_hourly','container_module_metrics_daily','hashboard_metrics_hourly','hashboard_metrics_daily'
]) aggregate_name;

CREATE TABLE telemetry_summary_source (
 source_name text PRIMARY KEY,
 raw_floor timestamptz,
 dropped_through timestamptz,
 raw_retention interval NOT NULL DEFAULT INTERVAL '240 hours',
 job_id integer CONSTRAINT uq_telemetry_summary_source_job UNIQUE,
 next_retention_at timestamptz NOT NULL DEFAULT now(),
 last_attempt_at timestamptz,
 last_retention_at timestamptz,
 retention_error text,
 retention_deferrals bigint NOT NULL DEFAULT 0
);
INSERT INTO telemetry_summary_source(source_name)
VALUES ('device_metrics'),('container_tank_metrics'),('container_module_metrics'),('hashboard_metrics');

-- An old chunk can have been recreated by a late observation after expiration.
-- Capture the old policy BEFORE removing it, and trust only its retained window,
-- capped by the repository's shortest prior raw target. Missing policy/history
-- (including an unfinished bootstrap) cannot prove older raw complete either.
-- Empty sources still start at this UTC day so their first live day is covered.
-- The extension catalog is unavailable to sqlc's offline schema parser.
DO $$ BEGIN
 UPDATE telemetry_summary_source s SET raw_floor=f.initial_floor,dropped_through=f.initial_floor
 FROM (
  SELECT s.source_name,CASE WHEN min(c.range_start) IS NULL THEN time_bucket(INTERVAL '24 hours',now())
   ELSE GREATEST(min(c.range_start),now()-LEAST(s.raw_retention,
    COALESCE(min(CASE WHEN j.job_id IS NOT NULL THEN
     COALESCE((j.config->>'drop_after')::interval,INTERVAL '0') END),s.raw_retention))) END AS initial_floor
  FROM telemetry_summary_source s LEFT JOIN timescaledb_information.chunks c
   ON c.hypertable_schema='public' AND c.hypertable_name=s.source_name
  LEFT JOIN timescaledb_information.jobs j ON j.hypertable_schema='public'
   AND j.hypertable_name=s.source_name AND j.proc_name='policy_retention'
  GROUP BY s.source_name,s.raw_retention
 ) f WHERE s.source_name=f.source_name;
END $$;
SELECT remove_retention_policy(source_name::regclass,if_exists=>true)
FROM telemetry_summary_source;

-- Writers hold a shared row lock through their transaction. Before certifying
-- a range, maintenance publishes its write fence and commits, waiting for all
-- earlier writers. Keep this row separate from retention/job metadata: holding
-- it while acquiring raw-table DDL locks would invert the writer's lock order.
CREATE TABLE telemetry_summary_write_gate (
 source_name text PRIMARY KEY REFERENCES telemetry_summary_source(source_name),
 frozen_through timestamptz
);
INSERT INTO telemetry_summary_write_gate(source_name) SELECT source_name FROM telemetry_summary_source;

CREATE FUNCTION telemetry_bucket_ceiling(width interval,at_time timestamptz) RETURNS timestamptz
LANGUAGE sql IMMUTABLE STRICT AS $$
 SELECT boundary+CASE WHEN boundary<at_time THEN width ELSE INTERVAL '0' END
 FROM (SELECT time_bucket(width,at_time) AS boundary) bucket;
$$;

CREATE TABLE telemetry_summary_progress (
 aggregate_name text PRIMARY KEY,
 source_name text NOT NULL REFERENCES telemetry_summary_source(source_name),
 bucket_width interval NOT NULL,
 lateness interval NOT NULL,
 end_offset interval NOT NULL,
 coverage_start timestamptz,
 completed_through timestamptz,
 last_success_at timestamptz,
 attempts bigint NOT NULL DEFAULT 0,
 recovered_batches bigint NOT NULL DEFAULT 0,
 CONSTRAINT ck_telemetry_summary_progress_coverage CHECK (completed_through>=coverage_start)
);
INSERT INTO telemetry_summary_progress(aggregate_name,source_name,bucket_width,lateness,end_offset)
SELECT name||'_'||tier,source,width,late,delay FROM (VALUES
 ('device_metrics','device_metrics'),('device_status','device_metrics'),
 ('container_metrics','device_metrics'),('container_tank_metrics','container_tank_metrics'),
 ('container_module_metrics','container_module_metrics'),('hashboard_metrics','hashboard_metrics')
) equipment(name,source) CROSS JOIN (VALUES
 ('hourly',INTERVAL '1 hour',INTERVAL '24 hours',INTERVAL '1 hour'),
 ('daily',INTERVAL '24 hours',INTERVAL '168 hours',INTERVAL '24 hours')
) tiers(tier,width,late,delay);

-- Each source has one canonical background job. Work is independently due so
-- a busy or failing tier cannot monopolize its siblings or recent refreshes.
CREATE TABLE telemetry_summary_work (
 aggregate_name text NOT NULL REFERENCES telemetry_summary_progress(aggregate_name),
 work_kind text NOT NULL CONSTRAINT ck_telemetry_summary_work_kind CHECK (work_kind IN ('catchup','recent')),
 next_run_at timestamptz NOT NULL DEFAULT now(),
 batch_buckets integer NOT NULL CONSTRAINT ck_telemetry_summary_work_batch CHECK (batch_buckets>0),
 replay_until timestamptz,
 pending boolean NOT NULL DEFAULT false,
 failures bigint NOT NULL DEFAULT 0,
 minimum_batch_failures integer NOT NULL DEFAULT 0,
 successful_batches integer NOT NULL DEFAULT 0,
 blocked boolean NOT NULL DEFAULT false,
 last_start timestamptz,
 last_end timestamptz,
 last_error text,
 last_success_at timestamptz,
 CONSTRAINT pk_telemetry_summary_work PRIMARY KEY (aggregate_name,work_kind)
);
INSERT INTO telemetry_summary_work(aggregate_name,work_kind,batch_buckets,next_run_at)
SELECT aggregate_name,kind,CASE WHEN bucket_width=INTERVAL '1 hour' THEN 24 ELSE 1 END,
 now()-CASE WHEN kind='recent' THEN INTERVAL '1 second' ELSE INTERVAL '0' END
FROM telemetry_summary_progress CROSS JOIN (VALUES ('catchup'),('recent')) kinds(kind);

CREATE FUNCTION retain_telemetry_raw(source_key text) RETURNS void
LANGUAGE plpgsql SET lock_timeout='5s' AS $$
DECLARE
 expires_before timestamptz;
 safe_end timestamptz;
 safe_start timestamptz;
 initialized boolean;
BEGIN
 SELECT now()-s.raw_retention,bool_and(p.completed_through IS NOT NULL),min(p.completed_through),max(p.coverage_start)
 INTO STRICT expires_before,initialized,safe_end,safe_start
 FROM telemetry_summary_source s JOIN telemetry_summary_progress p USING (source_name)
 WHERE s.source_name=source_key GROUP BY s.raw_retention;
 IF NOT COALESCE(initialized,false) OR safe_end<expires_before THEN
  UPDATE telemetry_summary_source SET retention_deferrals=retention_deferrals+1 WHERE source_name=source_key;
 END IF;
 IF NOT COALESCE(initialized,false) THEN RETURN; END IF;
 safe_end := LEAST(safe_end,expires_before);
 IF safe_end<=safe_start THEN RETURN; END IF;
 SELECT max(range_end) INTO safe_end FROM (
  SELECT range_end FROM timescaledb_information.chunks
  WHERE hypertable_schema='public' AND hypertable_name=source_key
   AND range_start>=safe_start AND range_end<=safe_end ORDER BY range_end LIMIT 32
 ) due;
 IF safe_end IS NULL THEN RETURN; END IF;
 PERFORM drop_chunks(source_key::regclass,older_than=>safe_end,newer_than=>safe_start);
 UPDATE telemetry_summary_source SET dropped_through=GREATEST(dropped_through,safe_end),last_retention_at=clock_timestamp()
 WHERE source_name=source_key;
END;
$$;
REVOKE ALL ON FUNCTION retain_telemetry_raw(text) FROM PUBLIC;

-- Internal worker step; invoke the canonical job through run_job, not this
-- procedure. Refresh owns transaction boundaries, so exception subtransactions
-- cannot wrap it. A committed pending marker makes failed attempts restartable.
CREATE PROCEDURE telemetry_summary_step(source_key text)
LANGUAGE plpgsql AS $$
DECLARE
 source telemetry_summary_source%ROWTYPE;
 progress telemetry_summary_progress%ROWTYPE;
 work telemetry_summary_work%ROWTYPE;
 start_at timestamptz;
 end_at timestamptz;
 final_target timestamptz;
 recent_target timestamptz;
 cadence interval;
 finalizing boolean;
 refresh_started timestamptz;
 fast_batch boolean;
BEGIN
 SELECT * INTO STRICT source FROM telemetry_summary_source WHERE source_name=source_key;
 UPDATE telemetry_summary_source SET last_attempt_at=clock_timestamp() WHERE source_name=source_key;
 -- A previous worker exited without acknowledging a refresh. Shrink only at
 -- bucket boundaries. At minimum size, stop after three failures and alert.
 UPDATE telemetry_summary_work w SET pending=false,failures=failures+1,
  minimum_batch_failures=minimum_batch_failures+CASE WHEN batch_buckets=1 THEN 1 ELSE 0 END,
  blocked=minimum_batch_failures+CASE WHEN batch_buckets=1 THEN 1 ELSE 0 END>=3,
  batch_buckets=GREATEST(1,batch_buckets/2),successful_batches=0,
  next_run_at=GREATEST(w.next_run_at,clock_timestamp()+INTERVAL '5 minutes'),
  last_error='refresh interrupted before acknowledgement; inspect Timescale job_errors'
 FROM telemetry_summary_progress p WHERE p.aggregate_name=w.aggregate_name AND p.source_name=source_key AND w.pending;

 -- Expiration gets a turn even when refresh work is continuously due.
 IF source.next_retention_at<=now() THEN
  UPDATE telemetry_summary_source SET next_retention_at=clock_timestamp()+INTERVAL '1 hour' WHERE source_name=source_key;
  BEGIN
   PERFORM retain_telemetry_raw(source_key);
   UPDATE telemetry_summary_source SET retention_error=NULL WHERE source_name=source_key;
  EXCEPTION WHEN OTHERS THEN
   UPDATE telemetry_summary_source SET retention_error=SQLERRM,next_retention_at=clock_timestamp()+INTERVAL '5 minutes'
    WHERE source_name=source_key;
  END;
 END IF;
 UPDATE telemetry_summary_progress p SET coverage_start=f.initial_floor,completed_through=f.initial_floor
 FROM (SELECT aggregate_name,telemetry_bucket_ceiling(bucket_width,source.raw_floor) AS initial_floor
  FROM telemetry_summary_progress WHERE source_name=source_key AND coverage_start IS NULL) f
 WHERE p.aggregate_name=f.aggregate_name;
 SELECT w.* INTO work FROM telemetry_summary_work w JOIN telemetry_summary_progress p USING (aggregate_name)
 WHERE p.source_name=source_key AND NOT w.blocked AND w.next_run_at<=now()
 ORDER BY w.next_run_at,w.aggregate_name,w.work_kind LIMIT 1;
 IF NOT FOUND THEN RETURN; END IF;
 SELECT * INTO STRICT progress FROM telemetry_summary_progress WHERE aggregate_name=work.aggregate_name;
 final_target := time_bucket(progress.bucket_width,now()-progress.lateness);
 recent_target := time_bucket(progress.bucket_width,now()-progress.end_offset);
 finalizing := work.work_kind='catchup';
 cadence := CASE WHEN progress.bucket_width=INTERVAL '1 hour' THEN INTERVAL '30 minutes' ELSE INTERVAL '6 hours' END;
 IF finalizing THEN
  start_at := progress.completed_through;
  end_at := LEAST(start_at+progress.bucket_width*work.batch_buckets,final_target);
 ELSE
  -- Replay newest buckets first even during a multi-week cold catch-up.
  -- Coordinated imports can request newer complete buckets through this
  -- cursor, without certifying coverage or advancing the write gate.
  end_at := LEAST(time_bucket(progress.bucket_width,now()),COALESCE(work.replay_until,recent_target));
  start_at := GREATEST(progress.coverage_start,final_target,end_at-progress.bucket_width*work.batch_buckets);
 END IF;
 IF start_at<source.dropped_through THEN
  UPDATE telemetry_summary_work SET blocked=true,pending=false,failures=failures+1,successful_batches=0,
   last_error=format('refresh would cross expired raw data at %s',source.dropped_through)
  WHERE aggregate_name=work.aggregate_name AND work_kind=work.work_kind;
  RETURN;
 END IF;
 IF start_at>=end_at THEN
  UPDATE telemetry_summary_work SET next_run_at=CASE WHEN finalizing THEN
   final_target+progress.bucket_width+progress.lateness ELSE clock_timestamp()+cadence END,replay_until=NULL
  WHERE aggregate_name=work.aggregate_name AND work_kind=work.work_kind;
  RETURN;
 END IF;
 -- Persist retry/backoff before Timescale commits internally. Other tasks keep
 -- their place ahead of this attempt, including after cancellation or restart.
 UPDATE telemetry_summary_work SET pending=true,last_start=start_at,last_end=end_at,
  next_run_at=clock_timestamp()+INTERVAL '5 minutes'
 WHERE aggregate_name=work.aggregate_name AND work_kind=work.work_kind;
 UPDATE telemetry_summary_progress SET attempts=attempts+1 WHERE aggregate_name=work.aggregate_name;
 COMMIT;
 IF finalizing THEN
  -- Retention's raw-table locks and retry bookkeeping committed above.
  -- An in-flight writer holds SHARE here; publishing waits for its commit.
  UPDATE telemetry_summary_write_gate SET frozen_through=GREATEST(frozen_through,end_at)
   WHERE source_name=source_key;
  COMMIT;
 END IF;
 refresh_started := clock_timestamp();
 CALL refresh_continuous_aggregate(progress.aggregate_name::regclass,start_at,end_at,force=>finalizing);
 fast_batch := clock_timestamp()-refresh_started<INTERVAL '1 minute';
 UPDATE telemetry_summary_progress SET
  completed_through=CASE WHEN finalizing THEN end_at ELSE completed_through END,
  last_success_at=clock_timestamp(),recovered_batches=recovered_batches+CASE WHEN finalizing THEN 1 ELSE 0 END
 WHERE aggregate_name=work.aggregate_name;
 -- Three fast successes restore capacity gradually, without growing a batch
 -- that is already taking a minute or more. Never exceed one UTC day.
 UPDATE telemetry_summary_work SET pending=false,last_error=NULL,minimum_batch_failures=0,last_success_at=clock_timestamp(),
  batch_buckets=CASE WHEN fast_batch AND work.successful_batches>=2 THEN
   LEAST(work.batch_buckets*2,CASE WHEN progress.bucket_width=INTERVAL '1 hour' THEN 24 ELSE 1 END) ELSE batch_buckets END,
  successful_batches=CASE WHEN fast_batch AND work.successful_batches<2 THEN successful_batches+1 ELSE 0 END,
  replay_until=CASE WHEN NOT finalizing AND start_at>GREATEST(progress.coverage_start,final_target) THEN start_at END,
  next_run_at=CASE WHEN finalizing AND end_at<final_target THEN clock_timestamp()
   WHEN finalizing THEN final_target+progress.bucket_width+progress.lateness
   WHEN start_at>GREATEST(progress.coverage_start,final_target) THEN clock_timestamp()
   ELSE clock_timestamp()+cadence END
 WHERE aggregate_name=work.aggregate_name AND work_kind=work.work_kind;
 IF finalizing AND end_at>=final_target THEN
  UPDATE telemetry_summary_source SET next_retention_at=LEAST(next_retention_at,clock_timestamp()) WHERE source_name=source_key;
 END IF;
END;
$$;
REVOKE ALL ON PROCEDURE telemetry_summary_step(text) FROM PUBLIC;

CREATE PROCEDURE maintain_telemetry_source(job_id integer,config jsonb)
LANGUAGE plpgsql AS $$
DECLARE
 source telemetry_summary_source%ROWTYPE;
 due timestamptz;
 enabled boolean;
BEGIN
 SELECT * INTO STRICT source FROM telemetry_summary_source WHERE source_name=config->>'source';
 IF job_id IS DISTINCT FROM source.job_id THEN
  RAISE EXCEPTION 'use canonical telemetry job % for %',source.job_id,source.source_name;
 END IF;
 -- Timescale serializes scheduled executions of one job. Foreground run_job
 -- bypasses that scheduler: request the canonical worker and return, so a pooled
 -- or hidden-role client can neither overlap it nor strand a session claim.
 IF NOT EXISTS (SELECT 1 FROM pg_stat_activity a JOIN timescaledb_information.jobs j
  ON j.job_id=source.job_id WHERE a.pid=pg_backend_pid() AND a.backend_type=j.application_name) THEN
  SELECT scheduled INTO STRICT enabled FROM timescaledb_information.jobs j WHERE j.job_id=source.job_id;
  IF NOT enabled THEN
   RAISE EXCEPTION 'telemetry maintenance is paused; explicitly enable job % before requesting it',job_id;
  END IF;
  PERFORM alter_job(job_id,next_start=>clock_timestamp());
  RETURN;
 END IF;
 -- Shared session lock survives refresh commits. The dedicated worker exits
 -- on failure, releasing it. A seeder holds the exclusive counterpart on its
 -- dedicated connection; an idle ordinary run_job caller holds no session lock.
 IF NOT pg_try_advisory_lock_shared(1030,source.source_name::regclass::oid::integer) THEN
  PERFORM alter_job(job_id,next_start=>clock_timestamp()+INTERVAL '30 seconds');
  RETURN;
 END IF;
 CALL telemetry_summary_step(source.source_name);
 -- Publish retention and refresh acknowledgements before an importer can
 -- acquire the exclusive lock and reopen retained history.
 COMMIT;
 PERFORM pg_advisory_unlock_shared(1030,source.source_name::regclass::oid::integer);
 SELECT LEAST(s.next_retention_at,min(w.next_run_at) FILTER (WHERE NOT w.blocked)) INTO due
 FROM telemetry_summary_source s JOIN telemetry_summary_progress p USING (source_name)
 JOIN telemetry_summary_work w USING (aggregate_name) WHERE s.source_name=source.source_name GROUP BY s.next_retention_at;
 -- Explicit next_start survives mark_end; idle jobs sleep until useful work.
 PERFORM alter_job(job_id,next_start=>GREATEST(clock_timestamp()+INTERVAL '10 seconds',due));
END;
$$;

UPDATE telemetry_summary_source s SET job_id=add_job('maintain_telemetry_source',INTERVAL '30 minutes',
 config=>jsonb_build_object('source',s.source_name),fixed_schedule=>false,
 initial_start=>now()+INTERVAL '1 minute'+n.ordinal*INTERVAL '45 seconds')
FROM (SELECT source_name,row_number() OVER (ORDER BY source_name)-1 AS ordinal FROM telemetry_summary_source) n
WHERE s.source_name=n.source_name;
SELECT alter_job(job_id,max_runtime=>INTERVAL '30 minutes',retry_period=>INTERVAL '10 seconds')
FROM telemetry_summary_source;

-- One lookup/lock per statement, with only a cached comparison per row. The
-- shared advisory transaction lock coordinates development imports; the shared
-- gate row lock coordinates normal writers with a finalizing maintenance batch.
CREATE FUNCTION lock_telemetry_summary_writes() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE floor_at timestamptz;
BEGIN
 PERFORM pg_advisory_xact_lock_shared(1030,TG_RELID::integer);
 SELECT frozen_through INTO STRICT floor_at FROM telemetry_summary_write_gate
 WHERE source_name=TG_ARGV[0] FOR SHARE;
 PERFORM set_config('protofleet.'||TG_ARGV[0]||'_write_floor',COALESCE(floor_at::text,'-infinity'),true);
 RETURN NULL;
END;
$$;
CREATE FUNCTION guard_telemetry_summary_time() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE floor_at timestamptz := current_setting('protofleet.'||TG_ARGV[0]||'_write_floor')::timestamptz;
BEGIN
 -- Cached/asynchronous readings and ON CONFLICT duplicates must not abort a
 -- healthy container poll. Updates also cannot remove a certified old row.
 IF NEW.time<floor_at OR (TG_OP='UPDATE' AND OLD.time<floor_at) THEN RETURN NULL; END IF;
 RETURN NEW;
END;
$$;
DO $$ DECLARE source text; BEGIN
 FOR source IN SELECT source_name FROM telemetry_summary_source LOOP
  EXECUTE format('CREATE TRIGGER lock_telemetry_summary_writes BEFORE INSERT OR UPDATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION lock_telemetry_summary_writes(%L)',source,source);
  EXECUTE format('CREATE TRIGGER guard_telemetry_summary_time BEFORE INSERT OR UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION guard_telemetry_summary_time(%L)',source,source);
 END LOOP;
END $$;

-- Development seeding uses an exclusive session maintenance lock. Reopen only
-- complete retained UTC days, invalidating coverage BEFORE modifying any raw.
-- Failure or disconnect leaves recovery able to rebuild the unfinished range.
CREATE FUNCTION prepare_telemetry_seed(requested_start timestamptz,requested_end timestamptz) RETURNS timestamptz
LANGUAGE plpgsql AS $$
DECLARE
 source telemetry_summary_source%ROWTYPE;
 safe_start timestamptz;
 aggregate_name text;
 populated boolean;
 empty_history boolean := true;
BEGIN
 IF requested_start IS NULL OR requested_end IS NULL OR requested_start>=requested_end THEN
  RAISE EXCEPTION 'telemetry seeding requires a nonempty affected range';
 END IF;
 IF NOT EXISTS (SELECT 1 FROM pg_locks WHERE locktype='advisory' AND pid=pg_backend_pid()
  AND classid=1030 AND objid='device_metrics'::regclass::oid AND objsubid=2 AND mode='ExclusiveLock' AND granted) THEN
  RAISE EXCEPTION 'telemetry seeding requires the exclusive maintenance lock';
 END IF;
 SELECT * INTO STRICT source FROM telemetry_summary_source WHERE source_name='device_metrics';
 IF source.last_retention_at IS NULL AND NOT EXISTS (SELECT 1 FROM device_metrics LIMIT 1) THEN
  FOR aggregate_name IN SELECT p.aggregate_name FROM telemetry_summary_progress p WHERE p.source_name=source.source_name LOOP
   EXECUTE format('SELECT EXISTS (SELECT 1 FROM %I LIMIT 1)',aggregate_name) INTO populated;
   empty_history := empty_history AND NOT populated;
  END LOOP;
 ELSE empty_history := false;
 END IF;
 IF empty_history THEN
  -- Proven empty sources can start their first history before migration time.
  safe_start := LEAST(source.raw_floor,time_bucket(INTERVAL '24 hours',requested_start));
  UPDATE telemetry_summary_source SET raw_floor=safe_start,dropped_through=safe_start WHERE source_name=source.source_name;
  UPDATE telemetry_summary_progress SET coverage_start=safe_start,completed_through=safe_start WHERE source_name=source.source_name;
 ELSE
  SELECT GREATEST(source.raw_floor,source.dropped_through,max(coverage_start)) INTO safe_start
  FROM telemetry_summary_progress WHERE source_name=source.source_name;
  safe_start := telemetry_bucket_ceiling(INTERVAL '24 hours',safe_start);
 END IF;
 safe_start := GREATEST(safe_start,time_bucket(INTERVAL '24 hours',requested_start));
 UPDATE telemetry_summary_progress SET
  coverage_start=COALESCE(coverage_start,telemetry_bucket_ceiling(bucket_width,source.raw_floor)),
  completed_through=LEAST(COALESCE(completed_through,telemetry_bucket_ceiling(bucket_width,source.raw_floor)),safe_start)
 WHERE source_name=source.source_name;
 UPDATE telemetry_summary_write_gate SET frozen_through=LEAST(frozen_through,safe_start) WHERE source_name=source.source_name;
 -- A retry may have already replaced raw, hiding the end of its deleted tail.
 -- Preserve any unfinished recent repair until those old buckets are rebuilt.
 UPDATE telemetry_summary_work w SET next_run_at=now(),
  replay_until=CASE WHEN w.work_kind='recent' THEN
   GREATEST(w.replay_until,LEAST(time_bucket(p.bucket_width,now()),telemetry_bucket_ceiling(p.bucket_width,requested_end))) END,
  pending=false,blocked=false,
  minimum_batch_failures=0,successful_batches=0,last_error=NULL
 FROM telemetry_summary_progress p WHERE p.aggregate_name=w.aggregate_name AND p.source_name=source.source_name;
 RETURN safe_start;
END;
$$;
REVOKE ALL ON FUNCTION prepare_telemetry_seed(timestamptz,timestamptz) FROM PUBLIC;

-- Summary expiration remains independent: these targets intentionally discard
-- old summary tiers, and never authorize discarding unfinished raw evidence.
SELECT add_retention_policy(aggregate_name::regclass,CASE WHEN bucket_width=INTERVAL '1 hour' THEN INTERVAL '3 months' ELSE INTERVAL '3 years' END,if_not_exists=>true)
FROM telemetry_summary_progress WHERE aggregate_name NOT IN ('device_status_hourly','device_status_daily');

-- Operator views depend on extension-owned catalogs, which sqlc's offline
-- schema parser does not have. They are not application query inputs.
DO $health$ BEGIN
EXECUTE $view$CREATE VIEW telemetry_summary_health AS
SELECT p.*,w.work_kind,w.next_run_at,w.batch_buckets,w.successful_batches,w.pending,w.failures,w.blocked,w.last_error,
 w.last_success_at AS work_last_success_at,w.last_start,w.last_end,
 CASE WHEN p.completed_through IS NOT NULL THEN
 GREATEST(INTERVAL '0',time_bucket(p.bucket_width,now()-p.lateness)-p.completed_through) END AS catchup_lag,
 s.job_id,js.last_run_status,js.total_failures,js.last_run_started_at,js.next_start
FROM telemetry_summary_progress p JOIN telemetry_summary_source s USING (source_name)
JOIN telemetry_summary_work w USING (aggregate_name)
LEFT JOIN timescaledb_information.job_stats js ON js.job_id=s.job_id;$view$;

EXECUTE $view$CREATE VIEW telemetry_retention_health AS
SELECT s.*,g.frozen_through,p.coverage_start,p.completed_through,
 (SELECT count(*) FROM timescaledb_information.chunks c WHERE c.hypertable_schema='public'
  AND c.hypertable_name=s.source_name AND c.range_start>=p.coverage_start
  AND c.range_end<=LEAST(p.completed_through,now()-s.raw_retention)) AS eligible_raw_chunks,
 (SELECT count(*) FROM timescaledb_information.chunks c WHERE c.hypertable_schema='public'
  AND c.hypertable_name=s.source_name AND c.range_start>=p.coverage_start
  AND c.range_end<now()-s.raw_retention AND c.range_end>p.completed_through) AS awaiting_summary_chunks,
 (SELECT count(*) FROM timescaledb_information.chunks c WHERE c.hypertable_schema='public'
  AND c.hypertable_name=s.source_name AND c.range_start<p.coverage_start
  AND c.range_end<now()-s.raw_retention) AS unverified_raw_chunks
FROM telemetry_summary_source s JOIN telemetry_summary_write_gate g USING (source_name)
CROSS JOIN LATERAL (SELECT max(coverage_start) AS coverage_start,min(completed_through) AS completed_through
 FROM telemetry_summary_progress p WHERE p.source_name=s.source_name) p;$view$;
END $health$;
