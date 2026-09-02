-- HA creates grafana_ha_ro during Patroni bootstrap, before Fleet migrations
-- run. Grant only the Fleet Node columns required by the availability rule.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'grafana_ha_ro') THEN
        GRANT SELECT (
            org_id,
            id,
            last_seen_at,
            updated_at,
            created_at,
            enrollment_status,
            deleted_at
        ) ON fleet_node TO grafana_ha_ro;
    END IF;
END
$$;
