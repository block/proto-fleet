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
