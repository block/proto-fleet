-- requires-shared: 1000
ALTER TABLE container_topology_observation ADD COLUMN last_confirmed_at timestamptz,
 ADD CONSTRAINT ck_container_topology_observation_confirmation CHECK (last_confirmed_at>=collected_at);

-- Index FK checks on retained raw and latest-value references.
CREATE INDEX idx_device_metrics_topology_observation ON device_metrics(topology_observation_id) WHERE topology_observation_id IS NOT NULL;
CREATE INDEX idx_container_tank_metrics_topology_observation ON container_tank_metrics(topology_observation_id);
CREATE INDEX idx_container_tank_latest_metrics_topology_observation ON container_tank_latest_metrics(topology_observation_id);
CREATE INDEX idx_container_latest_metrics_topology_observation ON container_latest_metrics(topology_observation_id);

CREATE TABLE container_topology_compaction_state (
 device_id bigint PRIMARY KEY REFERENCES device(id),
 scanned_through timestamptz,
 anchor_id bigint REFERENCES container_topology_observation(id),
 next_run_at timestamptz NOT NULL DEFAULT now(),
 last_run_at timestamptz,
 last_duration_ms double precision,
 examined bigint NOT NULL DEFAULT 0,
 deleted bigint NOT NULL DEFAULT 0,
 failures bigint NOT NULL DEFAULT 0,
 last_error text
);
CREATE INDEX idx_container_topology_compaction_state_due ON container_topology_compaction_state(next_run_at,device_id);
CREATE TABLE container_topology_compaction_pending (
 observation_id bigint PRIMARY KEY REFERENCES container_topology_observation(id) ON DELETE CASCADE,
 device_id bigint NOT NULL REFERENCES device(id),
 next_check_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_container_topology_compaction_pending_due ON container_topology_compaction_pending(device_id,next_check_at,observation_id);
INSERT INTO container_topology_compaction_state(device_id) SELECT DISTINCT device_id FROM container_topology_observation;

-- Register once, including newly discovered controllers. No per-poll progress
-- update: collection never waits for a cleanup cursor on an existing device.
CREATE FUNCTION register_container_topology_compaction() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO container_topology_compaction_state(device_id) VALUES (NEW.device_id) ON CONFLICT DO NOTHING;
 RETURN NEW;
END;
$$;
CREATE TRIGGER register_container_topology_compaction AFTER INSERT ON container_topology_observation
FOR EACH ROW EXECUTE FUNCTION register_container_topology_compaction();

CREATE FUNCTION container_topology_references(ids bigint[])
RETURNS TABLE(observation_id bigint,reference_kind text) LANGUAGE sql STABLE AS $$
 -- Ingestion stamps device_metrics only at the observation's collection time
 -- (StampContainerSample). Constrain time and device for chunk/segment pruning.
 WITH observations AS MATERIALIZED (
  SELECT o.id,o.collected_at,d.device_identifier FROM container_topology_observation o
  JOIN device d ON d.id=o.device_id WHERE o.id=ANY(ids)
 )
 SELECT DISTINCT m.topology_observation_id,'raw'::text FROM observations o
 JOIN LATERAL (SELECT topology_observation_id FROM device_metrics m
  WHERE m.time=o.collected_at AND m.device_identifier=o.device_identifier
   AND m.topology_observation_id=o.id LIMIT 1) m ON true
 UNION
 SELECT DISTINCT topology_observation_id,'raw'::text FROM container_tank_metrics WHERE topology_observation_id=ANY(ids)
 UNION
 SELECT DISTINCT topology_observation_id,'latest'::text FROM container_tank_latest_metrics WHERE topology_observation_id=ANY(ids)
 UNION
 SELECT DISTINCT topology_observation_id,'latest'::text FROM container_latest_metrics WHERE topology_observation_id=ANY(ids);
$$;

-- Read-only bounded classifier, shared by the candidate report and cleanup.
-- Policy v1 is fixed at 120 seconds: default scheduler eligibility is 15 seconds
-- plus its five-second tick and collection duration. Several missed polls are
-- tolerated, but longer outages remain explicit. This is not metric freshness.
-- Changing this policy cannot reconstruct timestamps already compacted away.
CREATE FUNCTION container_topology_candidates(p_device bigint,p_cutoff timestamptz,p_limit integer)
RETURNS TABLE(observation_id bigint,anchor_id bigint,collected_at timestamptz,confirmed_at timestamptz,is_anchor boolean)
LANGUAGE plpgsql STABLE AS $$
DECLARE
 progress container_topology_compaction_state%ROWTYPE;
 anchor container_topology_observation%ROWTYPE;
 observation container_topology_observation%ROWTYPE;
BEGIN
 IF p_limit IS NULL OR p_cutoff IS NULL OR p_device IS NULL OR p_limit<1 OR p_limit>10000 OR p_cutoff>now()-INTERVAL '10 days' THEN
  RAISE EXCEPTION 'cleanup requires a limit of 1..10000 and a cutoff at least ten days old';
 END IF;
 SELECT * INTO progress FROM container_topology_compaction_state WHERE device_id=p_device;
 SELECT * INTO anchor FROM container_topology_observation WHERE id=progress.anchor_id;
 FOR observation IN
  SELECT o.* FROM container_topology_observation o WHERE o.device_id=p_device
   AND o.collected_at>COALESCE(progress.scanned_through,'-infinity'::timestamptz)
   AND o.collected_at<p_cutoff ORDER BY o.collected_at LIMIT p_limit
 LOOP
  is_anchor := anchor.id IS NULL OR observation.version_id<>anchor.version_id
   OR observation.complete<>anchor.complete
   OR observation.collected_at>COALESCE(anchor.last_confirmed_at,anchor.collected_at)+INTERVAL '120 seconds';
  IF is_anchor THEN
   anchor := observation;
  ELSE
   anchor.last_confirmed_at := GREATEST(COALESCE(anchor.last_confirmed_at,anchor.collected_at),COALESCE(observation.last_confirmed_at,observation.collected_at));
  END IF;
  observation_id := observation.id;
  anchor_id := anchor.id;
  collected_at := observation.collected_at;
  confirmed_at := COALESCE(anchor.last_confirmed_at,anchor.collected_at);
  RETURN NEXT;
 END LOOP;
END;
$$;

CREATE FUNCTION compact_container_topology(p_device bigint,p_cutoff timestamptz,p_limit integer)
RETURNS jsonb LANGUAGE plpgsql AS $$
DECLARE
 candidate record;
 pending record;
 final_scanned_through timestamptz;
 final_anchor_id bigint;
 examined_count integer := 0;
 deleted_count integer := 0;
 pinned_count integer := 0;
 started timestamptz := clock_timestamp();
BEGIN
 IF p_limit IS NULL OR p_cutoff IS NULL OR p_device IS NULL OR p_limit<1 OR p_limit>10000 OR p_cutoff>now()-INTERVAL '10 days' THEN
  RAISE EXCEPTION 'cleanup requires a device, a limit of 1..10000 and a cutoff at least ten days old';
 END IF;
 -- Match ingestion's organization lock before touching cleanup progress. Never
 -- queue behind live ingestion. The scheduler retries this device later.
 PERFORM o.id FROM organization o JOIN device d ON d.org_id=o.id WHERE d.id=p_device FOR NO KEY UPDATE OF o NOWAIT;
 SELECT s.scanned_through,s.anchor_id INTO final_scanned_through,final_anchor_id
 FROM container_topology_compaction_state s WHERE s.device_id=p_device FOR UPDATE NOWAIT;
 IF NOT FOUND THEN RETURN jsonb_build_object('examined',0,'deleted',0,'pinned',0); END IF;
 -- Retry pinned receipts independently of forward progress; bounded work even
 -- when a latest-value reference pins one forever. The represented spans already
 -- include these observations, so deleting them does not alter any anchor.
 FOR pending IN
  WITH due AS MATERIALIZED (
   SELECT p.observation_id FROM container_topology_compaction_pending p
   WHERE p.device_id=p_device AND p.next_check_at<=now() ORDER BY p.next_check_at,p.observation_id LIMIT p_limit
  ), refs AS MATERIALIZED (
   SELECT * FROM container_topology_references(ARRAY(SELECT d.observation_id FROM due d))
  )
  SELECT d.observation_id,EXISTS(SELECT 1 FROM refs r WHERE r.observation_id=d.observation_id) AS pinned FROM due d
 LOOP
  IF pending.pinned THEN
   UPDATE container_topology_compaction_pending SET next_check_at=now()+INTERVAL '1 hour' WHERE observation_id=pending.observation_id;
   pinned_count := pinned_count+1;
  ELSE
   DELETE FROM container_topology_observation WHERE id=pending.observation_id;
   deleted_count := deleted_count+1;
  END IF;
 END LOOP;
 FOR candidate IN
  WITH candidates AS MATERIALIZED (SELECT * FROM container_topology_candidates(p_device,p_cutoff,p_limit)),
  refs AS MATERIALIZED (SELECT * FROM container_topology_references(ARRAY(SELECT c.observation_id FROM candidates c WHERE NOT c.is_anchor)))
  SELECT c.*,EXISTS(SELECT 1 FROM refs r WHERE r.observation_id=c.observation_id) AS pinned FROM candidates c ORDER BY c.collected_at
 LOOP
  examined_count := examined_count+1;
  IF NOT candidate.is_anchor THEN
   UPDATE container_topology_observation SET last_confirmed_at=candidate.confirmed_at WHERE id=candidate.anchor_id;
   IF candidate.pinned THEN
    INSERT INTO container_topology_compaction_pending(observation_id,device_id,next_check_at)
     VALUES(candidate.observation_id,p_device,now()+INTERVAL '1 hour') ON CONFLICT DO NOTHING;
    pinned_count := pinned_count+1;
   ELSE
    DELETE FROM container_topology_observation WHERE id=candidate.observation_id;
    deleted_count := deleted_count+1;
   END IF;
  END IF;
  final_scanned_through := candidate.collected_at;
  final_anchor_id := candidate.anchor_id;
 END LOOP;
 -- A retry backlog also needs another bounded turn, even after forward scanning
 -- catches up. Pins deferred into the future must not cause immediate spinning.
 UPDATE container_topology_compaction_state SET scanned_through=final_scanned_through,
  anchor_id=final_anchor_id,next_run_at=now()+CASE WHEN examined_count=p_limit OR EXISTS (
  SELECT 1 FROM container_topology_compaction_pending p WHERE p.device_id=p_device AND p.next_check_at<=now()
 ) THEN INTERVAL '0' ELSE INTERVAL '1 hour' END,
  last_run_at=now(),last_duration_ms=extract(epoch FROM clock_timestamp()-started)*1000,
  examined=examined+examined_count,deleted=deleted+deleted_count,last_error=NULL WHERE device_id=p_device;
 RETURN jsonb_build_object('examined',examined_count,'deleted',deleted_count,'pinned',pinned_count);
END;
$$;

-- One device per job transaction bounds organization-lock duration. Fair due-time
-- ordering prevents a busy controller or an indefinitely pinned receipt starving
-- other controllers. Timescale's max_runtime bounds the whole transaction.
CREATE PROCEDURE compact_container_topology_job(job_id integer,config jsonb)
LANGUAGE plpgsql AS $$
DECLARE
 target bigint;
 cutoff timestamptz := now()-INTERVAL '10 days';
 batch_size integer := COALESCE((config->>'batch_size')::integer,500);
BEGIN
 IF NOT COALESCE((config->>'enabled')::boolean,false) THEN RETURN; END IF;
 SELECT device_id INTO target FROM container_topology_compaction_state WHERE next_run_at<=now()
  ORDER BY next_run_at,device_id LIMIT 1 FOR UPDATE SKIP LOCKED;
 IF target IS NULL THEN RETURN; END IF;
 -- Persist only a scheduling lease before beginning destructive work. A worker
 -- timeout or termination rolls back the entire batch but not this backoff,
 -- so a slow device cannot monopolize subsequent runs. CALL outside a transaction.
 UPDATE container_topology_compaction_state SET next_run_at=now()+INTERVAL '1 minute' WHERE device_id=target;
 COMMIT;
 BEGIN
  PERFORM compact_container_topology(target,cutoff,batch_size);
 EXCEPTION WHEN lock_not_available THEN
  -- Live ingestion owns the lock: this is expected backpressure, not a failure.
  UPDATE container_topology_compaction_state SET next_run_at=now()+INTERVAL '1 minute' WHERE device_id=target;
 WHEN OTHERS THEN
  -- The failed subtransaction releases all its locks and mutations.
  UPDATE container_topology_compaction_state SET next_run_at=now()+INTERVAL '1 minute',failures=failures+1,last_error=SQLERRM WHERE device_id=target;
 END;
END;
$$;
SELECT add_job('compact_container_topology_job',INTERVAL '10 seconds',config=>' {"enabled":false,"batch_size":500}'::jsonb);
SELECT alter_job(job_id,max_runtime=>INTERVAL '2 seconds',retry_period=>INTERVAL '1 minute')
FROM timescaledb_information.jobs WHERE proc_name='compact_container_topology_job';
