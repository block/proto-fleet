CREATE TABLE firmware_rollout (
    id                 BIGSERIAL PRIMARY KEY,
    rollout_uuid       UUID        NOT NULL UNIQUE,
    org_id             BIGINT      NOT NULL,
    name               TEXT        NOT NULL,
    firmware_file_id   TEXT        NOT NULL,
    state              TEXT        NOT NULL,
    target_count       INT         NOT NULL DEFAULT 0,
    batch_size         INT         NOT NULL,
    batch_interval_sec INT         NOT NULL,
    scope_type         TEXT        NOT NULL,
    scope_jsonb        JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_by         BIGINT      NOT NULL,
    started_at         TIMESTAMPTZ NULL,
    ended_at           TIMESTAMPTZ NULL,
    last_batch_at      TIMESTAMPTZ NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_firmware_rollout_org FOREIGN KEY (org_id)
        REFERENCES organization(id) ON DELETE RESTRICT,
    CONSTRAINT fk_firmware_rollout_created_by FOREIGN KEY (created_by)
        REFERENCES "user"(id) ON DELETE RESTRICT,
    CONSTRAINT ck_firmware_rollout_name_nonempty
        CHECK (length(trim(name)) > 0),
    CONSTRAINT ck_firmware_rollout_state
        CHECK (state IN ('draft', 'running', 'paused', 'completed', 'completed_with_failures', 'canceled')),
    CONSTRAINT ck_firmware_rollout_batch_size_positive
        CHECK (batch_size > 0),
    CONSTRAINT ck_firmware_rollout_batch_interval_nonnegative
        CHECK (batch_interval_sec >= 0),
    CONSTRAINT ck_firmware_rollout_target_count_nonnegative
        CHECK (target_count >= 0)
);

CREATE TRIGGER update_firmware_rollout_updated_at
    BEFORE UPDATE ON firmware_rollout
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

CREATE INDEX idx_firmware_rollout_org_created
    ON firmware_rollout (org_id, created_at DESC, id DESC);

CREATE INDEX idx_firmware_rollout_non_terminal
    ON firmware_rollout (state, last_batch_at)
    WHERE state IN ('running', 'paused');

CREATE TABLE firmware_rollout_target (
    rollout_id             BIGINT      NOT NULL,
    device_identifier      VARCHAR     NOT NULL,
    state                  TEXT        NOT NULL,
    current_attempt_number INT         NOT NULL DEFAULT 0,
    last_command_batch_uuid VARCHAR(36) NULL,
    last_error             TEXT        NULL,
    added_at               TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    PRIMARY KEY (rollout_id, device_identifier),
    CONSTRAINT fk_firmware_rollout_target_rollout FOREIGN KEY (rollout_id)
        REFERENCES firmware_rollout(id) ON DELETE CASCADE,
    CONSTRAINT ck_firmware_rollout_target_state
        CHECK (state IN ('pending', 'dispatching', 'dispatched', 'succeeded', 'failed', 'canceled')),
    CONSTRAINT ck_firmware_rollout_target_attempt_nonnegative
        CHECK (current_attempt_number >= 0)
);

CREATE TRIGGER update_firmware_rollout_target_updated_at
    BEFORE UPDATE ON firmware_rollout_target
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

CREATE INDEX idx_firmware_rollout_target_work
    ON firmware_rollout_target (rollout_id, state, updated_at, device_identifier)
    WHERE state IN ('pending', 'dispatching', 'dispatched');

CREATE TABLE firmware_rollout_attempt (
    rollout_id          BIGINT      NOT NULL,
    device_identifier   VARCHAR     NOT NULL,
    attempt_number      INT         NOT NULL,
    command_batch_uuid  VARCHAR(36) NULL,
    status              TEXT        NOT NULL,
    error_info          TEXT        NULL,
    started_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    finished_at         TIMESTAMPTZ NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    PRIMARY KEY (rollout_id, device_identifier, attempt_number),
    CONSTRAINT fk_firmware_rollout_attempt_target FOREIGN KEY (rollout_id, device_identifier)
        REFERENCES firmware_rollout_target(rollout_id, device_identifier) ON DELETE CASCADE,
    CONSTRAINT ck_firmware_rollout_attempt_status
        CHECK (status IN ('dispatching', 'dispatched', 'succeeded', 'failed')),
    CONSTRAINT ck_firmware_rollout_attempt_number_positive
        CHECK (attempt_number > 0)
);

CREATE TRIGGER update_firmware_rollout_attempt_updated_at
    BEFORE UPDATE ON firmware_rollout_attempt
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

CREATE INDEX idx_firmware_rollout_attempt_batch
    ON firmware_rollout_attempt (command_batch_uuid)
    WHERE command_batch_uuid IS NOT NULL;

CREATE TABLE firmware_rollout_event (
    id          BIGSERIAL PRIMARY KEY,
    rollout_id  BIGINT      NOT NULL,
    event_type  TEXT        NOT NULL,
    actor_type  TEXT        NOT NULL,
    user_id     TEXT        NULL,
    username    TEXT        NULL,
    message     TEXT        NOT NULL,
    metadata    JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_firmware_rollout_event_rollout FOREIGN KEY (rollout_id)
        REFERENCES firmware_rollout(id) ON DELETE CASCADE,
    CONSTRAINT ck_firmware_rollout_event_type_nonempty
        CHECK (length(trim(event_type)) > 0),
    CONSTRAINT ck_firmware_rollout_event_message_nonempty
        CHECK (length(trim(message)) > 0)
);

CREATE INDEX idx_firmware_rollout_event_rollout_created
    ON firmware_rollout_event (rollout_id, created_at, id);

CREATE TABLE firmware_rollout_reconciler_heartbeat (
    id                    SMALLINT     PRIMARY KEY DEFAULT 1,
    last_tick_at          TIMESTAMPTZ  NOT NULL,
    last_tick_uuid        UUID         NOT NULL,
    last_tick_duration_ms INT          NULL,
    active_rollout_count  INT          NOT NULL DEFAULT 0,

    CONSTRAINT ck_firmware_rollout_reconciler_heartbeat_singleton
        CHECK (id = 1)
);

INSERT INTO firmware_rollout_reconciler_heartbeat (id, last_tick_at, last_tick_uuid)
    VALUES (1, CURRENT_TIMESTAMP, '00000000-0000-0000-0000-000000000000')
    ON CONFLICT (id) DO NOTHING;
