-- requires-shared: 1000
ALTER TABLE hashboard_metrics ADD COLUMN power_w double precision, ADD COLUMN efficiency_jh double precision;
ALTER TABLE hashboard_latest_metrics ADD COLUMN power_w double precision, ADD COLUMN efficiency_jh double precision;
ALTER TABLE container_module_metrics ADD COLUMN efficiency_jh double precision, ADD COLUMN coverage jsonb;
ALTER TABLE container_module_latest_metrics ADD COLUMN efficiency_jh double precision, ADD COLUMN coverage jsonb,
 ADD COLUMN source_times jsonb NOT NULL DEFAULT '{}';

CREATE TABLE container_topology_version (
 id bigserial PRIMARY KEY,
 device_id bigint NOT NULL REFERENCES device(id),
 fingerprint text NOT NULL,
 topology jsonb NOT NULL,
 CONSTRAINT uq_container_topology_version_device_fingerprint UNIQUE(device_id, fingerprint)
);
CREATE TABLE container_topology_observation (
 id bigserial PRIMARY KEY,
 device_id bigint NOT NULL REFERENCES device(id),
 version_id bigint NOT NULL REFERENCES container_topology_version(id),
 collected_at timestamptz NOT NULL,
 complete boolean NOT NULL,
 CONSTRAINT uq_container_topology_observation_device_time UNIQUE(device_id, collected_at)
);
CREATE INDEX idx_container_topology_observation_version ON container_topology_observation(version_id);

CREATE TABLE container_tank_metrics (
 tank_id bigint NOT NULL REFERENCES container_tank(id),
 time timestamptz NOT NULL,
 topology_observation_id bigint NOT NULL REFERENCES container_topology_observation(id),
 site_id bigint REFERENCES site(id),
 hashrate_hs double precision,
 power_w double precision,
 efficiency_jh double precision,
 temperature_c double precision,
 min_temperature_c double precision,
 max_temperature_c double precision,
 coverage jsonb NOT NULL,
 CONSTRAINT uq_container_tank_metrics_tank_time UNIQUE(tank_id, time)
);
SELECT create_hypertable('container_tank_metrics','time',chunk_time_interval => INTERVAL '1 day');
SELECT add_retention_policy('container_tank_metrics', INTERVAL '30 days');
CREATE TABLE container_tank_latest_metrics (
 tank_id bigint PRIMARY KEY REFERENCES container_tank(id),
 time timestamptz NOT NULL,
 topology_observation_id bigint NOT NULL REFERENCES container_topology_observation(id),
 hashrate_hs double precision,
 power_w double precision,
 efficiency_jh double precision,
 temperature_c double precision,
 min_temperature_c double precision,
 max_temperature_c double precision,
 coverage jsonb NOT NULL,
 source_times jsonb NOT NULL DEFAULT '{}'
);
ALTER TABLE device_metrics ADD COLUMN metric_semantics smallint NOT NULL DEFAULT 1,
 ADD COLUMN coverage jsonb,
 -- Compressed device_metrics cannot acquire a new FK. The telemetry transaction
 -- writes this reference after creating the observation; topology is retained.
 ADD COLUMN topology_observation_id bigint;
CREATE TABLE container_latest_metrics (
 device_id bigint PRIMARY KEY REFERENCES device(id),
 time timestamptz NOT NULL,
 topology_observation_id bigint NOT NULL REFERENCES container_topology_observation(id),
 hashrate_hs double precision,
 power_w double precision,
 efficiency_jh double precision,
 temperature_c double precision,
 min_temperature_c double precision,
 max_temperature_c double precision,
 coverage jsonb NOT NULL,
 source_times jsonb NOT NULL DEFAULT '{}'
);

CREATE VIEW container_equipment_current_metrics AS
WITH latest AS (
 SELECT 4 AS kind,tank_id AS id,time,hashrate_hs,power_w,efficiency_jh,temperature_c,min_temperature_c,max_temperature_c,coverage FROM container_tank_latest_metrics
 UNION ALL
 SELECT 2,device_id,time,hashrate_hs,power_w,efficiency_jh,temperature_c,min_temperature_c,max_temperature_c,coverage FROM container_latest_metrics
 UNION ALL
 SELECT 6,module_id,collected_at,hashrate_hs,power_w,efficiency_jh,temperature_c,temperature_c,temperature_c,coverage FROM container_module_latest_metrics
), fresh AS (
 SELECT *, time > now()-INTERVAL '2 minutes' AND time <= now()+INTERVAL '5 seconds'
  AND (CASE WHEN coverage IS NULL THEN time ELSE (coverage#>>'{hashrate,oldest_at}')::timestamptz END) > now()-INTERVAL '2 minutes' AS hash_fresh,
 time > now()-INTERVAL '2 minutes' AND time <= now()+INTERVAL '5 seconds'
  AND (CASE WHEN coverage IS NULL THEN time ELSE (coverage#>>'{power,oldest_at}')::timestamptz END) > now()-INTERVAL '2 minutes' AS power_fresh,
 time > now()-INTERVAL '2 minutes' AND time <= now()+INTERVAL '5 seconds'
  AND (CASE WHEN coverage IS NULL THEN time ELSE (coverage#>>'{temperature,oldest_at}')::timestamptz END) > now()-INTERVAL '2 minutes' AS temp_fresh
 FROM latest
)
SELECT kind,id,time,
 (CASE WHEN hash_fresh THEN hashrate_hs END)::double precision AS hashrate_hs,
 (CASE WHEN power_fresh THEN power_w END)::double precision AS power_w,
 (CASE WHEN hash_fresh AND power_fresh THEN efficiency_jh END)::double precision AS efficiency_jh,
 (CASE WHEN temp_fresh THEN temperature_c END)::double precision AS temperature_c,
 (CASE WHEN temp_fresh THEN min_temperature_c END)::double precision AS min_temperature_c,
 (CASE WHEN temp_fresh THEN max_temperature_c END)::double precision AS max_temperature_c,
 coverage FROM fresh;

CREATE OR REPLACE VIEW fleet_mining_asset AS
SELECT 5::int AS kind, d.id, d.org_id, d.site_id,
       COALESCE(NULLIF(d.custom_name, ''), concat_ws(' ', dd.manufacturer, dd.model))::text AS name,
       d.device_identifier::text AS device_identifier, 0::int AS position, COALESCE(d.serial_number, '')::text AS reported_id,
       1::int AS facility_kind, COALESCE(d.building_id, rack.building_id) AS facility_id,
       3::int AS group_kind, rack.device_set_id AS group_id,
       CASE WHEN ds.status = 'OFFLINE' OR (ds.status IS NULL AND dp.pairing_status IN ('PAIRED','DEFAULT_PASSWORD')) OR (fn.last_seen_at < now() - INTERVAL '2 minutes' AND fn.enrollment_status = 'CONFIRMED' AND fn.deleted_at IS NULL) THEN 'offline'
            WHEN ds.status IN ('MAINTENANCE', 'INACTIVE') AND dp.pairing_status <> 'AUTHENTICATION_NEEDED' THEN 'sleeping'
            WHEN ds.status IN ('ERROR', 'NEEDS_MINING_POOL', 'UPDATING', 'REBOOT_REQUIRED') OR dp.pairing_status='AUTHENTICATION_NEEDED' OR EXISTS (SELECT 1 FROM errors e WHERE e.device_id=d.id AND e.org_id=d.org_id AND e.closed_at IS NULL AND e.severity IN (1,2,3,4)) THEN 'attention'
            WHEN ds.status = 'ACTIVE' THEN 'hashing' ELSE 'unknown' END::text AS state,
       dm.time AS observed_at, dm.hash_rate_hs AS hashrate_hs, dm.power_w, dm.temp_c AS temperature_c, (dm.efficiency_jh * 1e12)::double precision AS miner_efficiency_jth
FROM device d
JOIN discovered_device dd ON dd.id = d.discovered_device_id AND dd.org_id = d.org_id
JOIN device_pairing dp ON dp.device_id = d.id
LEFT JOIN device_status ds ON ds.device_id = d.id
LEFT JOIN fleet_node_device fnd ON fnd.device_id = d.id AND fnd.org_id = d.org_id
LEFT JOIN fleet_node fn ON fn.id = fnd.fleet_node_id
LEFT JOIN LATERAL (
  SELECT r.device_set_id, r.building_id
  FROM device_set_membership ms JOIN device_set s ON s.id = ms.device_set_id AND s.deleted_at IS NULL
  JOIN device_set_rack r ON r.device_set_id = s.id
  WHERE ms.device_id = d.id AND ms.device_set_type = 'rack' AND ms.org_id = d.org_id
) rack ON TRUE
LEFT JOIN LATERAL (
  SELECT time, hash_rate_hs, power_w, temp_c, efficiency_jh FROM device_metrics
  WHERE device_identifier = d.device_identifier AND time > now() - INTERVAL '10 minutes'
    AND time <= now() + INTERVAL '5 seconds'
    AND ds.status IS DISTINCT FROM 'OFFLINE'
    AND NOT COALESCE(fn.last_seen_at < now() - INTERVAL '2 minutes' AND fn.enrollment_status = 'CONFIRMED' AND fn.deleted_at IS NULL, FALSE)
  ORDER BY time DESC LIMIT 1
) dm ON TRUE
WHERE d.deleted_at IS NULL AND dd.deleted_at IS NULL AND dd.is_active AND dd.device_class <> 2
 AND dp.pairing_status IN ('PAIRED','AUTHENTICATION_NEEDED','DEFAULT_PASSWORD')
UNION ALL
SELECT 6::int, m.id, d.org_id, d.site_id,
       (COALESCE(NULLIF(d.custom_name, ''), 'Container ' || COALESCE(NULLIF(d.serial_number, ''), d.device_identifier)) || ' T' || t.position || ' M' || m.position)::text,
       d.device_identifier::text, m.position, m.reported_id,
       2::int, d.id, 4::int, t.id,
       CASE WHEN m.state_observed_at > now() - INTERVAL '2 minutes' AND m.state_observed_at <= now() + INTERVAL '5 seconds'
              AND m.last_seen_at > now() - INTERVAL '2 minutes' THEN m.state ELSE 'unknown' END::text,
       mm.time, mm.hashrate_hs, mm.power_w, mm.temperature_c, (mm.efficiency_jh * 1e12)::double precision
FROM container_module m JOIN container_tank t ON t.id = m.tank_id
JOIN device d ON d.id = t.device_id
JOIN discovered_device dd ON dd.id = d.discovered_device_id AND dd.org_id = d.org_id
LEFT JOIN container_equipment_current_metrics mm ON mm.kind=6 AND mm.id = m.id
  AND mm.time > now() - INTERVAL '2 minutes' AND mm.time <= now() + INTERVAL '5 seconds'
  AND m.last_seen_at > now() - INTERVAL '2 minutes' AND m.state <> 'offline'
WHERE m.retired_at IS NULL AND t.present AND d.deleted_at IS NULL AND dd.deleted_at IS NULL;
