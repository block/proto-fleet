-- requires-shared: 1000

-- Stay inside the scheduler's two-second deadline even when several otherwise
-- successful microbatches have variable planning, FK-check, or commit costs.
-- Lock waits use a smaller bound so they become ordinary deferrals instead of
-- consuming the whole worker deadline.
ALTER TABLE container_topology_compaction_state
 ADD COLUMN runtime_yields bigint NOT NULL DEFAULT 0,
 ADD COLUMN last_runtime_yield_at timestamptz;

CREATE OR REPLACE PROCEDURE compact_container_topology_job(job_id integer,config jsonb)
LANGUAGE plpgsql AS $$
DECLARE
 target bigint;
 cutoff timestamptz := now()-INTERVAL '10 days';
 batch_size integer := GREATEST(1,LEAST(COALESCE((config->>'batch_size')::integer,50),50));
 microbatch_limit integer := GREATEST(1,LEAST(COALESCE((config->>'microbatch_size')::integer,3),batch_size));
 run_budget_ms integer := GREATEST(100,LEAST(COALESCE((config->>'run_budget_ms')::integer,1000),1000));
 lock_timeout_ms integer := GREATEST(1,LEAST(COALESCE((config->>'lock_timeout_ms')::integer,250),250));
 run_started_at timestamptz := clock_timestamp();
 remaining_rows integer;
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
 remaining_rows := batch_size;
 LOOP
  -- Commit latency counts against the worker deadline. Recheck after the
  -- initial lease commit and every handoff commit before starting more work.
  IF extract(epoch FROM clock_timestamp()-run_started_at)*1000>=run_budget_ms THEN
   UPDATE container_topology_compaction_state SET next_run_at=now(),
    runtime_yields=runtime_yields+1,last_runtime_yield_at=clock_timestamp()
    WHERE device_id=target;
   COMMIT;
   RETURN;
  END IF;
  microbatch_size := LEAST(remaining_rows,microbatch_limit);
  end_run := false;
  -- SET LOCAL resets at every handoff commit. Reapply it before each bounded
  -- microbatch, including FK checks that can wait behind an in-flight writer.
  PERFORM set_config('lock_timeout',lock_timeout_ms::text||'ms',true);
  BEGIN
   SELECT compact_container_topology(target,cutoff,microbatch_size) INTO microbatch_result;
   made_progress := made_progress
    OR COALESCE((microbatch_result->>'examined')::integer,0)>0
    OR COALESCE((microbatch_result->>'deleted')::integer,0)>0
    OR COALESCE((microbatch_result->>'pinned')::integer,0)>0;
  EXCEPTION WHEN lock_not_available THEN
   -- Both the nonblocking organization guard and bounded FK lock waits use
   -- SQLSTATE 55P03. They are expected backpressure, not cleanup failures.
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
   remaining_rows := remaining_rows-microbatch_size;
   IF remaining_rows<=0 THEN
    end_run := true;
   ELSE
    -- Renew the cancellation-safe lease only if compact_container_topology left
    -- more due work. Yielded work stays due behind older controllers.
    IF extract(epoch FROM clock_timestamp()-run_started_at)*1000>=run_budget_ms THEN
     UPDATE container_topology_compaction_state SET next_run_at=now(),
      runtime_yields=runtime_yields+1,last_runtime_yield_at=clock_timestamp()
      WHERE device_id=target AND next_run_at<=now();
     end_run := true;
    ELSE
     UPDATE container_topology_compaction_state SET next_run_at=now()+INTERVAL '1 minute'
      WHERE device_id=target AND next_run_at<=now();
     IF NOT FOUND THEN
      end_run := true;
     END IF;
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
