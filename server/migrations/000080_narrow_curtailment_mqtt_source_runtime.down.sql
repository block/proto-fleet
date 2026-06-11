ALTER TABLE curtailment_mqtt_source_config
    ADD COLUMN contracted_curtailment_kw INT NULL,
    ADD COLUMN curtail_mode TEXT NOT NULL DEFAULT 'FULL_FLEET',
    ADD COLUMN scope_type TEXT NOT NULL DEFAULT 'whole_org',
    ADD COLUMN scope_site_id BIGINT NULL,
    ADD COLUMN scope_device_identifiers TEXT[] NULL,
    ADD COLUMN min_curtailed_duration_sec INT NULL;

ALTER TABLE curtailment_mqtt_source_config
    ADD CONSTRAINT fk_curtailment_mqtt_source_config_site
        FOREIGN KEY (scope_site_id, organization_id)
        REFERENCES site(id, org_id) ON DELETE RESTRICT,
    ADD CONSTRAINT ck_curtailment_mqtt_source_config_contracted_kw_range
        CHECK (contracted_curtailment_kw IS NULL
            OR (contracted_curtailment_kw > 0 AND contracted_curtailment_kw <= 1000000)),
    ADD CONSTRAINT ck_curtailment_mqtt_source_config_curtail_mode
        CHECK (curtail_mode IN ('FIXED_KW', 'FULL_FLEET')),
    ADD CONSTRAINT ck_curtailment_mqtt_source_config_fixed_kw_requires_target
        CHECK (curtail_mode <> 'FIXED_KW' OR contracted_curtailment_kw IS NOT NULL),
    ADD CONSTRAINT ck_curtailment_mqtt_source_config_hold_nonneg
        CHECK (min_curtailed_duration_sec IS NULL OR min_curtailed_duration_sec >= 0),
    ADD CONSTRAINT ck_curtailment_mqtt_source_config_scope CHECK (
        (scope_type = 'whole_org' AND scope_site_id IS NULL
            AND scope_device_identifiers IS NULL)
        OR
        (scope_type = 'site' AND scope_site_id IS NOT NULL
            AND scope_device_identifiers IS NULL)
        OR
        (scope_type = 'device_list' AND scope_site_id IS NULL
            AND scope_device_identifiers IS NOT NULL
            AND cardinality(scope_device_identifiers) > 0)
    );

ALTER TABLE curtailment_mqtt_source_state
    ADD COLUMN last_edge_event_uuid UUID NULL,
    ADD COLUMN last_empty_full_fleet_watchdog_ref TEXT NULL;
