CREATE TABLE device_firmware_state (
    org_id            BIGINT      NOT NULL,
    device_identifier VARCHAR     NOT NULL,
    firmware_version  TEXT        NOT NULL,
    observed_at       TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT pk_device_firmware_state
        PRIMARY KEY (org_id, device_identifier),
    CONSTRAINT fk_device_firmware_state_org
        FOREIGN KEY (org_id) REFERENCES organization(id) ON DELETE RESTRICT,
    CONSTRAINT ck_device_firmware_state_identifier_nonempty
        CHECK (length(trim(device_identifier)) > 0),
    CONSTRAINT ck_device_firmware_state_version_nonempty
        CHECK (length(trim(firmware_version)) > 0)
);

CREATE INDEX idx_device_firmware_state_observed
    ON device_firmware_state (org_id, observed_at DESC);

CREATE TRIGGER update_device_firmware_state_updated_at
    BEFORE UPDATE ON device_firmware_state
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE device_enforcement_state (
    org_id                   BIGINT      NOT NULL,
    device_identifier        VARCHAR     NOT NULL,
    dimension                TEXT        NOT NULL,
    state                    TEXT        NOT NULL,
    desired_firmware_file_id VARCHAR     NULL,
    desired_firmware_version TEXT        NULL,
    retry_count              INT         NOT NULL DEFAULT 0,
    last_batch_uuid          VARCHAR(36) NULL,
    last_dispatched_at       TIMESTAMPTZ NULL,
    confirmed_at             TIMESTAMPTZ NULL,
    observed_at              TIMESTAMPTZ NULL,
    last_error               TEXT        NULL,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT pk_device_enforcement_state
        PRIMARY KEY (org_id, device_identifier, dimension),
    CONSTRAINT fk_device_enforcement_state_org
        FOREIGN KEY (org_id) REFERENCES organization(id) ON DELETE RESTRICT,
    CONSTRAINT ck_device_enforcement_state_dimension
        CHECK (dimension IN ('firmware')),
    CONSTRAINT ck_device_enforcement_state_state
        CHECK (state IN ('pending', 'dispatching', 'dispatched', 'confirmed', 'drifted', 'failed')),
    CONSTRAINT ck_device_enforcement_state_retry_nonnegative
        CHECK (retry_count >= 0),
    CONSTRAINT ck_device_enforcement_state_identifier_nonempty
        CHECK (length(trim(device_identifier)) > 0),
    CONSTRAINT ck_device_enforcement_state_desired_file_nonempty
        CHECK (desired_firmware_file_id IS NULL OR length(trim(desired_firmware_file_id)) > 0),
    CONSTRAINT ck_device_enforcement_state_desired_version_nonempty
        CHECK (desired_firmware_version IS NULL OR length(trim(desired_firmware_version)) > 0)
);

CREATE INDEX idx_device_enforcement_state_firmware_state
    ON device_enforcement_state (org_id, state, updated_at DESC)
    WHERE dimension = 'firmware';

CREATE TRIGGER update_device_enforcement_state_updated_at
    BEFORE UPDATE ON device_enforcement_state
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE cohort_reconciler_heartbeat (
    id                    SMALLINT     PRIMARY KEY DEFAULT 1,
    last_tick_at          TIMESTAMPTZ  NOT NULL,
    last_tick_uuid        UUID         NOT NULL,
    last_tick_duration_ms INT          NULL,
    active_device_count   INT          NOT NULL DEFAULT 0,

    CONSTRAINT ck_cohort_reconciler_heartbeat_singleton
        CHECK (id = 1)
);

INSERT INTO cohort_reconciler_heartbeat (id, last_tick_at, last_tick_uuid)
VALUES (1, CURRENT_TIMESTAMP, '00000000-0000-0000-0000-000000000000')
ON CONFLICT (id) DO NOTHING;
