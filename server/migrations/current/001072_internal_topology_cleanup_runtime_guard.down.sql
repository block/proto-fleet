-- Restore the lock-handoff procedure while preserving job configuration,
-- scheduling, cleanup progress, and the original deferral counters.
CREATE OR REPLACE PROCEDURE compact_container_topology_job(job_id integer,config jsonb)
LANGUAGE plpgsql AS $$
DECLARE
 target bigint;
 cutoff timestamptz := now()-INTERVAL '10 days';
 batch_size integer := GREATEST(1,LEAST(COALESCE((config->>'batch_size')::integer,50),50));
 configured_microbatch_size integer := GREATEST(1,LEAST(COALESCE((config->>'microbatch_size')::integer,3),batch_size));
 remaining_budget integer;
 microbatch_size integer;
 microbatch_result jsonb;
 made_progress boolean := false;
 end_run boolean;
BEGIN
 IF NOT COALESCE((config->>'enabled')::boolean,false) THEN RETURN; END IF;
 SELECT device_id INTO target FROM container_topology_compaction_state WHERE next_run_at<=now()
  ORDER BY next_run_at,device_id LIMIT 1 FOR UPDATE SKIP LOCKED;
 IF target IS NULL THEN RETURN; END IF;
 -- Persist only a scheduling lease before beginning destructive work. A worker
 -- timeout or termination leaves this backoff and all completed microbatches
 -- committed, so a slow device cannot monopolize subsequent runs.
 UPDATE container_topology_compaction_state SET next_run_at=now()+INTERVAL '1 minute' WHERE device_id=target;
 COMMIT;
 remaining_budget := batch_size;
 LOOP
  microbatch_size := LEAST(remaining_budget,configured_microbatch_size);
  end_run := false;
  BEGIN
   SELECT compact_container_topology(target,cutoff,microbatch_size) INTO microbatch_result;
   made_progress := made_progress
    OR COALESCE((microbatch_result->>'examined')::integer,0)>0
    OR COALESCE((microbatch_result->>'deleted')::integer,0)>0
    OR COALESCE((microbatch_result->>'pinned')::integer,0)>0;
  EXCEPTION WHEN lock_not_available THEN
   -- Live ingestion owns the lock: this is expected backpressure, not a failure.
   -- A device that already made progress stays due behind older work. A device
   -- that could not start keeps the longer backoff and cannot spin each second.
   UPDATE container_topology_compaction_state SET
    next_run_at=CASE WHEN made_progress THEN now() ELSE now()+INTERVAL '1 minute' END,
    deferrals=deferrals+1,last_deferred_at=now() WHERE device_id=target;
   end_run := true;
  WHEN OTHERS THEN
   -- The failed subtransaction releases all its locks and mutations.
   UPDATE container_topology_compaction_state SET next_run_at=now()+INTERVAL '1 minute',
    failures=failures+1,last_error=SQLERRM WHERE device_id=target;
   end_run := true;
  END;
  IF NOT end_run THEN
   remaining_budget := remaining_budget-microbatch_size;
   IF remaining_budget<=0 THEN
    end_run := true;
   ELSE
    -- Renew the cancellation-safe lease only if compact_container_topology left
    -- more due work. FOUND avoids a separate state query on every microbatch.
    UPDATE container_topology_compaction_state SET next_run_at=now()+INTERVAL '1 minute'
     WHERE device_id=target AND next_run_at<=now();
    IF NOT FOUND THEN
     end_run := true;
    END IF;
   END IF;
  END IF;
  -- This is the lock handoff. It commits progress and the renewed lease before
  -- releasing the organization row for ingestion.
  COMMIT;
  IF end_run THEN RETURN; END IF;
 END LOOP;
END;
$$;

ALTER TABLE container_topology_compaction_state
 DROP COLUMN last_runtime_yield_at,
 DROP COLUMN runtime_yields;
