-- requires-shared: 1000
-- Keep raw evidence until the scheduled bootstrap has materialized both tiers.
SELECT remove_retention_policy('hashboard_metrics', if_exists => true);
ALTER TABLE hashboard_metrics ADD COLUMN topology_version_id bigint REFERENCES container_topology_version(id);

-- Match the collection, not the health source timestamp or current placement.
-- Legacy rows without an observation keep NULL provenance and still summarize.
UPDATE hashboard_metrics b SET topology_version_id=o.version_id
FROM hashboard_installation i
JOIN container_module m ON m.id=i.module_id
JOIN container_tank t ON t.id=m.tank_id
JOIN container_topology_observation o ON o.device_id=t.device_id
WHERE b.installation_id=i.id AND b.collected_at=o.collected_at;

-- Source-time buckets preserve board history semantics. Collection bounds retain
-- the poll timing separately; parent rollups use collection-time buckets.
CREATE MATERIALIZED VIEW hashboard_metrics_hourly
WITH (timescaledb.continuous, timescaledb.materialized_only=false) AS
SELECT time_bucket(INTERVAL '1 hour',time) AS bucket,hashboard_id,installation_id,topology_version_id,
 min(collected_at) AS first_collected_at,max(collected_at) AS last_collected_at,count(*) AS observations,
 sum(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_sum,
 count(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_count,
 min(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_min,
 max(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_max,
 sum(temperature_c) FILTER (WHERE temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8) AS temperature_sum,
 count(temperature_c) FILTER (WHERE temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8) AS temperature_count,
 min(temperature_c) FILTER (WHERE temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8) AS temperature_min,
 max(temperature_c) FILTER (WHERE temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8) AS temperature_max,
 sum(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_sum,
 count(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_count,
 min(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_min,
 max(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_max,
 sum(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_sum,
 count(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_count,
 min(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_min,
 max(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_max
FROM hashboard_metrics
GROUP BY bucket,hashboard_id,installation_id,topology_version_id WITH NO DATA;
CREATE INDEX idx_hashboard_metrics_hourly_board_bucket ON hashboard_metrics_hourly(hashboard_id,bucket DESC);

-- Source-time buckets preserve board history semantics. Collection bounds retain
-- the poll timing separately; parent rollups use collection-time buckets.
CREATE MATERIALIZED VIEW hashboard_metrics_daily
WITH (timescaledb.continuous, timescaledb.materialized_only=false) AS
SELECT time_bucket(INTERVAL '1 day',time) AS bucket,hashboard_id,installation_id,topology_version_id,
 min(collected_at) AS first_collected_at,max(collected_at) AS last_collected_at,count(*) AS observations,
 sum(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_sum,
 count(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_count,
 min(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_min,
 max(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_max,
 sum(temperature_c) FILTER (WHERE temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8) AS temperature_sum,
 count(temperature_c) FILTER (WHERE temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8) AS temperature_count,
 min(temperature_c) FILTER (WHERE temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8) AS temperature_min,
 max(temperature_c) FILTER (WHERE temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8) AS temperature_max,
 sum(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_sum,
 count(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_count,
 min(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_min,
 max(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_max,
 sum(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_sum,
 count(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_count,
 min(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_min,
 max(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_max
FROM hashboard_metrics
GROUP BY bucket,hashboard_id,installation_id,topology_version_id WITH NO DATA;
CREATE INDEX idx_hashboard_metrics_daily_board_bucket ON hashboard_metrics_daily(hashboard_id,bucket DESC);

-- A typed union keeps sqlc and history consumers independent of aggregate
-- inference. Unknown legacy topology remains unknown rather than being dropped.
CREATE VIEW hashboard_metric_summaries AS
SELECT bucket::timestamptz,hashboard_id,installation_id,topology_version_id,first_collected_at::timestamptz,last_collected_at::timestamptz,observations::bigint,3600::int AS resolution_seconds,hashrate_sum::float8,hashrate_count::bigint,hashrate_min::float8,hashrate_max::float8,temperature_sum::float8,temperature_count::bigint,temperature_min::float8,temperature_max::float8,power_sum::float8,power_count::bigint,power_min::float8,power_max::float8,efficiency_sum::float8,efficiency_count::bigint,efficiency_min::float8,efficiency_max::float8 FROM hashboard_metrics_hourly
UNION ALL
SELECT bucket::timestamptz,hashboard_id,installation_id,topology_version_id,first_collected_at::timestamptz,last_collected_at::timestamptz,observations::bigint,86400::int AS resolution_seconds,hashrate_sum::float8,hashrate_count::bigint,hashrate_min::float8,hashrate_max::float8,temperature_sum::float8,temperature_count::bigint,temperature_min::float8,temperature_max::float8,power_sum::float8,power_count::bigint,power_min::float8,power_max::float8,efficiency_sum::float8,efficiency_count::bigint,efficiency_min::float8,efficiency_max::float8 FROM hashboard_metrics_daily;

CREATE PROCEDURE initialize_hashboard_metric_summaries(job_id integer,config jsonb)
LANGUAGE plpgsql AS $$
BEGIN
 CALL refresh_continuous_aggregate('hashboard_metrics_hourly',NULL,now());
 CALL refresh_continuous_aggregate('hashboard_metrics_daily',NULL,now());
 PERFORM add_continuous_aggregate_policy('hashboard_metrics_hourly',start_offset=>INTERVAL '1 day',end_offset=>INTERVAL '1 hour',schedule_interval=>INTERVAL '30 minutes',if_not_exists=>true);
 PERFORM add_retention_policy('hashboard_metrics_hourly',INTERVAL '3 months',if_not_exists=>true);
 PERFORM add_continuous_aggregate_policy('hashboard_metrics_daily',start_offset=>INTERVAL '7 days',end_offset=>INTERVAL '1 day',schedule_interval=>INTERVAL '6 hours',if_not_exists=>true);
 PERFORM add_retention_policy('hashboard_metrics_daily',INTERVAL '3 years',if_not_exists=>true);
 PERFORM add_retention_policy('hashboard_metrics',INTERVAL '10 days',if_not_exists=>true);
 PERFORM delete_job(job_id);
END;
$$;
SELECT add_job('initialize_hashboard_metric_summaries',INTERVAL '1 hour',initial_start=>now()+INTERVAL '1 minute');
