ALTER TABLE curtailment_mqtt_source_state
    DROP COLUMN IF EXISTS pending_retry_at;
