-- requires-shared: 1000
-- Preserve raw evidence until the backfill job has materialized the existing history.
SELECT remove_retention_policy('device_metrics', if_exists => true);
SELECT remove_retention_policy('container_tank_metrics', if_exists => true);
SELECT remove_retention_policy('container_module_metrics', if_exists => true);

-- Nullable provenance supports existing compressed raw chunks. Topology versions
-- are immutable and retained indefinitely; writers stamp these in the poll transaction.
ALTER TABLE device_metrics ADD COLUMN topology_version_id bigint;
ALTER TABLE container_tank_metrics ADD COLUMN topology_version_id bigint;
ALTER TABLE container_module_metrics ADD COLUMN topology_version_id bigint, ADD COLUMN site_id bigint;
UPDATE device_metrics m SET topology_version_id=o.version_id FROM container_topology_observation o WHERE o.id=m.topology_observation_id;
UPDATE container_tank_metrics m SET topology_version_id=o.version_id FROM container_topology_observation o WHERE o.id=m.topology_observation_id;
UPDATE container_module_metrics mm SET topology_version_id=o.version_id,
 site_id=CASE WHEN (v.topology#>>'{site,Valid}')::boolean THEN (v.topology#>>'{site,Int64}')::bigint END
FROM container_module m JOIN container_tank t ON t.id=m.tank_id
JOIN container_topology_observation o ON o.device_id=t.device_id
JOIN container_topology_version v ON v.id=o.version_id
WHERE m.id=mm.module_id AND o.collected_at=mm.collected_at;

-- Separate topology versions and sites even when a move falls inside a bucket.
CREATE MATERIALIZED VIEW container_metrics_hourly
WITH (timescaledb.continuous, timescaledb.materialized_only=false) AS
SELECT time_bucket(INTERVAL '1 hour',time) AS bucket,device_identifier,site_id,topology_version_id,
 sum(temp_c) FILTER (WHERE temp_c > '-Infinity'::float8 AND temp_c < 'Infinity'::float8) AS temperature_sum,
 count(temp_c) FILTER (WHERE temp_c > '-Infinity'::float8 AND temp_c < 'Infinity'::float8) AS temperature_count,
 min(temp_c) FILTER (WHERE temp_c > '-Infinity'::float8 AND temp_c < 'Infinity'::float8) AS temperature_min,
 max(temp_c) FILTER (WHERE temp_c > '-Infinity'::float8 AND temp_c < 'Infinity'::float8) AS temperature_max,
 min(COALESCE((coverage#>>'{temperature,reporting}')::int,0)) AS temperature_reporting,
 max(COALESCE((coverage#>>'{temperature,expected}')::int,0)) AS temperature_expected,
 bool_and(COALESCE((coverage#>>'{temperature,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{temperature,reporting}')::int,0)=COALESCE((coverage#>>'{temperature,expected}')::int,0) AND COALESCE((coverage#>>'{temperature,expected}')::int,0)>0 AND COALESCE(temp_c > '-Infinity'::float8 AND temp_c < 'Infinity'::float8,false)) AS temperature_complete,
 sum(hash_rate_hs) FILTER (WHERE hash_rate_hs > '-Infinity'::float8 AND hash_rate_hs < 'Infinity'::float8) AS hashrate_sum,
 count(hash_rate_hs) FILTER (WHERE hash_rate_hs > '-Infinity'::float8 AND hash_rate_hs < 'Infinity'::float8) AS hashrate_count,
 min(hash_rate_hs) FILTER (WHERE hash_rate_hs > '-Infinity'::float8 AND hash_rate_hs < 'Infinity'::float8) AS hashrate_min,
 max(hash_rate_hs) FILTER (WHERE hash_rate_hs > '-Infinity'::float8 AND hash_rate_hs < 'Infinity'::float8) AS hashrate_max,
 min(COALESCE((coverage#>>'{hashrate,reporting}')::int,0)) AS hashrate_reporting,
 max(COALESCE((coverage#>>'{hashrate,expected}')::int,0)) AS hashrate_expected,
 bool_and(COALESCE((coverage#>>'{hashrate,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{hashrate,reporting}')::int,0)=COALESCE((coverage#>>'{hashrate,expected}')::int,0) AND COALESCE((coverage#>>'{hashrate,expected}')::int,0)>0 AND COALESCE(hash_rate_hs > '-Infinity'::float8 AND hash_rate_hs < 'Infinity'::float8,false)) AS hashrate_complete,
 sum(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_sum,
 count(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_count,
 min(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_min,
 max(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_max,
 min(COALESCE((coverage#>>'{power,reporting}')::int,0)) AS power_reporting,
 max(COALESCE((coverage#>>'{power,expected}')::int,0)) AS power_expected,
 bool_and(COALESCE((coverage#>>'{power,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{power,reporting}')::int,0)=COALESCE((coverage#>>'{power,expected}')::int,0) AND COALESCE((coverage#>>'{power,expected}')::int,0)>0 AND COALESCE(power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8,false)) AS power_complete,
 sum(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_sum,
 count(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_count,
 min(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_min,
 max(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_max,
 min(COALESCE((coverage#>>'{hashrate,reporting}')::int,0)) AS efficiency_reporting,
 max(COALESCE((coverage#>>'{hashrate,expected}')::int,0)) AS efficiency_expected,
 bool_and(COALESCE((coverage#>>'{hashrate,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{hashrate,reporting}')::int,0)=COALESCE((coverage#>>'{hashrate,expected}')::int,0) AND COALESCE((coverage#>>'{hashrate,expected}')::int,0)>0 AND COALESCE(efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8,false)) AS efficiency_complete
FROM device_metrics WHERE metric_semantics=2 AND topology_version_id IS NOT NULL
GROUP BY bucket,device_identifier,site_id,topology_version_id WITH NO DATA;
CREATE INDEX idx_container_metrics_hourly_equipment_bucket ON container_metrics_hourly(device_identifier,bucket DESC);

-- Separate topology versions and sites even when a move falls inside a bucket.
CREATE MATERIALIZED VIEW container_metrics_daily
WITH (timescaledb.continuous, timescaledb.materialized_only=false) AS
SELECT time_bucket(INTERVAL '1 day',time) AS bucket,device_identifier,site_id,topology_version_id,
 sum(temp_c) FILTER (WHERE temp_c > '-Infinity'::float8 AND temp_c < 'Infinity'::float8) AS temperature_sum,
 count(temp_c) FILTER (WHERE temp_c > '-Infinity'::float8 AND temp_c < 'Infinity'::float8) AS temperature_count,
 min(temp_c) FILTER (WHERE temp_c > '-Infinity'::float8 AND temp_c < 'Infinity'::float8) AS temperature_min,
 max(temp_c) FILTER (WHERE temp_c > '-Infinity'::float8 AND temp_c < 'Infinity'::float8) AS temperature_max,
 min(COALESCE((coverage#>>'{temperature,reporting}')::int,0)) AS temperature_reporting,
 max(COALESCE((coverage#>>'{temperature,expected}')::int,0)) AS temperature_expected,
 bool_and(COALESCE((coverage#>>'{temperature,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{temperature,reporting}')::int,0)=COALESCE((coverage#>>'{temperature,expected}')::int,0) AND COALESCE((coverage#>>'{temperature,expected}')::int,0)>0 AND COALESCE(temp_c > '-Infinity'::float8 AND temp_c < 'Infinity'::float8,false)) AS temperature_complete,
 sum(hash_rate_hs) FILTER (WHERE hash_rate_hs > '-Infinity'::float8 AND hash_rate_hs < 'Infinity'::float8) AS hashrate_sum,
 count(hash_rate_hs) FILTER (WHERE hash_rate_hs > '-Infinity'::float8 AND hash_rate_hs < 'Infinity'::float8) AS hashrate_count,
 min(hash_rate_hs) FILTER (WHERE hash_rate_hs > '-Infinity'::float8 AND hash_rate_hs < 'Infinity'::float8) AS hashrate_min,
 max(hash_rate_hs) FILTER (WHERE hash_rate_hs > '-Infinity'::float8 AND hash_rate_hs < 'Infinity'::float8) AS hashrate_max,
 min(COALESCE((coverage#>>'{hashrate,reporting}')::int,0)) AS hashrate_reporting,
 max(COALESCE((coverage#>>'{hashrate,expected}')::int,0)) AS hashrate_expected,
 bool_and(COALESCE((coverage#>>'{hashrate,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{hashrate,reporting}')::int,0)=COALESCE((coverage#>>'{hashrate,expected}')::int,0) AND COALESCE((coverage#>>'{hashrate,expected}')::int,0)>0 AND COALESCE(hash_rate_hs > '-Infinity'::float8 AND hash_rate_hs < 'Infinity'::float8,false)) AS hashrate_complete,
 sum(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_sum,
 count(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_count,
 min(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_min,
 max(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_max,
 min(COALESCE((coverage#>>'{power,reporting}')::int,0)) AS power_reporting,
 max(COALESCE((coverage#>>'{power,expected}')::int,0)) AS power_expected,
 bool_and(COALESCE((coverage#>>'{power,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{power,reporting}')::int,0)=COALESCE((coverage#>>'{power,expected}')::int,0) AND COALESCE((coverage#>>'{power,expected}')::int,0)>0 AND COALESCE(power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8,false)) AS power_complete,
 sum(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_sum,
 count(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_count,
 min(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_min,
 max(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_max,
 min(COALESCE((coverage#>>'{hashrate,reporting}')::int,0)) AS efficiency_reporting,
 max(COALESCE((coverage#>>'{hashrate,expected}')::int,0)) AS efficiency_expected,
 bool_and(COALESCE((coverage#>>'{hashrate,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{hashrate,reporting}')::int,0)=COALESCE((coverage#>>'{hashrate,expected}')::int,0) AND COALESCE((coverage#>>'{hashrate,expected}')::int,0)>0 AND COALESCE(efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8,false)) AS efficiency_complete
FROM device_metrics WHERE metric_semantics=2 AND topology_version_id IS NOT NULL
GROUP BY bucket,device_identifier,site_id,topology_version_id WITH NO DATA;
CREATE INDEX idx_container_metrics_daily_equipment_bucket ON container_metrics_daily(device_identifier,bucket DESC);

-- Separate topology versions and sites even when a move falls inside a bucket.
CREATE MATERIALIZED VIEW container_tank_metrics_hourly
WITH (timescaledb.continuous, timescaledb.materialized_only=false) AS
SELECT time_bucket(INTERVAL '1 hour',time) AS bucket,tank_id,site_id,topology_version_id,
 sum(temperature_c) FILTER (WHERE temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8) AS temperature_sum,
 count(temperature_c) FILTER (WHERE temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8) AS temperature_count,
 min(min_temperature_c) FILTER (WHERE min_temperature_c > '-Infinity'::float8 AND min_temperature_c < 'Infinity'::float8) AS temperature_min,
 max(max_temperature_c) FILTER (WHERE max_temperature_c > '-Infinity'::float8 AND max_temperature_c < 'Infinity'::float8) AS temperature_max,
 min(COALESCE((coverage#>>'{temperature,reporting}')::int,0)) AS temperature_reporting,
 max(COALESCE((coverage#>>'{temperature,expected}')::int,0)) AS temperature_expected,
 bool_and(COALESCE((coverage#>>'{temperature,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{temperature,reporting}')::int,0)=COALESCE((coverage#>>'{temperature,expected}')::int,0) AND COALESCE((coverage#>>'{temperature,expected}')::int,0)>0 AND COALESCE(temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8,false)) AS temperature_complete,
 sum(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_sum,
 count(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_count,
 min(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_min,
 max(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_max,
 min(COALESCE((coverage#>>'{hashrate,reporting}')::int,0)) AS hashrate_reporting,
 max(COALESCE((coverage#>>'{hashrate,expected}')::int,0)) AS hashrate_expected,
 bool_and(COALESCE((coverage#>>'{hashrate,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{hashrate,reporting}')::int,0)=COALESCE((coverage#>>'{hashrate,expected}')::int,0) AND COALESCE((coverage#>>'{hashrate,expected}')::int,0)>0 AND COALESCE(hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8,false)) AS hashrate_complete,
 sum(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_sum,
 count(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_count,
 min(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_min,
 max(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_max,
 min(COALESCE((coverage#>>'{power,reporting}')::int,0)) AS power_reporting,
 max(COALESCE((coverage#>>'{power,expected}')::int,0)) AS power_expected,
 bool_and(COALESCE((coverage#>>'{power,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{power,reporting}')::int,0)=COALESCE((coverage#>>'{power,expected}')::int,0) AND COALESCE((coverage#>>'{power,expected}')::int,0)>0 AND COALESCE(power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8,false)) AS power_complete,
 sum(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_sum,
 count(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_count,
 min(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_min,
 max(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_max,
 min(COALESCE((coverage#>>'{hashrate,reporting}')::int,0)) AS efficiency_reporting,
 max(COALESCE((coverage#>>'{hashrate,expected}')::int,0)) AS efficiency_expected,
 bool_and(COALESCE((coverage#>>'{hashrate,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{hashrate,reporting}')::int,0)=COALESCE((coverage#>>'{hashrate,expected}')::int,0) AND COALESCE((coverage#>>'{hashrate,expected}')::int,0)>0 AND COALESCE(efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8,false)) AS efficiency_complete
FROM container_tank_metrics WHERE coverage IS NOT NULL AND topology_version_id IS NOT NULL
GROUP BY bucket,tank_id,site_id,topology_version_id WITH NO DATA;
CREATE INDEX idx_container_tank_metrics_hourly_equipment_bucket ON container_tank_metrics_hourly(tank_id,bucket DESC);

-- Separate topology versions and sites even when a move falls inside a bucket.
CREATE MATERIALIZED VIEW container_tank_metrics_daily
WITH (timescaledb.continuous, timescaledb.materialized_only=false) AS
SELECT time_bucket(INTERVAL '1 day',time) AS bucket,tank_id,site_id,topology_version_id,
 sum(temperature_c) FILTER (WHERE temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8) AS temperature_sum,
 count(temperature_c) FILTER (WHERE temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8) AS temperature_count,
 min(min_temperature_c) FILTER (WHERE min_temperature_c > '-Infinity'::float8 AND min_temperature_c < 'Infinity'::float8) AS temperature_min,
 max(max_temperature_c) FILTER (WHERE max_temperature_c > '-Infinity'::float8 AND max_temperature_c < 'Infinity'::float8) AS temperature_max,
 min(COALESCE((coverage#>>'{temperature,reporting}')::int,0)) AS temperature_reporting,
 max(COALESCE((coverage#>>'{temperature,expected}')::int,0)) AS temperature_expected,
 bool_and(COALESCE((coverage#>>'{temperature,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{temperature,reporting}')::int,0)=COALESCE((coverage#>>'{temperature,expected}')::int,0) AND COALESCE((coverage#>>'{temperature,expected}')::int,0)>0 AND COALESCE(temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8,false)) AS temperature_complete,
 sum(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_sum,
 count(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_count,
 min(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_min,
 max(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_max,
 min(COALESCE((coverage#>>'{hashrate,reporting}')::int,0)) AS hashrate_reporting,
 max(COALESCE((coverage#>>'{hashrate,expected}')::int,0)) AS hashrate_expected,
 bool_and(COALESCE((coverage#>>'{hashrate,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{hashrate,reporting}')::int,0)=COALESCE((coverage#>>'{hashrate,expected}')::int,0) AND COALESCE((coverage#>>'{hashrate,expected}')::int,0)>0 AND COALESCE(hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8,false)) AS hashrate_complete,
 sum(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_sum,
 count(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_count,
 min(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_min,
 max(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_max,
 min(COALESCE((coverage#>>'{power,reporting}')::int,0)) AS power_reporting,
 max(COALESCE((coverage#>>'{power,expected}')::int,0)) AS power_expected,
 bool_and(COALESCE((coverage#>>'{power,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{power,reporting}')::int,0)=COALESCE((coverage#>>'{power,expected}')::int,0) AND COALESCE((coverage#>>'{power,expected}')::int,0)>0 AND COALESCE(power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8,false)) AS power_complete,
 sum(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_sum,
 count(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_count,
 min(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_min,
 max(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_max,
 min(COALESCE((coverage#>>'{hashrate,reporting}')::int,0)) AS efficiency_reporting,
 max(COALESCE((coverage#>>'{hashrate,expected}')::int,0)) AS efficiency_expected,
 bool_and(COALESCE((coverage#>>'{hashrate,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{hashrate,reporting}')::int,0)=COALESCE((coverage#>>'{hashrate,expected}')::int,0) AND COALESCE((coverage#>>'{hashrate,expected}')::int,0)>0 AND COALESCE(efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8,false)) AS efficiency_complete
FROM container_tank_metrics WHERE coverage IS NOT NULL AND topology_version_id IS NOT NULL
GROUP BY bucket,tank_id,site_id,topology_version_id WITH NO DATA;
CREATE INDEX idx_container_tank_metrics_daily_equipment_bucket ON container_tank_metrics_daily(tank_id,bucket DESC);

-- Separate topology versions and sites even when a move falls inside a bucket.
CREATE MATERIALIZED VIEW container_module_metrics_hourly
WITH (timescaledb.continuous, timescaledb.materialized_only=false) AS
SELECT time_bucket(INTERVAL '1 hour',time) AS bucket,module_id,site_id,topology_version_id,
 sum(temperature_c) FILTER (WHERE temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8) AS temperature_sum,
 count(temperature_c) FILTER (WHERE temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8) AS temperature_count,
 min(temperature_c) FILTER (WHERE temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8) AS temperature_min,
 max(temperature_c) FILTER (WHERE temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8) AS temperature_max,
 min(COALESCE((coverage#>>'{temperature,reporting}')::int,0)) AS temperature_reporting,
 max(COALESCE((coverage#>>'{temperature,expected}')::int,0)) AS temperature_expected,
 bool_and(COALESCE((coverage#>>'{temperature,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{temperature,reporting}')::int,0)=COALESCE((coverage#>>'{temperature,expected}')::int,0) AND COALESCE((coverage#>>'{temperature,expected}')::int,0)>0 AND COALESCE(temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8,false)) AS temperature_complete,
 sum(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_sum,
 count(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_count,
 min(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_min,
 max(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_max,
 min(COALESCE((coverage#>>'{hashrate,reporting}')::int,0)) AS hashrate_reporting,
 max(COALESCE((coverage#>>'{hashrate,expected}')::int,0)) AS hashrate_expected,
 bool_and(COALESCE((coverage#>>'{hashrate,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{hashrate,reporting}')::int,0)=COALESCE((coverage#>>'{hashrate,expected}')::int,0) AND COALESCE((coverage#>>'{hashrate,expected}')::int,0)>0 AND COALESCE(hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8,false)) AS hashrate_complete,
 sum(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_sum,
 count(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_count,
 min(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_min,
 max(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_max,
 min(COALESCE((coverage#>>'{power,reporting}')::int,0)) AS power_reporting,
 max(COALESCE((coverage#>>'{power,expected}')::int,0)) AS power_expected,
 bool_and(COALESCE((coverage#>>'{power,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{power,reporting}')::int,0)=COALESCE((coverage#>>'{power,expected}')::int,0) AND COALESCE((coverage#>>'{power,expected}')::int,0)>0 AND COALESCE(power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8,false)) AS power_complete,
 sum(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_sum,
 count(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_count,
 min(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_min,
 max(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_max,
 min(COALESCE((coverage#>>'{hashrate,reporting}')::int,0)) AS efficiency_reporting,
 max(COALESCE((coverage#>>'{hashrate,expected}')::int,0)) AS efficiency_expected,
 bool_and(COALESCE((coverage#>>'{hashrate,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{hashrate,reporting}')::int,0)=COALESCE((coverage#>>'{hashrate,expected}')::int,0) AND COALESCE((coverage#>>'{hashrate,expected}')::int,0)>0 AND COALESCE(efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8,false)) AS efficiency_complete
FROM container_module_metrics WHERE coverage IS NOT NULL AND topology_version_id IS NOT NULL
GROUP BY bucket,module_id,site_id,topology_version_id WITH NO DATA;
CREATE INDEX idx_container_module_metrics_hourly_equipment_bucket ON container_module_metrics_hourly(module_id,bucket DESC);

-- Separate topology versions and sites even when a move falls inside a bucket.
CREATE MATERIALIZED VIEW container_module_metrics_daily
WITH (timescaledb.continuous, timescaledb.materialized_only=false) AS
SELECT time_bucket(INTERVAL '1 day',time) AS bucket,module_id,site_id,topology_version_id,
 sum(temperature_c) FILTER (WHERE temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8) AS temperature_sum,
 count(temperature_c) FILTER (WHERE temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8) AS temperature_count,
 min(temperature_c) FILTER (WHERE temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8) AS temperature_min,
 max(temperature_c) FILTER (WHERE temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8) AS temperature_max,
 min(COALESCE((coverage#>>'{temperature,reporting}')::int,0)) AS temperature_reporting,
 max(COALESCE((coverage#>>'{temperature,expected}')::int,0)) AS temperature_expected,
 bool_and(COALESCE((coverage#>>'{temperature,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{temperature,reporting}')::int,0)=COALESCE((coverage#>>'{temperature,expected}')::int,0) AND COALESCE((coverage#>>'{temperature,expected}')::int,0)>0 AND COALESCE(temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8,false)) AS temperature_complete,
 sum(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_sum,
 count(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_count,
 min(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_min,
 max(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_max,
 min(COALESCE((coverage#>>'{hashrate,reporting}')::int,0)) AS hashrate_reporting,
 max(COALESCE((coverage#>>'{hashrate,expected}')::int,0)) AS hashrate_expected,
 bool_and(COALESCE((coverage#>>'{hashrate,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{hashrate,reporting}')::int,0)=COALESCE((coverage#>>'{hashrate,expected}')::int,0) AND COALESCE((coverage#>>'{hashrate,expected}')::int,0)>0 AND COALESCE(hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8,false)) AS hashrate_complete,
 sum(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_sum,
 count(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_count,
 min(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_min,
 max(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_max,
 min(COALESCE((coverage#>>'{power,reporting}')::int,0)) AS power_reporting,
 max(COALESCE((coverage#>>'{power,expected}')::int,0)) AS power_expected,
 bool_and(COALESCE((coverage#>>'{power,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{power,reporting}')::int,0)=COALESCE((coverage#>>'{power,expected}')::int,0) AND COALESCE((coverage#>>'{power,expected}')::int,0)>0 AND COALESCE(power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8,false)) AS power_complete,
 sum(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_sum,
 count(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_count,
 min(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_min,
 max(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_max,
 min(COALESCE((coverage#>>'{hashrate,reporting}')::int,0)) AS efficiency_reporting,
 max(COALESCE((coverage#>>'{hashrate,expected}')::int,0)) AS efficiency_expected,
 bool_and(COALESCE((coverage#>>'{hashrate,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{hashrate,reporting}')::int,0)=COALESCE((coverage#>>'{hashrate,expected}')::int,0) AND COALESCE((coverage#>>'{hashrate,expected}')::int,0)>0 AND COALESCE(efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8,false)) AS efficiency_complete
FROM container_module_metrics WHERE coverage IS NOT NULL AND topology_version_id IS NOT NULL
GROUP BY bucket,module_id,site_id,topology_version_id WITH NO DATA;
CREATE INDEX idx_container_module_metrics_daily_equipment_bucket ON container_module_metrics_daily(module_id,bucket DESC);

-- One typed read surface; resolution filters prune unused UNION branches.
CREATE VIEW container_equipment_history AS
SELECT 2::int AS equipment_kind,device_identifier::text AS container_identifier,NULL::bigint AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,time AS bucket,time AS collected_at,0::int AS resolution_seconds,1::int AS measurement_type,
 CASE WHEN temp_c > '-Infinity'::float8 AND temp_c < 'Infinity'::float8 THEN temp_c END::float8 AS total,CASE WHEN temp_c > '-Infinity'::float8 AND temp_c < 'Infinity'::float8 THEN 1 ELSE 0 END::bigint AS samples,
 CASE WHEN temp_c > '-Infinity'::float8 AND temp_c < 'Infinity'::float8 THEN temp_c END::float8 AS minimum,CASE WHEN temp_c > '-Infinity'::float8 AND temp_c < 'Infinity'::float8 THEN temp_c END::float8 AS maximum,
 COALESCE((coverage#>>'{temperature,reporting}')::int,0) AS reporting_count,COALESCE((coverage#>>'{temperature,expected}')::int,0) AS expected_count,(COALESCE((coverage#>>'{temperature,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{temperature,reporting}')::int,0)=COALESCE((coverage#>>'{temperature,expected}')::int,0) AND COALESCE((coverage#>>'{temperature,expected}')::int,0)>0 AND COALESCE(temp_c > '-Infinity'::float8 AND temp_c < 'Infinity'::float8,false)) AS coverage_complete
FROM device_metrics WHERE metric_semantics=2
UNION ALL
SELECT 2::int AS equipment_kind,device_identifier::text AS container_identifier,NULL::bigint AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,time AS bucket,time AS collected_at,0::int AS resolution_seconds,2::int AS measurement_type,
 CASE WHEN hash_rate_hs > '-Infinity'::float8 AND hash_rate_hs < 'Infinity'::float8 THEN hash_rate_hs END::float8 AS total,CASE WHEN hash_rate_hs > '-Infinity'::float8 AND hash_rate_hs < 'Infinity'::float8 THEN 1 ELSE 0 END::bigint AS samples,
 CASE WHEN hash_rate_hs > '-Infinity'::float8 AND hash_rate_hs < 'Infinity'::float8 THEN hash_rate_hs END::float8 AS minimum,CASE WHEN hash_rate_hs > '-Infinity'::float8 AND hash_rate_hs < 'Infinity'::float8 THEN hash_rate_hs END::float8 AS maximum,
 COALESCE((coverage#>>'{hashrate,reporting}')::int,0) AS reporting_count,COALESCE((coverage#>>'{hashrate,expected}')::int,0) AS expected_count,(COALESCE((coverage#>>'{hashrate,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{hashrate,reporting}')::int,0)=COALESCE((coverage#>>'{hashrate,expected}')::int,0) AND COALESCE((coverage#>>'{hashrate,expected}')::int,0)>0 AND COALESCE(hash_rate_hs > '-Infinity'::float8 AND hash_rate_hs < 'Infinity'::float8,false)) AS coverage_complete
FROM device_metrics WHERE metric_semantics=2
UNION ALL
SELECT 2::int AS equipment_kind,device_identifier::text AS container_identifier,NULL::bigint AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,time AS bucket,time AS collected_at,0::int AS resolution_seconds,3::int AS measurement_type,
 CASE WHEN power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8 THEN power_w END::float8 AS total,CASE WHEN power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8 THEN 1 ELSE 0 END::bigint AS samples,
 CASE WHEN power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8 THEN power_w END::float8 AS minimum,CASE WHEN power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8 THEN power_w END::float8 AS maximum,
 COALESCE((coverage#>>'{power,reporting}')::int,0) AS reporting_count,COALESCE((coverage#>>'{power,expected}')::int,0) AS expected_count,(COALESCE((coverage#>>'{power,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{power,reporting}')::int,0)=COALESCE((coverage#>>'{power,expected}')::int,0) AND COALESCE((coverage#>>'{power,expected}')::int,0)>0 AND COALESCE(power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8,false)) AS coverage_complete
FROM device_metrics WHERE metric_semantics=2
UNION ALL
SELECT 2::int AS equipment_kind,device_identifier::text AS container_identifier,NULL::bigint AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,time AS bucket,time AS collected_at,0::int AS resolution_seconds,4::int AS measurement_type,
 CASE WHEN efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8 THEN efficiency_jh END::float8 AS total,CASE WHEN efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8 THEN 1 ELSE 0 END::bigint AS samples,
 CASE WHEN efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8 THEN efficiency_jh END::float8 AS minimum,CASE WHEN efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8 THEN efficiency_jh END::float8 AS maximum,
 COALESCE((coverage#>>'{hashrate,reporting}')::int,0) AS reporting_count,COALESCE((coverage#>>'{hashrate,expected}')::int,0) AS expected_count,(COALESCE((coverage#>>'{hashrate,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{hashrate,reporting}')::int,0)=COALESCE((coverage#>>'{hashrate,expected}')::int,0) AND COALESCE((coverage#>>'{hashrate,expected}')::int,0)>0 AND COALESCE(efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8,false)) AS coverage_complete
FROM device_metrics WHERE metric_semantics=2
UNION ALL
SELECT 2::int,device_identifier::text AS container_identifier,NULL::bigint AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,bucket,bucket,3600::int,1::int,
 temperature_sum::float8,temperature_count::bigint,temperature_min::float8,temperature_max::float8,temperature_reporting::int,temperature_expected::int,temperature_complete::boolean
FROM container_metrics_hourly
UNION ALL
SELECT 2::int,device_identifier::text AS container_identifier,NULL::bigint AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,bucket,bucket,3600::int,2::int,
 hashrate_sum::float8,hashrate_count::bigint,hashrate_min::float8,hashrate_max::float8,hashrate_reporting::int,hashrate_expected::int,hashrate_complete::boolean
FROM container_metrics_hourly
UNION ALL
SELECT 2::int,device_identifier::text AS container_identifier,NULL::bigint AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,bucket,bucket,3600::int,3::int,
 power_sum::float8,power_count::bigint,power_min::float8,power_max::float8,power_reporting::int,power_expected::int,power_complete::boolean
FROM container_metrics_hourly
UNION ALL
SELECT 2::int,device_identifier::text AS container_identifier,NULL::bigint AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,bucket,bucket,3600::int,4::int,
 efficiency_sum::float8,efficiency_count::bigint,efficiency_min::float8,efficiency_max::float8,efficiency_reporting::int,efficiency_expected::int,efficiency_complete::boolean
FROM container_metrics_hourly
UNION ALL
SELECT 2::int,device_identifier::text AS container_identifier,NULL::bigint AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,bucket,bucket,86400::int,1::int,
 temperature_sum::float8,temperature_count::bigint,temperature_min::float8,temperature_max::float8,temperature_reporting::int,temperature_expected::int,temperature_complete::boolean
FROM container_metrics_daily
UNION ALL
SELECT 2::int,device_identifier::text AS container_identifier,NULL::bigint AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,bucket,bucket,86400::int,2::int,
 hashrate_sum::float8,hashrate_count::bigint,hashrate_min::float8,hashrate_max::float8,hashrate_reporting::int,hashrate_expected::int,hashrate_complete::boolean
FROM container_metrics_daily
UNION ALL
SELECT 2::int,device_identifier::text AS container_identifier,NULL::bigint AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,bucket,bucket,86400::int,3::int,
 power_sum::float8,power_count::bigint,power_min::float8,power_max::float8,power_reporting::int,power_expected::int,power_complete::boolean
FROM container_metrics_daily
UNION ALL
SELECT 2::int,device_identifier::text AS container_identifier,NULL::bigint AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,bucket,bucket,86400::int,4::int,
 efficiency_sum::float8,efficiency_count::bigint,efficiency_min::float8,efficiency_max::float8,efficiency_reporting::int,efficiency_expected::int,efficiency_complete::boolean
FROM container_metrics_daily
UNION ALL
SELECT 4::int AS equipment_kind,NULL::text AS container_identifier,tank_id AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,time AS bucket,time AS collected_at,0::int AS resolution_seconds,1::int AS measurement_type,
 CASE WHEN temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8 THEN temperature_c END::float8 AS total,CASE WHEN temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8 THEN 1 ELSE 0 END::bigint AS samples,
 CASE WHEN min_temperature_c > '-Infinity'::float8 AND min_temperature_c < 'Infinity'::float8 THEN min_temperature_c END::float8 AS minimum,CASE WHEN max_temperature_c > '-Infinity'::float8 AND max_temperature_c < 'Infinity'::float8 THEN max_temperature_c END::float8 AS maximum,
 COALESCE((coverage#>>'{temperature,reporting}')::int,0) AS reporting_count,COALESCE((coverage#>>'{temperature,expected}')::int,0) AS expected_count,(COALESCE((coverage#>>'{temperature,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{temperature,reporting}')::int,0)=COALESCE((coverage#>>'{temperature,expected}')::int,0) AND COALESCE((coverage#>>'{temperature,expected}')::int,0)>0 AND COALESCE(temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8,false)) AS coverage_complete
FROM container_tank_metrics
UNION ALL
SELECT 4::int AS equipment_kind,NULL::text AS container_identifier,tank_id AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,time AS bucket,time AS collected_at,0::int AS resolution_seconds,2::int AS measurement_type,
 CASE WHEN hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8 THEN hashrate_hs END::float8 AS total,CASE WHEN hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8 THEN 1 ELSE 0 END::bigint AS samples,
 CASE WHEN hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8 THEN hashrate_hs END::float8 AS minimum,CASE WHEN hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8 THEN hashrate_hs END::float8 AS maximum,
 COALESCE((coverage#>>'{hashrate,reporting}')::int,0) AS reporting_count,COALESCE((coverage#>>'{hashrate,expected}')::int,0) AS expected_count,(COALESCE((coverage#>>'{hashrate,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{hashrate,reporting}')::int,0)=COALESCE((coverage#>>'{hashrate,expected}')::int,0) AND COALESCE((coverage#>>'{hashrate,expected}')::int,0)>0 AND COALESCE(hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8,false)) AS coverage_complete
FROM container_tank_metrics
UNION ALL
SELECT 4::int AS equipment_kind,NULL::text AS container_identifier,tank_id AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,time AS bucket,time AS collected_at,0::int AS resolution_seconds,3::int AS measurement_type,
 CASE WHEN power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8 THEN power_w END::float8 AS total,CASE WHEN power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8 THEN 1 ELSE 0 END::bigint AS samples,
 CASE WHEN power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8 THEN power_w END::float8 AS minimum,CASE WHEN power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8 THEN power_w END::float8 AS maximum,
 COALESCE((coverage#>>'{power,reporting}')::int,0) AS reporting_count,COALESCE((coverage#>>'{power,expected}')::int,0) AS expected_count,(COALESCE((coverage#>>'{power,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{power,reporting}')::int,0)=COALESCE((coverage#>>'{power,expected}')::int,0) AND COALESCE((coverage#>>'{power,expected}')::int,0)>0 AND COALESCE(power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8,false)) AS coverage_complete
FROM container_tank_metrics
UNION ALL
SELECT 4::int AS equipment_kind,NULL::text AS container_identifier,tank_id AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,time AS bucket,time AS collected_at,0::int AS resolution_seconds,4::int AS measurement_type,
 CASE WHEN efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8 THEN efficiency_jh END::float8 AS total,CASE WHEN efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8 THEN 1 ELSE 0 END::bigint AS samples,
 CASE WHEN efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8 THEN efficiency_jh END::float8 AS minimum,CASE WHEN efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8 THEN efficiency_jh END::float8 AS maximum,
 COALESCE((coverage#>>'{hashrate,reporting}')::int,0) AS reporting_count,COALESCE((coverage#>>'{hashrate,expected}')::int,0) AS expected_count,(COALESCE((coverage#>>'{hashrate,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{hashrate,reporting}')::int,0)=COALESCE((coverage#>>'{hashrate,expected}')::int,0) AND COALESCE((coverage#>>'{hashrate,expected}')::int,0)>0 AND COALESCE(efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8,false)) AS coverage_complete
FROM container_tank_metrics
UNION ALL
SELECT 4::int,NULL::text AS container_identifier,tank_id AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,bucket,bucket,3600::int,1::int,
 temperature_sum::float8,temperature_count::bigint,temperature_min::float8,temperature_max::float8,temperature_reporting::int,temperature_expected::int,temperature_complete::boolean
FROM container_tank_metrics_hourly
UNION ALL
SELECT 4::int,NULL::text AS container_identifier,tank_id AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,bucket,bucket,3600::int,2::int,
 hashrate_sum::float8,hashrate_count::bigint,hashrate_min::float8,hashrate_max::float8,hashrate_reporting::int,hashrate_expected::int,hashrate_complete::boolean
FROM container_tank_metrics_hourly
UNION ALL
SELECT 4::int,NULL::text AS container_identifier,tank_id AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,bucket,bucket,3600::int,3::int,
 power_sum::float8,power_count::bigint,power_min::float8,power_max::float8,power_reporting::int,power_expected::int,power_complete::boolean
FROM container_tank_metrics_hourly
UNION ALL
SELECT 4::int,NULL::text AS container_identifier,tank_id AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,bucket,bucket,3600::int,4::int,
 efficiency_sum::float8,efficiency_count::bigint,efficiency_min::float8,efficiency_max::float8,efficiency_reporting::int,efficiency_expected::int,efficiency_complete::boolean
FROM container_tank_metrics_hourly
UNION ALL
SELECT 4::int,NULL::text AS container_identifier,tank_id AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,bucket,bucket,86400::int,1::int,
 temperature_sum::float8,temperature_count::bigint,temperature_min::float8,temperature_max::float8,temperature_reporting::int,temperature_expected::int,temperature_complete::boolean
FROM container_tank_metrics_daily
UNION ALL
SELECT 4::int,NULL::text AS container_identifier,tank_id AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,bucket,bucket,86400::int,2::int,
 hashrate_sum::float8,hashrate_count::bigint,hashrate_min::float8,hashrate_max::float8,hashrate_reporting::int,hashrate_expected::int,hashrate_complete::boolean
FROM container_tank_metrics_daily
UNION ALL
SELECT 4::int,NULL::text AS container_identifier,tank_id AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,bucket,bucket,86400::int,3::int,
 power_sum::float8,power_count::bigint,power_min::float8,power_max::float8,power_reporting::int,power_expected::int,power_complete::boolean
FROM container_tank_metrics_daily
UNION ALL
SELECT 4::int,NULL::text AS container_identifier,tank_id AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,bucket,bucket,86400::int,4::int,
 efficiency_sum::float8,efficiency_count::bigint,efficiency_min::float8,efficiency_max::float8,efficiency_reporting::int,efficiency_expected::int,efficiency_complete::boolean
FROM container_tank_metrics_daily
UNION ALL
SELECT 6::int AS equipment_kind,NULL::text AS container_identifier,NULL::bigint AS tank_id,module_id AS module_id,site_id,topology_version_id,time AS bucket,collected_at AS collected_at,0::int AS resolution_seconds,1::int AS measurement_type,
 CASE WHEN temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8 THEN temperature_c END::float8 AS total,CASE WHEN temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8 THEN 1 ELSE 0 END::bigint AS samples,
 CASE WHEN temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8 THEN temperature_c END::float8 AS minimum,CASE WHEN temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8 THEN temperature_c END::float8 AS maximum,
 COALESCE((coverage#>>'{temperature,reporting}')::int,0) AS reporting_count,COALESCE((coverage#>>'{temperature,expected}')::int,0) AS expected_count,(COALESCE((coverage#>>'{temperature,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{temperature,reporting}')::int,0)=COALESCE((coverage#>>'{temperature,expected}')::int,0) AND COALESCE((coverage#>>'{temperature,expected}')::int,0)>0 AND COALESCE(temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8,false)) AS coverage_complete
FROM container_module_metrics
UNION ALL
SELECT 6::int AS equipment_kind,NULL::text AS container_identifier,NULL::bigint AS tank_id,module_id AS module_id,site_id,topology_version_id,time AS bucket,collected_at AS collected_at,0::int AS resolution_seconds,2::int AS measurement_type,
 CASE WHEN hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8 THEN hashrate_hs END::float8 AS total,CASE WHEN hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8 THEN 1 ELSE 0 END::bigint AS samples,
 CASE WHEN hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8 THEN hashrate_hs END::float8 AS minimum,CASE WHEN hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8 THEN hashrate_hs END::float8 AS maximum,
 COALESCE((coverage#>>'{hashrate,reporting}')::int,0) AS reporting_count,COALESCE((coverage#>>'{hashrate,expected}')::int,0) AS expected_count,(COALESCE((coverage#>>'{hashrate,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{hashrate,reporting}')::int,0)=COALESCE((coverage#>>'{hashrate,expected}')::int,0) AND COALESCE((coverage#>>'{hashrate,expected}')::int,0)>0 AND COALESCE(hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8,false)) AS coverage_complete
FROM container_module_metrics
UNION ALL
SELECT 6::int AS equipment_kind,NULL::text AS container_identifier,NULL::bigint AS tank_id,module_id AS module_id,site_id,topology_version_id,time AS bucket,collected_at AS collected_at,0::int AS resolution_seconds,3::int AS measurement_type,
 CASE WHEN power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8 THEN power_w END::float8 AS total,CASE WHEN power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8 THEN 1 ELSE 0 END::bigint AS samples,
 CASE WHEN power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8 THEN power_w END::float8 AS minimum,CASE WHEN power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8 THEN power_w END::float8 AS maximum,
 COALESCE((coverage#>>'{power,reporting}')::int,0) AS reporting_count,COALESCE((coverage#>>'{power,expected}')::int,0) AS expected_count,(COALESCE((coverage#>>'{power,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{power,reporting}')::int,0)=COALESCE((coverage#>>'{power,expected}')::int,0) AND COALESCE((coverage#>>'{power,expected}')::int,0)>0 AND COALESCE(power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8,false)) AS coverage_complete
FROM container_module_metrics
UNION ALL
SELECT 6::int AS equipment_kind,NULL::text AS container_identifier,NULL::bigint AS tank_id,module_id AS module_id,site_id,topology_version_id,time AS bucket,collected_at AS collected_at,0::int AS resolution_seconds,4::int AS measurement_type,
 CASE WHEN efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8 THEN efficiency_jh END::float8 AS total,CASE WHEN efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8 THEN 1 ELSE 0 END::bigint AS samples,
 CASE WHEN efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8 THEN efficiency_jh END::float8 AS minimum,CASE WHEN efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8 THEN efficiency_jh END::float8 AS maximum,
 COALESCE((coverage#>>'{hashrate,reporting}')::int,0) AS reporting_count,COALESCE((coverage#>>'{hashrate,expected}')::int,0) AS expected_count,(COALESCE((coverage#>>'{hashrate,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{hashrate,reporting}')::int,0)=COALESCE((coverage#>>'{hashrate,expected}')::int,0) AND COALESCE((coverage#>>'{hashrate,expected}')::int,0)>0 AND COALESCE(efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8,false)) AS coverage_complete
FROM container_module_metrics
UNION ALL
SELECT 6::int,NULL::text AS container_identifier,NULL::bigint AS tank_id,module_id AS module_id,site_id,topology_version_id,bucket,bucket,3600::int,1::int,
 temperature_sum::float8,temperature_count::bigint,temperature_min::float8,temperature_max::float8,temperature_reporting::int,temperature_expected::int,temperature_complete::boolean
FROM container_module_metrics_hourly
UNION ALL
SELECT 6::int,NULL::text AS container_identifier,NULL::bigint AS tank_id,module_id AS module_id,site_id,topology_version_id,bucket,bucket,3600::int,2::int,
 hashrate_sum::float8,hashrate_count::bigint,hashrate_min::float8,hashrate_max::float8,hashrate_reporting::int,hashrate_expected::int,hashrate_complete::boolean
FROM container_module_metrics_hourly
UNION ALL
SELECT 6::int,NULL::text AS container_identifier,NULL::bigint AS tank_id,module_id AS module_id,site_id,topology_version_id,bucket,bucket,3600::int,3::int,
 power_sum::float8,power_count::bigint,power_min::float8,power_max::float8,power_reporting::int,power_expected::int,power_complete::boolean
FROM container_module_metrics_hourly
UNION ALL
SELECT 6::int,NULL::text AS container_identifier,NULL::bigint AS tank_id,module_id AS module_id,site_id,topology_version_id,bucket,bucket,3600::int,4::int,
 efficiency_sum::float8,efficiency_count::bigint,efficiency_min::float8,efficiency_max::float8,efficiency_reporting::int,efficiency_expected::int,efficiency_complete::boolean
FROM container_module_metrics_hourly
UNION ALL
SELECT 6::int,NULL::text AS container_identifier,NULL::bigint AS tank_id,module_id AS module_id,site_id,topology_version_id,bucket,bucket,86400::int,1::int,
 temperature_sum::float8,temperature_count::bigint,temperature_min::float8,temperature_max::float8,temperature_reporting::int,temperature_expected::int,temperature_complete::boolean
FROM container_module_metrics_daily
UNION ALL
SELECT 6::int,NULL::text AS container_identifier,NULL::bigint AS tank_id,module_id AS module_id,site_id,topology_version_id,bucket,bucket,86400::int,2::int,
 hashrate_sum::float8,hashrate_count::bigint,hashrate_min::float8,hashrate_max::float8,hashrate_reporting::int,hashrate_expected::int,hashrate_complete::boolean
FROM container_module_metrics_daily
UNION ALL
SELECT 6::int,NULL::text AS container_identifier,NULL::bigint AS tank_id,module_id AS module_id,site_id,topology_version_id,bucket,bucket,86400::int,3::int,
 power_sum::float8,power_count::bigint,power_min::float8,power_max::float8,power_reporting::int,power_expected::int,power_complete::boolean
FROM container_module_metrics_daily
UNION ALL
SELECT 6::int,NULL::text AS container_identifier,NULL::bigint AS tank_id,module_id AS module_id,site_id,topology_version_id,bucket,bucket,86400::int,4::int,
 efficiency_sum::float8,efficiency_count::bigint,efficiency_min::float8,efficiency_max::float8,efficiency_reporting::int,efficiency_expected::int,efficiency_complete::boolean
FROM container_module_metrics_daily;

-- Match miner routing, also honoring the available tier for old narrow windows.
CREATE FUNCTION container_history_resolution(start_time timestamptz,end_time timestamptz)
RETURNS integer LANGUAGE sql STABLE AS $$
 SELECT CASE WHEN end_time-start_time <= INTERVAL '1 day' AND start_time >= now()-INTERVAL '10 days' THEN 0
 WHEN end_time-start_time <= INTERVAL '10 days' AND start_time >= now()-INTERVAL '3 months' THEN 3600
 ELSE 86400 END;
$$;

-- Timescale refresh cannot run inside a migration transaction. A scheduled
-- procedure backfills first, then enables policies and removes its own job.
-- Failed backfills retry with raw retention disabled, preserving the evidence.
CREATE PROCEDURE initialize_container_metric_summaries(job_id integer,config jsonb)
LANGUAGE plpgsql AS $$
BEGIN

 CALL refresh_continuous_aggregate('container_metrics_hourly',NULL,now());
 CALL refresh_continuous_aggregate('container_metrics_daily',NULL,now());
 CALL refresh_continuous_aggregate('container_tank_metrics_hourly',NULL,now());
 CALL refresh_continuous_aggregate('container_tank_metrics_daily',NULL,now());
 CALL refresh_continuous_aggregate('container_module_metrics_hourly',NULL,now());
 CALL refresh_continuous_aggregate('container_module_metrics_daily',NULL,now());
 PERFORM add_continuous_aggregate_policy('container_metrics_hourly',start_offset=>INTERVAL '1 day',end_offset=>INTERVAL '1 hour',schedule_interval=>INTERVAL '30 minutes',if_not_exists=>true);
 PERFORM add_retention_policy('container_metrics_hourly',INTERVAL '3 months',if_not_exists=>true);
 PERFORM add_continuous_aggregate_policy('container_metrics_daily',start_offset=>INTERVAL '7 days',end_offset=>INTERVAL '1 day',schedule_interval=>INTERVAL '6 hours',if_not_exists=>true);
 PERFORM add_retention_policy('container_metrics_daily',INTERVAL '3 years',if_not_exists=>true);
 PERFORM add_continuous_aggregate_policy('container_tank_metrics_hourly',start_offset=>INTERVAL '1 day',end_offset=>INTERVAL '1 hour',schedule_interval=>INTERVAL '30 minutes',if_not_exists=>true);
 PERFORM add_retention_policy('container_tank_metrics_hourly',INTERVAL '3 months',if_not_exists=>true);
 PERFORM add_continuous_aggregate_policy('container_tank_metrics_daily',start_offset=>INTERVAL '7 days',end_offset=>INTERVAL '1 day',schedule_interval=>INTERVAL '6 hours',if_not_exists=>true);
 PERFORM add_retention_policy('container_tank_metrics_daily',INTERVAL '3 years',if_not_exists=>true);
 PERFORM add_continuous_aggregate_policy('container_module_metrics_hourly',start_offset=>INTERVAL '1 day',end_offset=>INTERVAL '1 hour',schedule_interval=>INTERVAL '30 minutes',if_not_exists=>true);
 PERFORM add_retention_policy('container_module_metrics_hourly',INTERVAL '3 months',if_not_exists=>true);
 PERFORM add_continuous_aggregate_policy('container_module_metrics_daily',start_offset=>INTERVAL '7 days',end_offset=>INTERVAL '1 day',schedule_interval=>INTERVAL '6 hours',if_not_exists=>true);
 PERFORM add_retention_policy('container_module_metrics_daily',INTERVAL '3 years',if_not_exists=>true);
 PERFORM add_retention_policy('device_metrics',INTERVAL '10 days',if_not_exists=>true);
 PERFORM add_retention_policy('container_tank_metrics',INTERVAL '10 days',if_not_exists=>true);
 PERFORM add_retention_policy('container_module_metrics',INTERVAL '10 days',if_not_exists=>true);
 PERFORM delete_job(job_id);
END;
$$;
SELECT add_job('initialize_container_metric_summaries',INTERVAL '1 hour',initial_start=>now()+INTERVAL '1 minute');
