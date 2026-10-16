-- Disabling compression requires decompressing retained chunks first. Rollback
-- can need substantial disk space and time; normally prefer a forward fix.
DROP FUNCTION stamp_hashboard_topology(bigint,timestamptz,jsonb);

SELECT remove_compression_policy('container_plant_metrics');
SELECT decompress_chunk(c, if_compressed => true) FROM show_chunks('container_plant_metrics') c;
ALTER TABLE container_plant_metrics SET (timescaledb.compress = false);
SELECT set_chunk_time_interval('container_plant_metrics', INTERVAL '7 days');

SELECT remove_compression_policy('container_tank_metrics');
SELECT decompress_chunk(c, if_compressed => true) FROM show_chunks('container_tank_metrics') c;
ALTER TABLE container_tank_metrics SET (timescaledb.compress = false);
SELECT set_chunk_time_interval('container_tank_metrics', INTERVAL '1 day');
ALTER TABLE container_tank_metrics
 DROP CONSTRAINT fk_container_tank_metrics_observation_time,
 ADD CONSTRAINT container_tank_metrics_topology_observation_id_fkey
 FOREIGN KEY(topology_observation_id) REFERENCES container_topology_observation(id);
ALTER TABLE container_topology_observation
 DROP CONSTRAINT uq_container_topology_observation_id_collected_at;

SELECT remove_compression_policy('container_module_metrics');
SELECT decompress_chunk(c, if_compressed => true) FROM show_chunks('container_module_metrics') c;
ALTER TABLE container_module_metrics SET (timescaledb.compress = false);
SELECT set_chunk_time_interval('container_module_metrics', INTERVAL '7 days');

SELECT remove_compression_policy('hashboard_metrics');
SELECT decompress_chunk(c, if_compressed => true) FROM show_chunks('hashboard_metrics') c;
ALTER TABLE hashboard_metrics SET (timescaledb.compress = false);
SELECT set_chunk_time_interval('hashboard_metrics', INTERVAL '1 day');

CREATE OR REPLACE FUNCTION container_topology_references(ids bigint[])
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

CREATE OR REPLACE PROCEDURE compact_container_topology_job(job_id integer,config jsonb)
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
