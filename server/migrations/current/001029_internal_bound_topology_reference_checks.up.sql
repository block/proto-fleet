-- requires-shared: 1000
-- Check a controller's tank IDs together. A lateral lookup per tank repeats
-- Timescale chunk pruning for every receipt/tank pair, exhausting the cleanup
-- job's two-second deadline as retained hourly chunks accumulate. One lookup
-- per receipt preserves historical-tank references and the existing batch cap.
CREATE OR REPLACE FUNCTION container_topology_references(ids bigint[])
RETURNS TABLE(observation_id bigint,reference_kind text) LANGUAGE sql STABLE AS $$
 WITH observations AS MATERIALIZED (
  SELECT o.id,o.device_id,o.collected_at,d.device_identifier FROM container_topology_observation o
  JOIN device d ON d.id=o.device_id WHERE o.id=ANY(ids)
 )
 SELECT DISTINCT m.topology_observation_id,'raw'::text FROM observations o
 JOIN LATERAL (SELECT topology_observation_id FROM device_metrics m
  WHERE m.time=o.collected_at AND m.device_identifier=o.device_identifier
   AND m.topology_observation_id=o.id LIMIT 1) m ON true
 UNION
 SELECT DISTINCT m.topology_observation_id,'raw'::text FROM observations o
 JOIN LATERAL (SELECT topology_observation_id FROM container_tank_metrics m
  WHERE m.time=o.collected_at AND m.topology_observation_id=o.id
   AND m.tank_id=ANY(ARRAY(SELECT t.id FROM container_tank t WHERE t.device_id=o.device_id))
  LIMIT 1) m ON true
 UNION
 SELECT DISTINCT topology_observation_id,'latest'::text FROM container_tank_latest_metrics WHERE topology_observation_id=ANY(ids)
 UNION
 SELECT DISTINCT topology_observation_id,'latest'::text FROM container_latest_metrics WHERE topology_observation_id=ANY(ids);
$$;
