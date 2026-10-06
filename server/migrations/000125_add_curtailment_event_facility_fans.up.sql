ALTER TABLE curtailment_event
    ADD COLUMN facility_fan_device_ids BIGINT[] NOT NULL DEFAULT '{}'::BIGINT[],
    ADD COLUMN facility_fan_site_ids BIGINT[] NOT NULL DEFAULT '{}'::BIGINT[],
    ADD COLUMN fan_off_delay_sec INT NOT NULL DEFAULT 0,
    ADD COLUMN fan_restore_delay_sec INT NOT NULL DEFAULT 0,
    ADD COLUMN fan_off_sent_at TIMESTAMPTZ,
    ADD COLUMN fan_on_sent_at TIMESTAMPTZ,
    ADD COLUMN fan_airflow_reopened_at TIMESTAMPTZ,
    ADD COLUMN fan_last_error TEXT,
    ADD CONSTRAINT ck_curtailment_event_fan_off_delay CHECK (fan_off_delay_sec >= 0),
    ADD CONSTRAINT ck_curtailment_event_fan_restore_delay CHECK (fan_restore_delay_sec >= 0),
    ADD CONSTRAINT ck_curtailment_event_fan_site_alignment CHECK (
        cardinality(facility_fan_device_ids) = cardinality(facility_fan_site_ids)
    );

CREATE INDEX idx_curtailment_event_active_facility_fans
    ON curtailment_event USING GIN (facility_fan_device_ids)
    WHERE state IN ('pending', 'active', 'restoring')
       OR fan_last_error IS NOT NULL;
