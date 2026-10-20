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
