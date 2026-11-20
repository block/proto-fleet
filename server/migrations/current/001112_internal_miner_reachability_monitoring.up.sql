-- requires-shared: 1000
-- Only the telemetry status writer records evidence for the current status.
-- Leave existing rows unknown until telemetry observes their status again.
ALTER TABLE device_status ADD COLUMN telemetry_status_at timestamptz;

CREATE TABLE device_reachability_exclusion (
 device_id bigint CONSTRAINT pk_device_reachability_exclusion PRIMARY KEY REFERENCES device(id) ON DELETE CASCADE,
 reason text NOT NULL CONSTRAINT ck_device_reachability_exclusion_reason CHECK (length(trim(reason)) BETWEEN 1 AND 256),
 created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
 expires_at timestamptz NOT NULL,
 CONSTRAINT ck_device_reachability_exclusion_expiry CHECK (expires_at > created_at)
);

CREATE VIEW device_reachability_summary AS
WITH inventory AS (
 SELECT d.org_id,COALESCE(d.site_id,0)::bigint AS site_id,
 COALESCE(fnd.fleet_node_id,0)::bigint AS source_node_id,dd.device_class,
 GREATEST(COALESCE(dp.paired_at,dp.created_at,d.created_at),COALESCE(fnd.assigned_at,'epoch'::timestamptz)) AS admitted_at,
 ds.status,ds.status_timestamp,ds.telemetry_status_at,
 COALESCE(ex.expires_at>CURRENT_TIMESTAMP,false) AS excluded
 FROM device d JOIN device_pairing dp ON dp.device_id=d.id
 JOIN discovered_device dd ON dd.id=d.discovered_device_id
 LEFT JOIN fleet_node_device fnd ON fnd.device_id=d.id AND fnd.org_id=d.org_id
 LEFT JOIN device_status ds ON ds.device_id=d.id
 LEFT JOIN device_reachability_exclusion ex ON ex.device_id=d.id
 WHERE d.deleted_at IS NULL AND dp.pairing_status IN ('PAIRED','DEFAULT_PASSWORD','AUTHENTICATION_NEEDED')
), classified AS (
 SELECT *,NOT excluded AND admitted_at <= CURRENT_TIMESTAMP-INTERVAL '5 minutes' AS expected,
 COALESCE(telemetry_status_at = status_timestamp
 AND telemetry_status_at >= GREATEST(admitted_at,CURRENT_TIMESTAMP-INTERVAL '2 minutes')
 AND telemetry_status_at <= CURRENT_TIMESTAMP
 AND status IN ('ACTIVE','INACTIVE','MAINTENANCE','ERROR','NEEDS_MINING_POOL','OFFLINE'),false) AS known
 FROM inventory
)
SELECT org_id,site_id,source_node_id,device_class,
 count(*) FILTER (WHERE expected)::bigint AS expected_count,
 count(*) FILTER (WHERE expected AND known AND status <> 'OFFLINE')::bigint AS reachable_count,
 count(*) FILTER (WHERE expected AND known AND status = 'OFFLINE')::bigint AS unreachable_count,
 count(*) FILTER (WHERE expected AND NOT known)::bigint AS unknown_count,
 count(*) FILTER (WHERE excluded)::bigint AS excluded_count,
 count(*) FILTER (WHERE NOT excluded AND NOT expected)::bigint AS grace_count
FROM classified GROUP BY org_id,site_id,source_node_id,device_class;

-- HA provisions the monitoring role before Fleet migrations run.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'grafana_ha_ro') THEN
        GRANT SELECT ON device_reachability_summary TO grafana_ha_ro;
    END IF;
END $$;
