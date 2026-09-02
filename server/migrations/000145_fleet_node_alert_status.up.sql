-- Owner-privilege view for Fleet Node availability alerts. Grafana receives
-- only the identifiers and timestamps needed to evaluate heartbeat staleness.
CREATE VIEW fleet_node_alert_status AS
SELECT
    org_id::text AS organization_id,
    id::text AS fleet_node_id,
    last_seen_at,
    updated_at,
    created_at
FROM fleet_node
WHERE enrollment_status = 'CONFIRMED'
  AND deleted_at IS NULL;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'grafana_ha_ro') THEN
        GRANT SELECT ON fleet_node_alert_status TO grafana_ha_ro;
    END IF;
END
$$;
