ALTER TABLE curtailment_mqtt_source_config
    ALTER COLUMN enabled SET DEFAULT FALSE;

COMMENT ON COLUMN curtailment_mqtt_source_config.enabled IS
    'Whether this MQTT source is actively ingested. New settings-created sources default disabled; existing rows are not backfilled.';
