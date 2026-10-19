-- requires-shared: 1000
-- Match device_metrics: equipment segments, newest samples first, and hourly
-- compression of chunks whose entire time range is at least six hours old.
-- Only new chunks adopt the one-hour interval. Existing day/week chunks remain
-- eligible once their full range ages out; do not rewrite history in migration.

-- UPDATE ... FROM a recordset can join after Timescale has decompressed every
-- board in a chunk. Group inside the database to keep one client round trip,
-- with explicit segment and exact source-time parameters on each UPDATE.
CREATE FUNCTION stamp_hashboard_topology(p_version bigint,p_collected_at timestamptz,p_samples jsonb)
RETURNS void LANGUAGE plpgsql SET plan_cache_mode=force_custom_plan AS $$
DECLARE
 sample record;
BEGIN
 FOR sample IN
  SELECT x.time,array_agg(DISTINCT x.hashboard_id) AS board_ids
  FROM jsonb_to_recordset(p_samples) x(hashboard_id bigint,time timestamptz)
  GROUP BY x.time ORDER BY x.time
 LOOP
  -- Timescale's DML decompression cannot prune a generic-plan ANY(array)
  -- parameter. Plan with this group's actual board IDs and timestamp even if
  -- the outer prepared SELECT uses a generic plan. The setting is call-local.
  UPDATE hashboard_metrics SET topology_version_id=p_version
  WHERE hashboard_id=ANY(sample.board_ids) AND time=sample.time
   AND collected_at=p_collected_at;
 END LOOP;
END;
$$;

CREATE OR REPLACE FUNCTION container_topology_references(ids bigint[])
RETURNS TABLE(observation_id bigint,reference_kind text) LANGUAGE sql STABLE AS $$
 -- Ingestion stamps device_metrics only at the observation's collection time
 -- (StampContainerSample). Constrain time and device for chunk/segment pruning.
 WITH observations AS MATERIALIZED (
  SELECT o.id,o.device_id,o.collected_at,d.device_identifier FROM container_topology_observation o
  JOIN device d ON d.id=o.device_id WHERE o.id=ANY(ids)
 )
 SELECT DISTINCT m.topology_observation_id,'raw'::text FROM observations o
 JOIN LATERAL (SELECT topology_observation_id FROM device_metrics m
  WHERE m.time=o.collected_at AND m.device_identifier=o.device_identifier
   AND m.topology_observation_id=o.id LIMIT 1) m ON true
 UNION
 -- Tank rollups use the same collection time as their observation. Include
 -- historical tanks too: retired equipment can still have retained telemetry.
 SELECT DISTINCT m.topology_observation_id,'raw'::text FROM observations o
 JOIN container_tank t ON t.device_id=o.device_id
 JOIN LATERAL (SELECT topology_observation_id FROM container_tank_metrics m
  WHERE m.time=o.collected_at AND m.tank_id=t.id
   AND m.topology_observation_id=o.id LIMIT 1) m ON true
 UNION
 SELECT DISTINCT topology_observation_id,'latest'::text FROM container_tank_latest_metrics WHERE topology_observation_id=ANY(ids)
 UNION
 SELECT DISTINCT topology_observation_id,'latest'::text FROM container_latest_metrics WHERE topology_observation_id=ANY(ids);
$$;

SELECT set_chunk_time_interval('container_plant_metrics', INTERVAL '1 hour');
ALTER TABLE container_plant_metrics SET (
 timescaledb.compress,
 timescaledb.compress_segmentby = 'device_id',
 timescaledb.compress_orderby = 'time DESC'
);
SELECT add_compression_policy('container_plant_metrics', INTERVAL '6 hours', schedule_interval => INTERVAL '1 hour');

-- RI checks run independently of container_topology_references(). Include the
-- collection time in the FK so deleting an expired receipt can prune retained
-- chunks instead of probing all compressed tank history for its observation ID.
-- Add constraints before enabling compression; existing rows must satisfy the
-- same collection-time invariant already used by ingestion and compaction.
ALTER TABLE container_topology_observation
 ADD CONSTRAINT uq_container_topology_observation_id_collected_at UNIQUE(id,collected_at);
ALTER TABLE container_tank_metrics
 DROP CONSTRAINT container_tank_metrics_topology_observation_id_fkey,
 ADD CONSTRAINT fk_container_tank_metrics_observation_time
 FOREIGN KEY(topology_observation_id,time) REFERENCES container_topology_observation(id,collected_at);

SELECT set_chunk_time_interval('container_tank_metrics', INTERVAL '1 hour');
ALTER TABLE container_tank_metrics SET (
 timescaledb.compress,
 timescaledb.compress_segmentby = 'tank_id',
 timescaledb.compress_orderby = 'time DESC'
);
SELECT add_compression_policy('container_tank_metrics', INTERVAL '6 hours', schedule_interval => INTERVAL '1 hour');

SELECT set_chunk_time_interval('container_module_metrics', INTERVAL '1 hour');
ALTER TABLE container_module_metrics SET (
 timescaledb.compress,
 timescaledb.compress_segmentby = 'module_id',
 timescaledb.compress_orderby = 'time DESC'
);
SELECT add_compression_policy('container_module_metrics', INTERVAL '6 hours', schedule_interval => INTERVAL '1 hour');

SELECT set_chunk_time_interval('hashboard_metrics', INTERVAL '1 hour');
ALTER TABLE hashboard_metrics SET (
 timescaledb.compress,
 timescaledb.compress_segmentby = 'hashboard_id',
 timescaledb.compress_orderby = 'time DESC'
);
SELECT add_compression_policy('hashboard_metrics', INTERVAL '6 hours', schedule_interval => INTERVAL '1 hour');

-- Chunk-level FK trigger overhead still grows with retained chunk count even
-- with time pruning. Bound each of the new-candidate and pending-retry queues
-- to 50 per scheduled transaction so both can progress under max_runtime=2s.
-- Keep the existing enabled flag, schedule, lease, and manual function limits.
CREATE OR REPLACE PROCEDURE compact_container_topology_job(job_id integer,config jsonb)
LANGUAGE plpgsql AS $$
DECLARE
 target bigint;
 cutoff timestamptz := now()-INTERVAL '10 days';
 batch_size integer := LEAST(COALESCE((config->>'batch_size')::integer,50),50);
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
