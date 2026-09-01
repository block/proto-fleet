-- Owner-privilege view for the Fleet Node availability alert. Keep Grafana's
-- direct access limited to the identifiers and heartbeat timestamps required
-- by the rule; enrollment and deletion state stay behind the view boundary.
CREATE VIEW fleet_node_alert_status AS
SELECT
    org_id::text AS organization_id,
    id::text AS fleet_node_id,
    name AS fleet_node_name,
    last_seen_at,
    updated_at,
    created_at
FROM fleet_node
WHERE enrollment_status = 'CONFIRMED'
  AND deleted_at IS NULL;

-- HA creates grafana_ha_ro during Patroni bootstrap, before Fleet migrations.
-- Standalone installs grant the view through run-fleet.sh after migrations.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'grafana_ha_ro') THEN
        GRANT SELECT ON fleet_node_alert_status TO grafana_ha_ro;
    END IF;
END
$$;
