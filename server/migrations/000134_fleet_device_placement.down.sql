-- Refuse to drop the view while any rule's persisted Grafana SQL still references it: a rollback with scoped rules live would leave them erroring silently (synthetic datasource errors are operator-only, so alert delivery just stops). 000135's down runs before this and refuses while any config rows exist, so if alert_rule_config is already gone no configs — hence no scoped rules — existed; if it survives (partial rollback to 134), it is queryable here. Legacy annotation configs predate scopes and cannot be scoped. The IFs nest because PL/pgSQL prepares each statement on first execution: a single ANDed expression would reference the possibly-dropped table at plan time and error before the to_regclass guard could matter.
DO $$
BEGIN
    IF to_regclass('alert_rule_config') IS NOT NULL THEN
        IF EXISTS (SELECT 1 FROM alert_rule_config WHERE config ? 'scope') THEN
            RAISE EXCEPTION 'scoped alert rules exist; unscope or delete them before rolling back (their Grafana SQL references fleet_device_placement)';
        END IF;
    END IF;
END $$;
DROP VIEW IF EXISTS fleet_device_placement;
