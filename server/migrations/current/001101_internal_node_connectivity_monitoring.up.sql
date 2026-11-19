-- requires-shared: 1000

CREATE TABLE fleet_node_monitoring_exclusion (
 fleet_node_id bigint CONSTRAINT pk_fleet_node_monitoring_exclusion PRIMARY KEY REFERENCES fleet_node(id) ON DELETE CASCADE,
 reason text NOT NULL CONSTRAINT ck_fleet_node_monitoring_exclusion_reason CHECK (length(trim(reason)) BETWEEN 1 AND 256),
 created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
 expires_at timestamptz NOT NULL,
 CONSTRAINT ck_fleet_node_monitoring_exclusion_expiry CHECK (
     expires_at > created_at AND expires_at <= created_at + INTERVAL '7 days'
 )
);

-- Authoritative heartbeat state is in fleet_node_live, not the legacy snapshot
-- fleet_node.last_seen_at. Nodes never shrink coverage merely by disconnecting.
CREATE VIEW fleet_node_connectivity_summary AS
WITH inventory AS (
 SELECT n.id,n.org_id,n.last_seen_at,
 COALESCE((SELECT max(p.consumed_at) FROM pending_enrollment p
           WHERE p.fleet_node_id=n.id AND p.org_id=n.org_id AND p.status='CONFIRMED'),n.created_at) AS admitted_at,
 COALESCE(e.created_at <= CURRENT_TIMESTAMP AND e.expires_at > CURRENT_TIMESTAMP,false) AS excluded
 FROM fleet_node_live n LEFT JOIN fleet_node_monitoring_exclusion e ON e.fleet_node_id=n.id
 WHERE n.enrollment_status='CONFIRMED' AND n.deleted_at IS NULL
), classified AS (
 SELECT *,NOT excluded AND admitted_at <= CURRENT_TIMESTAMP-INTERVAL '5 minutes' AS expected,
 last_seen_at IS NOT NULL AND last_seen_at >= admitted_at AND last_seen_at <= CURRENT_TIMESTAMP + INTERVAL '5 seconds' AS known
 FROM inventory
)
SELECT o.id AS org_id,
 count(c.id) FILTER (WHERE c.expected)::bigint AS expected_count,
 count(c.id) FILTER (WHERE c.expected AND c.known AND c.last_seen_at >= CURRENT_TIMESTAMP-INTERVAL '2 minutes')::bigint AS recent_count,
 count(c.id) FILTER (WHERE c.expected AND c.known AND c.last_seen_at < CURRENT_TIMESTAMP-INTERVAL '2 minutes')::bigint AS overdue_count,
 count(c.id) FILTER (WHERE c.expected AND NOT c.known)::bigint AS unknown_count,
 count(c.id) FILTER (WHERE c.excluded)::bigint AS excluded_count,
 count(c.id) FILTER (WHERE NOT c.excluded AND NOT c.expected)::bigint AS grace_count,
 GREATEST(max(EXTRACT(EPOCH FROM CURRENT_TIMESTAMP-c.last_seen_at)) FILTER (WHERE c.expected AND c.known),0)::double precision AS oldest_heartbeat_age_seconds
FROM organization o LEFT JOIN classified c ON c.org_id=o.id
WHERE o.deleted_at IS NULL
GROUP BY o.id;

-- HA provisions the monitoring role before Fleet migrations run.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'grafana_ha_ro') THEN
        GRANT SELECT ON fleet_node_connectivity_summary TO grafana_ha_ro;
    END IF;
END $$;
