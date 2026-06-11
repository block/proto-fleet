-- Narrow MQTT source settings to source/runtime only by dropping the legacy
-- direct-response columns. Existing rows predate this model, so this migration
-- refuses to run while any remain rather than silently discarding their
-- response behavior.
--
-- Pre-deploy step: in every environment that has MQTT sources, delete them
-- before applying this migration (state rows cascade away with the config):
--   SELECT id, organization_id, source_name FROM curtailment_mqtt_source_config;
--   DELETE FROM curtailment_mqtt_source_config;
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM curtailment_mqtt_source_config
    ) THEN
        RAISE EXCEPTION 'migration 000080 will not run while curtailment_mqtt_source_config has rows: delete all MQTT sources before deploying (this migration drops the legacy direct-response columns and refuses to discard existing source behavior)';
    END IF;
END $$;

ALTER TABLE curtailment_mqtt_source_config
    DROP CONSTRAINT IF EXISTS fk_curtailment_mqtt_source_config_site,
    DROP CONSTRAINT IF EXISTS ck_curtailment_mqtt_source_config_contracted_kw_range,
    DROP CONSTRAINT IF EXISTS ck_curtailment_mqtt_source_config_curtail_mode,
    DROP CONSTRAINT IF EXISTS ck_curtailment_mqtt_source_config_fixed_kw_requires_target,
    DROP CONSTRAINT IF EXISTS ck_curtailment_mqtt_source_config_hold_nonneg,
    DROP CONSTRAINT IF EXISTS ck_curtailment_mqtt_source_config_scope;

ALTER TABLE curtailment_mqtt_source_config
    DROP COLUMN IF EXISTS contracted_curtailment_kw,
    DROP COLUMN IF EXISTS curtail_mode,
    DROP COLUMN IF EXISTS scope_type,
    DROP COLUMN IF EXISTS scope_site_id,
    DROP COLUMN IF EXISTS scope_device_identifiers,
    DROP COLUMN IF EXISTS min_curtailed_duration_sec;

ALTER TABLE curtailment_mqtt_source_state
    DROP COLUMN IF EXISTS last_edge_event_uuid,
    DROP COLUMN IF EXISTS last_empty_full_fleet_watchdog_ref;
