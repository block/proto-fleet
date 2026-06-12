ALTER TABLE curtailment_event
    ADD COLUMN curtail_batch_size INT NULL,
    ADD COLUMN curtail_batch_interval_sec INT NOT NULL DEFAULT 0,
    ADD CONSTRAINT ck_curtailment_event_curtail_batch_size
        CHECK (curtail_batch_size IS NULL OR (curtail_batch_size > 0 AND curtail_batch_size <= 10000)),
    ADD CONSTRAINT ck_curtailment_event_curtail_batch_interval
        CHECK (curtail_batch_interval_sec >= 0 AND curtail_batch_interval_sec <= 3600);

-- Preserve the pre-existing manual Start behavior for any events that existed
-- before this migration: curtail dispatch used effective_batch_size.
UPDATE curtailment_event
SET curtail_batch_size = effective_batch_size
WHERE effective_batch_size IS NOT NULL;

CREATE TABLE curtailment_automation_rule (
    id                          BIGSERIAL    PRIMARY KEY,
    org_id                      BIGINT       NOT NULL,
    rule_name                   VARCHAR(64)  NOT NULL,
    trigger_type                TEXT         NOT NULL DEFAULT 'MQTT',
    mqtt_source_id              BIGINT       NOT NULL,
    response_profile_id         BIGINT       NOT NULL,
    enabled                     BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at                  TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at                  TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_curtailment_automation_rule_org FOREIGN KEY (org_id)
        REFERENCES organization(id) ON DELETE RESTRICT,
    CONSTRAINT fk_curtailment_automation_rule_mqtt_source FOREIGN KEY (mqtt_source_id)
        REFERENCES curtailment_mqtt_source_config(id) ON DELETE RESTRICT,
    CONSTRAINT fk_curtailment_automation_rule_response_profile FOREIGN KEY (response_profile_id)
        REFERENCES curtailment_response_profile(id) ON DELETE RESTRICT,
    CONSTRAINT uq_curtailment_automation_rule_org_name UNIQUE (org_id, rule_name),
    CONSTRAINT ck_curtailment_automation_rule_name_nonempty
        CHECK (btrim(rule_name) <> ''),
    CONSTRAINT ck_curtailment_automation_rule_trigger_type
        CHECK (trigger_type IN ('MQTT'))
);

CREATE INDEX idx_curtailment_automation_rule_org
    ON curtailment_automation_rule (org_id);

CREATE INDEX idx_curtailment_automation_rule_mqtt_source
    ON curtailment_automation_rule (mqtt_source_id)
    WHERE enabled = TRUE;

CREATE INDEX idx_curtailment_automation_rule_response_profile
    ON curtailment_automation_rule (response_profile_id);

CREATE TRIGGER update_curtailment_automation_rule_updated_at
    BEFORE UPDATE ON curtailment_automation_rule
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE curtailment_automation_rule_state (
    rule_id                     BIGINT       PRIMARY KEY,
    last_signal                 TEXT         NULL,
    last_signal_at              TIMESTAMPTZ  NULL,
    active_event_uuid           UUID         NULL,
    last_started_at             TIMESTAMPTZ  NULL,
    last_restored_at            TIMESTAMPTZ  NULL,
    last_error                  TEXT         NULL,
    last_error_at               TIMESTAMPTZ  NULL,
    updated_at                  TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_curtailment_automation_rule_state_rule FOREIGN KEY (rule_id)
        REFERENCES curtailment_automation_rule(id) ON DELETE CASCADE,
    CONSTRAINT fk_curtailment_automation_rule_state_event FOREIGN KEY (active_event_uuid)
        REFERENCES curtailment_event(event_uuid) ON DELETE SET NULL,
    CONSTRAINT ck_curtailment_automation_rule_state_signal
        CHECK (last_signal IS NULL OR last_signal IN ('OFF', 'ON'))
);

CREATE TRIGGER update_curtailment_automation_rule_state_updated_at
    BEFORE UPDATE ON curtailment_automation_rule_state
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
