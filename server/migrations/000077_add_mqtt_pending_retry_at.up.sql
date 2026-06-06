ALTER TABLE curtailment_mqtt_source_state
    ADD COLUMN pending_retry_at TIMESTAMPTZ NULL;

COMMENT ON COLUMN curtailment_mqtt_source_state.pending_retry_at IS
    'Earliest retry time for durable pending MQTT edge dispatches that intentionally throttle retryable outcomes.';
