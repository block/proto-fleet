DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'grafana_ha_ro') THEN
        REVOKE SELECT (
            org_id,
            id,
            last_seen_at,
            enrollment_status,
            deleted_at
        ) ON fleet_node FROM grafana_ha_ro;
    END IF;
END
$$;
