ALTER TABLE firmware_release_target
    ADD CONSTRAINT uq_firmware_release_target_id_set_org
    UNIQUE (id, release_set_id, org_id);

ALTER TABLE queue_message
    ADD COLUMN max_attempts INT NOT NULL DEFAULT 5,
    ADD CONSTRAINT ck_queue_message_max_attempts
        CHECK (max_attempts > 0);

CREATE TABLE channel_firmware_authority (
    id UUID PRIMARY KEY,
    org_id BIGINT NOT NULL,
    authority_type TEXT NOT NULL,
    authority_reference TEXT NOT NULL,
    revision BIGINT NOT NULL DEFAULT 1,
    halted_at TIMESTAMPTZ NULL,
    created_by_user_id BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_channel_firmware_authority_org
        FOREIGN KEY (org_id) REFERENCES organization(id) ON DELETE RESTRICT,
    CONSTRAINT fk_channel_firmware_authority_user
        FOREIGN KEY (created_by_user_id) REFERENCES "user"(id) ON DELETE RESTRICT,
    CONSTRAINT uq_channel_firmware_authority_id_org
        UNIQUE (id, org_id),
    CONSTRAINT uq_channel_firmware_authority_reference
        UNIQUE (org_id, authority_type, authority_reference),
    CONSTRAINT ck_channel_firmware_authority_type
        CHECK (btrim(authority_type) <> ''),
    CONSTRAINT ck_channel_firmware_authority_reference
        CHECK (btrim(authority_reference) <> ''),
    CONSTRAINT ck_channel_firmware_authority_revision
        CHECK (revision > 0)
);

CREATE TRIGGER update_channel_firmware_authority_updated_at
    BEFORE UPDATE ON channel_firmware_authority
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE channel_firmware_enforcement (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL,
    device_id BIGINT NOT NULL,
    desired_release_set_id BIGINT NOT NULL,
    desired_release_target_id BIGINT NOT NULL,
    desired_firmware_file_id TEXT NOT NULL,
    desired_firmware_version TEXT NOT NULL,
    cause_type TEXT NOT NULL,
    cause_reference TEXT NULL,
    authority_id UUID NOT NULL,
    authority_revision BIGINT NOT NULL,
    state TEXT NOT NULL DEFAULT 'pending',
    attempt_count INT NOT NULL DEFAULT 0,
    command_batch_uuid VARCHAR(36) NULL,
    revision BIGINT NOT NULL DEFAULT 1,
    desired_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    held_at TIMESTAMPTZ NULL,
    claimed_at TIMESTAMPTZ NULL,
    enqueued_at TIMESTAMPTZ NULL,
    command_completed_at TIMESTAMPTZ NULL,
    last_observed_firmware_version TEXT NULL,
    firmware_observed_at TIMESTAMPTZ NULL,
    last_observed_hashrate_hs DOUBLE PRECISION NULL,
    hashing_observed_at TIMESTAMPTZ NULL,
    confirmed_at TIMESTAMPTZ NULL,
    attention_required_at TIMESTAMPTZ NULL,
    last_error TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_channel_firmware_enforcement_device
        FOREIGN KEY (device_id, org_id)
        REFERENCES device(id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_channel_firmware_enforcement_release
        FOREIGN KEY (desired_release_set_id, org_id)
        REFERENCES firmware_release_set(id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_channel_firmware_enforcement_target
        FOREIGN KEY (desired_release_target_id, desired_release_set_id, org_id)
        REFERENCES firmware_release_target(id, release_set_id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_channel_firmware_enforcement_authority
        FOREIGN KEY (authority_id, org_id)
        REFERENCES channel_firmware_authority(id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT uq_channel_firmware_enforcement_device
        UNIQUE (device_id),
    CONSTRAINT ck_channel_firmware_enforcement_state
        CHECK (state IN (
            'pending',
            'held',
            'dispatching',
            'dispatched',
            'verifying',
            'confirmed',
            'attention_required',
            'cancelled'
        )),
    CONSTRAINT ck_channel_firmware_enforcement_attempt_count
        CHECK (attempt_count BETWEEN 0 AND 1),
    CONSTRAINT ck_channel_firmware_enforcement_revision
        CHECK (revision > 0),
    CONSTRAINT ck_channel_firmware_enforcement_authority_revision
        CHECK (authority_revision > 0),
    CONSTRAINT ck_channel_firmware_enforcement_cause
        CHECK (btrim(cause_type) <> ''),
    CONSTRAINT ck_channel_firmware_enforcement_file
        CHECK (btrim(desired_firmware_file_id) <> ''),
    CONSTRAINT ck_channel_firmware_enforcement_version
        CHECK (btrim(desired_firmware_version) <> ''),
    CONSTRAINT ck_channel_firmware_enforcement_confirmed
        CHECK (state <> 'confirmed' OR confirmed_at IS NOT NULL),
    CONSTRAINT ck_channel_firmware_enforcement_attention
        CHECK (state <> 'attention_required' OR attention_required_at IS NOT NULL)
);

CREATE INDEX idx_channel_firmware_enforcement_reconcile
    ON channel_firmware_enforcement(state, updated_at, id)
    WHERE state IN ('pending', 'held', 'dispatching', 'dispatched', 'verifying');

CREATE INDEX idx_channel_firmware_enforcement_authority
    ON channel_firmware_enforcement(authority_id, authority_revision, state);

CREATE INDEX idx_channel_firmware_enforcement_batch
    ON channel_firmware_enforcement(command_batch_uuid)
    WHERE command_batch_uuid IS NOT NULL;

CREATE TRIGGER update_channel_firmware_enforcement_updated_at
    BEFORE UPDATE ON channel_firmware_enforcement
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
