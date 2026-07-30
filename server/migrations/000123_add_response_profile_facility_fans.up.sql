ALTER TABLE curtailment_response_profile
    ADD COLUMN facility_fan_device_ids BIGINT[] NOT NULL DEFAULT '{}'::BIGINT[],
    ADD COLUMN fan_off_delay_sec INT NOT NULL DEFAULT 0,
    ADD COLUMN fan_restore_delay_sec INT NOT NULL DEFAULT 0,
    ADD CONSTRAINT ck_curtailment_response_profile_fan_off_delay
        CHECK (fan_off_delay_sec >= 0),
    ADD CONSTRAINT ck_curtailment_response_profile_fan_restore_delay
        CHECK (fan_restore_delay_sec >= 0);

-- Supports the infrastructure-device delete guard's array containment lookup.
CREATE INDEX idx_curtailment_response_profile_facility_fans
    ON curtailment_response_profile USING GIN (facility_fan_device_ids);
