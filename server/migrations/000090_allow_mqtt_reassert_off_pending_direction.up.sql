ALTER TABLE curtailment_mqtt_source_state
    DROP CONSTRAINT ck_curtailment_mqtt_source_state_pending_direction_valid;

ALTER TABLE curtailment_mqtt_source_state
    ADD CONSTRAINT ck_curtailment_mqtt_source_state_pending_direction_valid
        CHECK (pending_direction IS NULL
            OR pending_direction IN ('on_to_off', 'reassert_off', 'off_to_on', 'watchdog_off'));
