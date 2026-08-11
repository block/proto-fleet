-- Refuse while any scoped rule's persisted Grafana SQL still references the view — rolled back, those rules error silently and alert delivery just stops. A missing alert_rule_config table proves no scoped rules exist (000135's down refuses while rows exist, and every rule evaluating scoped SQL has a row).
-- The IFs nest because PL/pgSQL prepares each statement on first execution: an ANDed expression would reference the possibly-dropped table at plan time.
DO $$
BEGIN
    IF to_regclass('alert_rule_config') IS NOT NULL THEN
        IF EXISTS (SELECT 1 FROM alert_rule_config WHERE config ? 'scope') THEN
            RAISE EXCEPTION 'scoped alert rules exist; unscope or delete them before rolling back (their Grafana SQL references fleet_device_placement)';
        END IF;
    END IF;
END $$;
DROP VIEW IF EXISTS fleet_device_placement;
