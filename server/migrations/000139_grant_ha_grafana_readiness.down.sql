DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'grafana_ha_ro') THEN
        REVOKE SELECT ON notification_metric_sample FROM grafana_ha_ro;
        REVOKE SELECT ON fleet_active_organization FROM grafana_ha_ro;
    END IF;
END
$$;
