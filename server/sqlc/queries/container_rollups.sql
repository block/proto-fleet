-- name: InsertContainerTopologyVersion :exec
INSERT INTO container_topology_version(device_id, fingerprint, topology)
VALUES (sqlc.arg('device_id'), sqlc.arg('fingerprint'), sqlc.arg('topology'))
ON CONFLICT(device_id, fingerprint) DO NOTHING;

-- name: GetContainerTopologyVersionID :one
SELECT id FROM container_topology_version
WHERE device_id=sqlc.arg('device_id') AND fingerprint=sqlc.arg('fingerprint');

-- Board health time can precede the poll. Stamp only samples inserted by this
-- collection. The function groups by exact source time and supplies explicit
-- board/time filters before Timescale decompresses batches, including generic plans.
-- name: StampHashboardTopology :exec
SELECT stamp_hashboard_topology(sqlc.arg('version_id')::bigint,
 sqlc.arg('collected_at')::timestamptz,sqlc.arg('samples')::jsonb);

-- name: InsertContainerTopologyObservation :one
INSERT INTO container_topology_observation(device_id, version_id, collected_at, complete)
VALUES (sqlc.arg('device_id'), sqlc.arg('version_id'), sqlc.arg('collected_at'), sqlc.arg('complete'))
RETURNING id;

-- name: GetContainerRollupContext :one
SELECT d.id, d.site_id, d.org_id, d.serial_number FROM device d WHERE d.id=sqlc.arg('device_id');

-- name: GetTankRollupWatermarks :many
SELECT t.position, lm.source_times FROM container_tank t
JOIN container_tank_latest_metrics lm ON lm.tank_id=t.id WHERE t.device_id=sqlc.arg('device_id');

-- name: GetContainerRollupWatermarks :one
SELECT source_times FROM container_latest_metrics WHERE device_id=sqlc.arg('device_id');

-- name: GetHashboardMetricWatermarks :many
SELECT hashboard_id, time FROM hashboard_latest_metrics WHERE hashboard_id=ANY(sqlc.arg('board_ids')::bigint[]);

-- name: InsertContainerTankRollups :exec
WITH inserted AS (
 INSERT INTO container_tank_metrics(tank_id,time,topology_observation_id,topology_version_id,site_id,hashrate_hs,power_w,efficiency_jh,temperature_c,min_temperature_c,max_temperature_c,coverage)
 SELECT t.id,sqlc.arg('time')::timestamptz,sqlc.arg('observation_id')::bigint,o.version_id,d.site_id,
 x.hashrate_hs,x.power_w,x.efficiency_jh,x.temperature_c,x.min_temperature_c,x.max_temperature_c,x.coverage
 FROM jsonb_to_recordset(sqlc.arg('samples')::jsonb) x(position int,hashrate_hs double precision,power_w double precision,efficiency_jh double precision,temperature_c double precision,min_temperature_c double precision,max_temperature_c double precision,coverage jsonb)
 JOIN container_tank t ON t.device_id=sqlc.arg('device_id') AND t.position=x.position
 JOIN container_scope d ON d.id=t.device_id
 JOIN container_topology_observation o ON o.id=sqlc.arg('observation_id')
 ON CONFLICT(tank_id,time) DO NOTHING RETURNING *
)
INSERT INTO container_tank_latest_metrics(tank_id,time,topology_observation_id,hashrate_hs,power_w,efficiency_jh,temperature_c,min_temperature_c,max_temperature_c,coverage,source_times)
SELECT tank_id,time,topology_observation_id,hashrate_hs,power_w,efficiency_jh,temperature_c,min_temperature_c,max_temperature_c,coverage,COALESCE(NULLIF(coverage->'source_times','null'::jsonb),'{}'::jsonb) FROM inserted
ON CONFLICT(tank_id) DO UPDATE SET time=EXCLUDED.time,topology_observation_id=EXCLUDED.topology_observation_id,
 hashrate_hs=EXCLUDED.hashrate_hs,power_w=EXCLUDED.power_w,efficiency_jh=EXCLUDED.efficiency_jh,temperature_c=EXCLUDED.temperature_c,
 min_temperature_c=EXCLUDED.min_temperature_c,max_temperature_c=EXCLUDED.max_temperature_c,coverage=EXCLUDED.coverage,
 source_times=(SELECT COALESCE(jsonb_object_agg(COALESCE(p.key,n.key),GREATEST(p.value::timestamptz,n.value::timestamptz)),'{}'::jsonb)
 FROM jsonb_each_text(container_tank_latest_metrics.source_times) p FULL JOIN jsonb_each_text(EXCLUDED.source_times) n USING(key))
WHERE container_tank_latest_metrics.time < EXCLUDED.time;

-- name: UpsertContainerRollup :exec
INSERT INTO container_latest_metrics(device_id,time,topology_observation_id,hashrate_hs,power_w,efficiency_jh,temperature_c,min_temperature_c,max_temperature_c,coverage,source_times)
SELECT sqlc.arg('device_id'),sqlc.arg('time')::timestamptz,sqlc.arg('observation_id'),x.hashrate_hs,x.power_w,x.efficiency_jh,x.temperature_c,x.min_temperature_c,x.max_temperature_c,sqlc.arg('coverage')::jsonb,COALESCE(NULLIF(sqlc.arg('coverage')::jsonb->'source_times','null'::jsonb),'{}'::jsonb)
FROM jsonb_to_record(sqlc.arg('coverage')::jsonb) x(hashrate_hs double precision,power_w double precision,efficiency_jh double precision,temperature_c double precision,min_temperature_c double precision,max_temperature_c double precision)
ON CONFLICT(device_id) DO UPDATE SET time=EXCLUDED.time,topology_observation_id=EXCLUDED.topology_observation_id,
 hashrate_hs=EXCLUDED.hashrate_hs,power_w=EXCLUDED.power_w,efficiency_jh=EXCLUDED.efficiency_jh,temperature_c=EXCLUDED.temperature_c,
 min_temperature_c=EXCLUDED.min_temperature_c,max_temperature_c=EXCLUDED.max_temperature_c,coverage=EXCLUDED.coverage,
 source_times=(SELECT COALESCE(jsonb_object_agg(COALESCE(p.key,n.key),GREATEST(p.value::timestamptz,n.value::timestamptz)),'{}'::jsonb)
 FROM jsonb_each_text(container_latest_metrics.source_times) p FULL JOIN jsonb_each_text(EXCLUDED.source_times) n USING(key))
WHERE container_latest_metrics.time < EXCLUDED.time;

-- name: StampContainerSample :exec
UPDATE device_metrics SET metric_semantics=2,coverage=lm.coverage,topology_observation_id=lm.topology_observation_id,
 topology_version_id=(SELECT version_id FROM container_topology_observation WHERE id=lm.topology_observation_id),
 hash_rate_hs=lm.hashrate_hs,power_w=lm.power_w,efficiency_jh=lm.efficiency_jh,temp_c=lm.temperature_c
FROM container_latest_metrics lm JOIN container_scope d ON d.id=lm.device_id
WHERE device_metrics.device_identifier=sqlc.arg('device_identifier') AND d.device_identifier=device_metrics.device_identifier
 AND device_metrics.time=sqlc.arg('time')::timestamptz AND lm.time=device_metrics.time;

-- Withdraw readings when the accepted poll has no health. Keep source ordering
-- separately in time; a later older sample must never restore withdrawn values.
-- name: WithdrawHashboardReadings :exec
UPDATE hashboard_latest_metrics lm SET hashrate_hs=NULL,temperature_c=NULL,power_w=NULL,efficiency_jh=NULL
FROM jsonb_to_recordset(sqlc.arg('boards')::jsonb) x(board_id bigint,hashrate_hs double precision,temperature_c double precision,power_w double precision)
WHERE lm.hashboard_id=x.board_id AND lm.collected_at < sqlc.arg('collected_at')::timestamptz
 AND x.hashrate_hs IS NULL AND x.temperature_c IS NULL AND x.power_w IS NULL;

-- name: GetContainerTopologyIdentities :many
SELECT t.position AS tank_position,m.position AS module_position,s.position,s.hashboard_id
FROM container_tank t JOIN container_module m ON m.tank_id=t.id AND m.retired_at IS NULL
JOIN hashboard_slot s ON s.module_id=m.id WHERE t.device_id=sqlc.arg('device_id');

-- name: GetContainerPollHistory :many
WITH resolution AS (
 SELECT container_history_resolution(sqlc.arg('start_time')::timestamptz,sqlc.arg('end_time')::timestamptz) AS seconds
), settings AS (
 SELECT seconds,GREATEST(1,seconds)*ceil(sqlc.arg('granularity_seconds')::int::numeric/GREATEST(1,seconds))::bigint AS width FROM resolution
), readings AS (
 SELECT floor(extract(epoch FROM mm.bucket)/settings.width)::bigint*settings.width AS bucket,
 mm.measurement_type,mm.samples,mm.total,mm.minimum,mm.maximum,mm.reporting_count,mm.expected_count,mm.coverage_complete,
 1::float8 AS scale
 FROM container_equipment_history mm JOIN container_scope d ON d.device_identifier=mm.container_identifier CROSS JOIN settings
 WHERE mm.equipment_kind=2 AND mm.resolution_seconds=settings.seconds AND d.device_identifier=sqlc.arg('container_identifier') AND (sqlc.arg('org_id')::bigint=0 OR d.org_id=sqlc.arg('org_id')) AND d.deleted_at IS NULL
 AND mm.bucket>=CASE WHEN settings.seconds=0 THEN sqlc.arg('start_time')::timestamptz ELSE to_timestamp(floor(extract(epoch FROM sqlc.arg('start_time')::timestamptz)/GREATEST(1,settings.seconds))*GREATEST(1,settings.seconds)) END
 AND mm.bucket<sqlc.arg('end_time')::timestamptz
 AND mm.site_id IS NOT DISTINCT FROM d.site_id
)
SELECT bucket::bigint AS bucket,measurement_type::int AS measurement_type,
 sum(samples)::int AS module_count,
 (sum(samples)>0)::boolean AS has_values,
 (count(minimum)>0)::boolean AS has_minimum,
 (count(maximum)>0)::boolean AS has_maximum,
 COALESCE(sum(total)/NULLIF(sum(samples),0)*min(scale),0)::float8 AS average,
 COALESCE(min(minimum)*min(scale),0)::float8 AS minimum,COALESCE(max(maximum)*min(scale),0)::float8 AS maximum,
 min(reporting_count)::int AS reporting_count,max(expected_count)::int AS expected_count,
 bool_and(coverage_complete)::boolean AS coverage_complete
FROM readings GROUP BY bucket,measurement_type ORDER BY bucket,measurement_type;

-- name: IsContainerMetricSelector :one
SELECT EXISTS(SELECT 1 FROM device d JOIN discovered_device dd ON dd.id=d.discovered_device_id WHERE d.device_identifier=sqlc.arg('identifier') AND dd.device_class=2)::boolean;

-- name: GetModuleRollupWatermarks :many
SELECT t.position AS tank_position,m.position,lm.source_times FROM container_module m
JOIN container_tank t ON t.id=m.tank_id
JOIN container_module_latest_metrics lm ON lm.module_id=m.id
WHERE t.device_id=sqlc.arg('device_id') AND m.retired_at IS NULL;
