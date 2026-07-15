CREATE TABLE device_config_state (
    org_id                 BIGINT      NOT NULL,
    device_identifier      VARCHAR     NOT NULL,
    dimension              TEXT        NOT NULL,
    observed_state_jsonb   JSONB       NOT NULL,
    observed_state_hash    TEXT        NOT NULL,
    observed_at            TIMESTAMPTZ NOT NULL,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT pk_device_config_state
        PRIMARY KEY (org_id, device_identifier, dimension),
    CONSTRAINT fk_device_config_state_org
        FOREIGN KEY (org_id) REFERENCES organization(id) ON DELETE RESTRICT,
    CONSTRAINT ck_device_config_state_identifier_nonempty
        CHECK (length(trim(device_identifier)) > 0),
    CONSTRAINT ck_device_config_state_dimension_nonempty
        CHECK (length(trim(dimension)) > 0),
    CONSTRAINT ck_device_config_state_observed_hash_nonempty
        CHECK (length(trim(observed_state_hash)) > 0)
);

CREATE INDEX idx_device_config_state_observed
    ON device_config_state (org_id, dimension, observed_at DESC);

CREATE TRIGGER update_device_config_state_updated_at
    BEFORE UPDATE ON device_config_state
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

ALTER TABLE device_enforcement_state
    DROP CONSTRAINT ck_device_enforcement_state_dimension,
    DROP CONSTRAINT ck_device_enforcement_state_state,
    ADD COLUMN desired_state_hash TEXT NULL,
    ADD COLUMN supported BOOLEAN NULL,
    ADD CONSTRAINT ck_device_enforcement_state_dimension
        CHECK (length(trim(dimension)) > 0),
    ADD CONSTRAINT ck_device_enforcement_state_state
        CHECK (state IN ('pending', 'dispatching', 'dispatched', 'confirmed', 'drifted', 'held', 'failed')),
    ADD CONSTRAINT ck_device_enforcement_state_desired_hash_nonempty
        CHECK (desired_state_hash IS NULL OR length(trim(desired_state_hash)) > 0);

CREATE INDEX idx_device_enforcement_state_config_state
    ON device_enforcement_state (org_id, dimension, state, updated_at DESC)
    WHERE dimension <> 'firmware';
