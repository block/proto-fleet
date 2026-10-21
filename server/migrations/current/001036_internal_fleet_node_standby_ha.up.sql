-- requires-shared: 1000
ALTER TABLE pending_enrollment ADD COLUMN standby_owner_node_id bigint;
ALTER TABLE pending_enrollment ADD CONSTRAINT fk_pending_enrollment_standby_owner
    FOREIGN KEY (standby_owner_node_id, org_id) REFERENCES fleet_node(id, org_id);

CREATE TABLE fleet_node_ha_connection (
    node_id bigint PRIMARY KEY,
    org_id bigint NOT NULL,
    ha_capable boolean NOT NULL,
    instance_id text NOT NULL,
    control_session_id text NOT NULL,
    credential_key_fingerprint bytea NOT NULL,
    seen_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT fk_fleet_node_ha_connection_node FOREIGN KEY (node_id,org_id) REFERENCES fleet_node(id,org_id)
);
CREATE TABLE fleet_node_ha_group (
    owner_node_id bigint PRIMARY KEY,
    org_id bigint NOT NULL,
    credential_key_fingerprint bytea NOT NULL CHECK (octet_length(credential_key_fingerprint)=32),
    active_node_id bigint NOT NULL DEFAULT 0,
    instance_id text NOT NULL DEFAULT '',
    control_session_id text NOT NULL DEFAULT '',
    lease_epoch bigint NOT NULL DEFAULT 0,
    lease_expires_at timestamptz NOT NULL DEFAULT '1970-01-01 00:00:00+00',
    acknowledged boolean NOT NULL DEFAULT false,
    CONSTRAINT uq_fleet_node_ha_group_owner_org UNIQUE(owner_node_id,org_id),
    CONSTRAINT fk_fleet_node_ha_group_owner FOREIGN KEY(owner_node_id,org_id) REFERENCES fleet_node(id,org_id)
);
CREATE TABLE fleet_node_ha_member (
    node_id bigint PRIMARY KEY,
    org_id bigint NOT NULL,
    owner_node_id bigint NOT NULL,
    ready boolean NOT NULL DEFAULT false,
    status_reason text NOT NULL DEFAULT 'preparing credential key',
    joined_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    revoked_at timestamptz,
    CONSTRAINT fk_fleet_node_ha_member_node FOREIGN KEY(node_id,org_id) REFERENCES fleet_node(id,org_id),
    CONSTRAINT fk_fleet_node_ha_member_group FOREIGN KEY(owner_node_id,org_id) REFERENCES fleet_node_ha_group(owner_node_id,org_id)
);
CREATE INDEX idx_fleet_node_ha_member_owner ON fleet_node_ha_member(owner_node_id,org_id,joined_at,node_id);
CREATE TABLE fleet_node_ha_transfer (
    recipient_node_id bigint PRIMARY KEY REFERENCES fleet_node_ha_member(node_id),
    operation_id text NOT NULL UNIQUE,
    owner_node_id bigint NOT NULL REFERENCES fleet_node_ha_group(owner_node_id),
    source_node_id bigint NOT NULL REFERENCES fleet_node_ha_member(node_id),
    expires_at timestamptz NOT NULL,
    attempts integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL DEFAULT '1970-01-01 00:00:00+00',
    failed boolean NOT NULL DEFAULT false,
    encrypted_key bytea NOT NULL DEFAULT ''::bytea
);

-- Stable device ownership survives revocation of the original physical node.
-- A grouped row reports liveness only for an acknowledged, unexpired lease.
CREATE VIEW fleet_node_effective_route AS
SELECT fn.id AS owner_node_id, fn.org_id,
       CASE WHEN g.owner_node_id IS NULL THEN fn.id ELSE g.active_node_id END AS physical_node_id,
       COALESCE(g.lease_epoch,0)::bigint AS lease_epoch,
       COALESCE(g.instance_id,'')::text AS instance_id,
       COALESCE(g.control_session_id,'')::text AS control_session_id,
       physical.encryption_pubkey,
       CASE WHEN g.owner_node_id IS NULL OR (g.acknowledged AND g.lease_expires_at>clock_timestamp() AND physical.enrollment_status='CONFIRMED' AND physical.deleted_at IS NULL) THEN h.last_seen_at ELSE NULL END AS last_seen_at,
       g.lease_expires_at,
       COALESCE(g.acknowledged,true)::boolean AS acknowledged
FROM fleet_node fn
LEFT JOIN fleet_node_ha_group g ON g.owner_node_id=fn.id AND g.org_id=fn.org_id
LEFT JOIN fleet_node physical ON physical.id=CASE WHEN g.owner_node_id IS NULL THEN fn.id ELSE g.active_node_id END AND physical.org_id=fn.org_id
LEFT JOIN fleet_node_heartbeat h ON h.fleet_node_id=physical.id AND h.org_id=physical.org_id
WHERE fn.deleted_at IS NULL AND (fn.enrollment_status='CONFIRMED' OR g.owner_node_id IS NOT NULL)
 AND NOT EXISTS (SELECT 1 FROM fleet_node_ha_member m WHERE m.node_id=fn.id AND m.owner_node_id<>fn.id);

CREATE VIEW fleet_node_effective_live AS
SELECT fn.id,fn.org_id,fn.name,fn.identity_pubkey,
 COALESCE(route.encryption_pubkey,fn.encryption_pubkey) AS encryption_pubkey,
 CASE WHEN g.owner_node_id IS NOT NULL AND EXISTS (
  SELECT 1 FROM fleet_node_ha_member m JOIN fleet_node physical ON physical.id=m.node_id AND physical.org_id=m.org_id
  WHERE m.owner_node_id=fn.id AND m.org_id=fn.org_id AND m.revoked_at IS NULL
   AND physical.enrollment_status='CONFIRMED' AND physical.deleted_at IS NULL
 ) THEN 'CONFIRMED'::varchar(32) ELSE fn.enrollment_status END AS enrollment_status,
 CASE WHEN g.owner_node_id IS NOT NULL THEN COALESCE(route.last_seen_at,'1970-01-01 00:00:00+00'::timestamptz) ELSE h.last_seen_at END AS last_seen_at,
 fn.created_at,fn.updated_at,fn.deleted_at
FROM fleet_node fn
LEFT JOIN fleet_node_ha_group g ON g.owner_node_id=fn.id AND g.org_id=fn.org_id
LEFT JOIN fleet_node_effective_route route ON route.owner_node_id=fn.id AND route.org_id=fn.org_id
LEFT JOIN fleet_node_heartbeat h ON h.fleet_node_id=fn.id AND h.org_id=fn.org_id
WHERE NOT EXISTS (SELECT 1 FROM fleet_node_ha_member m WHERE m.node_id=fn.id AND m.owner_node_id<>fn.id);

-- Device availability follows the active physical member, not the original host.
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
LEFT JOIN fleet_node_effective_live fn ON fn.id = fnd.fleet_node_id AND fn.org_id=fnd.org_id
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
