-- HA creates grafana_ha_ro during Patroni bootstrap, before Fleet migrations
-- create these objects. Grant only the two relations used by the provisioned
-- HA readiness rule; standalone installs continue provisioning their broader
-- optional Grafana grants through run-fleet.sh.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'grafana_ha_ro') THEN
        GRANT SELECT ON notification_metric_sample TO grafana_ha_ro;
        GRANT SELECT ON fleet_active_organization TO grafana_ha_ro;
    END IF;
END
$$;
