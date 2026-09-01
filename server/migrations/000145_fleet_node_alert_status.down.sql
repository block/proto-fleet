DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'grafana_ha_ro') THEN
        REVOKE SELECT ON fleet_node_alert_status FROM grafana_ha_ro;
    END IF;
END
$$;

DROP VIEW IF EXISTS fleet_node_alert_status;
