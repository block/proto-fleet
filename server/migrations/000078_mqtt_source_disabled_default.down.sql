ALTER TABLE curtailment_mqtt_source_config
    ALTER COLUMN enabled SET DEFAULT TRUE;

COMMENT ON COLUMN curtailment_mqtt_source_config.enabled IS NULL;
