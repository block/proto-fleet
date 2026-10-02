-- The controller remains the only paired/polled device. Child IDs are Fleet
-- identities, independent of firmware selectors and reported serial numbers.
CREATE TABLE container_inventory (
    device_id BIGINT PRIMARY KEY REFERENCES device(id),
    observed_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE container_tank (
    id BIGSERIAL PRIMARY KEY,
    device_id BIGINT NOT NULL REFERENCES container_inventory(device_id),
    position INT NOT NULL CHECK (position > 0),
    present BOOLEAN NOT NULL DEFAULT TRUE,
    first_seen_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    UNIQUE (device_id, position)
);

CREATE TABLE container_module (
    id BIGSERIAL PRIMARY KEY,
    tank_id BIGINT NOT NULL REFERENCES container_tank(id),
    position INT NOT NULL CHECK (position > 0),
    reported_id TEXT NOT NULL DEFAULT '',
    first_seen_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    retired_at TIMESTAMPTZ,
    state TEXT NOT NULL DEFAULT 'unknown',
    state_observed_at TIMESTAMPTZ,
    CHECK (state IN ('unknown', 'hashing', 'attention', 'offline', 'sleeping', 'isolated'))
);
CREATE UNIQUE INDEX container_module_current_position ON container_module(tank_id, position) WHERE retired_at IS NULL;

CREATE TABLE container_module_metrics (
    module_id BIGINT NOT NULL REFERENCES container_module(id),
    time TIMESTAMPTZ NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL,
    hashrate_hs DOUBLE PRECISION,
    power_w DOUBLE PRECISION,
    temperature_c DOUBLE PRECISION,
    PRIMARY KEY (module_id, time, collected_at)
);
SELECT create_hypertable('container_module_metrics', 'time');
SELECT add_retention_policy('container_module_metrics', INTERVAL '30 days');

-- Latest values survive history retention and avoid a history scan per list row.
CREATE TABLE container_module_latest_metrics (
    module_id BIGINT PRIMARY KEY REFERENCES container_module(id),
    time TIMESTAMPTZ NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL,
    hashrate_hs DOUBLE PRECISION,
    power_w DOUBLE PRECISION,
    temperature_c DOUBLE PRECISION
);

-- Existing controllers reconcile from their next successful observation. No
-- identities or historical samples are synthesized from the legacy snapshot.

-- Shared read projection, not a shared physical-entity lifecycle. In particular,
-- modules are never inserted into device and are never separately polled.
CREATE VIEW fleet_mining_asset AS
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
       mm.time, mm.hashrate_hs, mm.power_w, mm.temperature_c, NULL::double precision
FROM container_module m JOIN container_tank t ON t.id = m.tank_id
JOIN device d ON d.id = t.device_id
JOIN discovered_device dd ON dd.id = d.discovered_device_id AND dd.org_id = d.org_id
LEFT JOIN container_module_latest_metrics mm ON mm.module_id = m.id
  AND mm.time > now() - INTERVAL '2 minutes' AND mm.time <= now() + INTERVAL '5 seconds'
  AND m.last_seen_at > now() - INTERVAL '2 minutes' AND m.state <> 'offline'
WHERE m.retired_at IS NULL AND t.present AND d.deleted_at IS NULL AND dd.deleted_at IS NULL;
