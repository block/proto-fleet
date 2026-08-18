ALTER TABLE channel_firmware_enforcement
    ADD CONSTRAINT uq_channel_firmware_enforcement_id_org_device
    UNIQUE (id, org_id, device_id);

CREATE TABLE firmware_rollout (
    id UUID PRIMARY KEY,
    org_id BIGINT NOT NULL,
    name TEXT NOT NULL,
    strategy_key TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'created',
    resume_state TEXT NULL,
    revision BIGINT NOT NULL DEFAULT 1,
    forward_authority_id UUID NOT NULL,
    forward_authority_revision BIGINT NOT NULL DEFAULT 1,
    revert_authority_id UUID NULL,
    revert_authority_revision BIGINT NULL,
    source_channel_id BIGINT NULL,
    target_channel_id BIGINT NULL,
    source_release_set_id BIGINT NULL,
    target_release_set_id BIGINT NULL,
    source_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    target_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    revert_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    idempotency_key VARCHAR(256) NOT NULL,
    create_fingerprint VARCHAR(64) NOT NULL,
    reason VARCHAR(256) NOT NULL,
    created_by_user_id BIGINT NOT NULL,
    started_at TIMESTAMPTZ NULL,
    paused_at TIMESTAMPTZ NULL,
    aborted_at TIMESTAMPTZ NULL,
    completed_at TIMESTAMPTZ NULL,
    reverting_at TIMESTAMPTZ NULL,
    reverted_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_firmware_rollout_org
        FOREIGN KEY (org_id) REFERENCES organization(id) ON DELETE RESTRICT,
    CONSTRAINT fk_firmware_rollout_user
        FOREIGN KEY (created_by_user_id) REFERENCES "user"(id) ON DELETE RESTRICT,
    CONSTRAINT fk_firmware_rollout_forward_authority
        FOREIGN KEY (forward_authority_id, org_id)
        REFERENCES channel_firmware_authority(id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_firmware_rollout_revert_authority
        FOREIGN KEY (revert_authority_id, org_id)
        REFERENCES channel_firmware_authority(id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_firmware_rollout_source_channel
        FOREIGN KEY (source_channel_id, org_id)
        REFERENCES device_set(id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_firmware_rollout_target_channel
        FOREIGN KEY (target_channel_id, org_id)
        REFERENCES device_set(id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_firmware_rollout_source_release
        FOREIGN KEY (source_release_set_id, org_id)
        REFERENCES firmware_release_set(id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_firmware_rollout_target_release
        FOREIGN KEY (target_release_set_id, org_id)
        REFERENCES firmware_release_set(id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT uq_firmware_rollout_id_org UNIQUE (id, org_id),
    CONSTRAINT uq_firmware_rollout_idempotency UNIQUE (org_id, idempotency_key),
    CONSTRAINT ck_firmware_rollout_name CHECK (btrim(name) <> ''),
    CONSTRAINT ck_firmware_rollout_strategy CHECK (btrim(strategy_key) <> ''),
    CONSTRAINT ck_firmware_rollout_idempotency CHECK (btrim(idempotency_key) <> ''),
    CONSTRAINT ck_firmware_rollout_create_fingerprint
        CHECK (create_fingerprint ~ '^[0-9a-f]{64}$'),
    CONSTRAINT ck_firmware_rollout_reason CHECK (btrim(reason) <> ''),
    CONSTRAINT ck_firmware_rollout_revision CHECK (revision > 0),
    CONSTRAINT ck_firmware_rollout_forward_authority_revision
        CHECK (forward_authority_revision > 0),
    CONSTRAINT ck_firmware_rollout_revert_authority_pair
        CHECK (
            (revert_authority_id IS NULL AND revert_authority_revision IS NULL)
            OR
            (revert_authority_id IS NOT NULL AND revert_authority_revision > 0)
        ),
    CONSTRAINT ck_firmware_rollout_state
        CHECK (state IN (
            'created',
            'running',
            'paused',
            'review',
            'aborted',
            'completed',
            'completed_with_failures',
            'reverting',
            'reverted'
        )),
    CONSTRAINT ck_firmware_rollout_resume_state
        CHECK (resume_state IS NULL OR resume_state IN ('running', 'review')),
    CONSTRAINT ck_firmware_rollout_source_snapshot_object
        CHECK (jsonb_typeof(source_snapshot) = 'object'),
    CONSTRAINT ck_firmware_rollout_target_snapshot_object
        CHECK (jsonb_typeof(target_snapshot) = 'object'),
    CONSTRAINT ck_firmware_rollout_revert_snapshot_object
        CHECK (jsonb_typeof(revert_snapshot) = 'object')
);

CREATE INDEX idx_firmware_rollout_org_created
    ON firmware_rollout(org_id, created_at DESC, id DESC);

CREATE INDEX idx_firmware_rollout_org_state
    ON firmware_rollout(org_id, state, updated_at DESC);

CREATE TRIGGER update_firmware_rollout_updated_at
    BEFORE UPDATE ON firmware_rollout
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE firmware_rollout_batch (
    id BIGSERIAL PRIMARY KEY,
    rollout_id UUID NOT NULL,
    org_id BIGINT NOT NULL,
    position INT NOT NULL,
    label TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL DEFAULT 'pending',
    revision BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_firmware_rollout_batch_rollout
        FOREIGN KEY (rollout_id, org_id)
        REFERENCES firmware_rollout(id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT uq_firmware_rollout_batch_id_rollout_org
        UNIQUE (id, rollout_id, org_id),
    CONSTRAINT uq_firmware_rollout_batch_position
        UNIQUE (rollout_id, position),
    CONSTRAINT ck_firmware_rollout_batch_position CHECK (position >= 0),
    CONSTRAINT ck_firmware_rollout_batch_revision CHECK (revision > 0),
    CONSTRAINT ck_firmware_rollout_batch_state
        CHECK (state IN ('pending', 'admitted', 'completed', 'cancelled'))
);

CREATE TRIGGER update_firmware_rollout_batch_updated_at
    BEFORE UPDATE ON firmware_rollout_batch
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE firmware_rollout_member (
    id BIGSERIAL PRIMARY KEY,
    rollout_id UUID NOT NULL,
    batch_id BIGINT NOT NULL,
    org_id BIGINT NOT NULL,
    device_id BIGINT NOT NULL,
    position INT NOT NULL,
    state TEXT NOT NULL DEFAULT 'pending',
    revision BIGINT NOT NULL DEFAULT 1,
    source_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    target_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    revert_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    enforcement_id BIGINT NULL,
    command_batch_uuid VARCHAR(36) NULL,
    last_error TEXT NULL,
    admitted_at TIMESTAMPTZ NULL,
    settled_at TIMESTAMPTZ NULL,
    owner_released_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_firmware_rollout_member_rollout
        FOREIGN KEY (rollout_id, org_id)
        REFERENCES firmware_rollout(id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_firmware_rollout_member_batch
        FOREIGN KEY (batch_id, rollout_id, org_id)
        REFERENCES firmware_rollout_batch(id, rollout_id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_firmware_rollout_member_device
        FOREIGN KEY (device_id, org_id)
        REFERENCES device(id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_firmware_rollout_member_enforcement
        FOREIGN KEY (enforcement_id, org_id, device_id)
        REFERENCES channel_firmware_enforcement(id, org_id, device_id)
        ON DELETE RESTRICT,
    CONSTRAINT uq_firmware_rollout_member_id_rollout_org
        UNIQUE (id, rollout_id, org_id),
    CONSTRAINT uq_firmware_rollout_member_device
        UNIQUE (rollout_id, device_id),
    CONSTRAINT uq_firmware_rollout_member_position
        UNIQUE (rollout_id, position),
    CONSTRAINT ck_firmware_rollout_member_position CHECK (position >= 0),
    CONSTRAINT ck_firmware_rollout_member_revision CHECK (revision > 0),
    CONSTRAINT ck_firmware_rollout_member_state
        CHECK (state IN (
            'pending',
            'admitted',
            'succeeded',
            'failed',
            'attention_required',
            'cancelled',
            'reverting',
            'reverted'
        )),
    CONSTRAINT ck_firmware_rollout_member_source_snapshot_object
        CHECK (jsonb_typeof(source_snapshot) = 'object'),
    CONSTRAINT ck_firmware_rollout_member_target_snapshot_object
        CHECK (jsonb_typeof(target_snapshot) = 'object'),
    CONSTRAINT ck_firmware_rollout_member_revert_snapshot_object
        CHECK (jsonb_typeof(revert_snapshot) = 'object')
);

CREATE UNIQUE INDEX uq_firmware_rollout_active_owner
    ON firmware_rollout_member(device_id)
    WHERE owner_released_at IS NULL;

CREATE INDEX idx_firmware_rollout_member_rollout_batch
    ON firmware_rollout_member(rollout_id, batch_id, position);

CREATE INDEX idx_firmware_rollout_member_enforcement
    ON firmware_rollout_member(enforcement_id)
    WHERE enforcement_id IS NOT NULL;

CREATE TRIGGER update_firmware_rollout_member_updated_at
    BEFORE UPDATE ON firmware_rollout_member
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE firmware_rollout_control (
    id UUID PRIMARY KEY,
    rollout_id UUID NOT NULL,
    org_id BIGINT NOT NULL,
    batch_id BIGINT NULL,
    operation TEXT NOT NULL,
    idempotency_key VARCHAR(256) NOT NULL,
    request_fingerprint VARCHAR(64) NOT NULL,
    expected_revision BIGINT NOT NULL,
    resulting_revision BIGINT NOT NULL,
    status TEXT NOT NULL DEFAULT 'started',
    error_message TEXT NULL,
    created_by_user_id BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_firmware_rollout_control_rollout
        FOREIGN KEY (rollout_id, org_id)
        REFERENCES firmware_rollout(id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_firmware_rollout_control_batch
        FOREIGN KEY (batch_id, rollout_id, org_id)
        REFERENCES firmware_rollout_batch(id, rollout_id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_firmware_rollout_control_user
        FOREIGN KEY (created_by_user_id) REFERENCES "user"(id) ON DELETE RESTRICT,
    CONSTRAINT uq_firmware_rollout_control_rollout_key
        UNIQUE (rollout_id, idempotency_key),
    CONSTRAINT ck_firmware_rollout_control_operation
        CHECK (operation IN (
            'admit',
            'continue',
            'pause',
            'resume',
            'abort',
            'revert',
            'complete'
        )),
    CONSTRAINT ck_firmware_rollout_control_idempotency
        CHECK (btrim(idempotency_key) <> ''),
    CONSTRAINT ck_firmware_rollout_control_fingerprint
        CHECK (request_fingerprint ~ '^[0-9a-f]{64}$'),
    CONSTRAINT ck_firmware_rollout_control_revisions
        CHECK (expected_revision > 0 AND resulting_revision > expected_revision),
    CONSTRAINT ck_firmware_rollout_control_status
        CHECK (status IN ('started', 'succeeded', 'failed'))
);

CREATE TRIGGER update_firmware_rollout_control_updated_at
    BEFORE UPDATE ON firmware_rollout_control
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE firmware_rollout_cause (
    id BIGSERIAL PRIMARY KEY,
    rollout_id UUID NOT NULL,
    member_id BIGINT NULL,
    control_id UUID NULL,
    org_id BIGINT NOT NULL,
    operation TEXT NOT NULL,
    reason TEXT NOT NULL,
    actor_user_id BIGINT NOT NULL,
    from_state TEXT NULL,
    to_state TEXT NOT NULL,
    rollout_revision BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_firmware_rollout_cause_rollout
        FOREIGN KEY (rollout_id, org_id)
        REFERENCES firmware_rollout(id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_firmware_rollout_cause_member
        FOREIGN KEY (member_id, rollout_id, org_id)
        REFERENCES firmware_rollout_member(id, rollout_id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_firmware_rollout_cause_control
        FOREIGN KEY (control_id)
        REFERENCES firmware_rollout_control(id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_firmware_rollout_cause_user
        FOREIGN KEY (actor_user_id) REFERENCES "user"(id) ON DELETE RESTRICT,
    CONSTRAINT ck_firmware_rollout_cause_operation
        CHECK (operation IN (
            'create',
            'admit',
            'continue',
            'pause',
            'resume',
            'abort',
            'revert',
            'complete'
        )),
    CONSTRAINT ck_firmware_rollout_cause_reason CHECK (btrim(reason) <> ''),
    CONSTRAINT ck_firmware_rollout_cause_revision CHECK (rollout_revision > 0)
);

CREATE INDEX idx_firmware_rollout_cause_rollout
    ON firmware_rollout_cause(rollout_id, created_at, id);

CREATE TABLE firmware_rollout_evidence (
    id BIGSERIAL PRIMARY KEY,
    rollout_id UUID NOT NULL,
    member_id BIGINT NOT NULL,
    org_id BIGINT NOT NULL,
    phase TEXT NOT NULL,
    window_start TIMESTAMPTZ NOT NULL,
    window_end TIMESTAMPTZ NOT NULL,
    observed_at TIMESTAMPTZ NULL,
    avg_hashrate_hs DOUBLE PRECISION NULL,
    avg_power_w DOUBLE PRECISION NULL,
    avg_temperature_c DOUBLE PRECISION NULL,
    error_count BIGINT NULL,
    sample_count BIGINT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_firmware_rollout_evidence_rollout
        FOREIGN KEY (rollout_id, org_id)
        REFERENCES firmware_rollout(id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_firmware_rollout_evidence_member
        FOREIGN KEY (member_id, rollout_id, org_id)
        REFERENCES firmware_rollout_member(id, rollout_id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT uq_firmware_rollout_evidence_member_phase
        UNIQUE (member_id, phase),
    CONSTRAINT ck_firmware_rollout_evidence_phase
        CHECK (phase IN ('baseline', 'post')),
    CONSTRAINT ck_firmware_rollout_evidence_window
        CHECK (window_start < window_end),
    CONSTRAINT ck_firmware_rollout_evidence_freshness
        CHECK (
            (observed_at IS NULL
                AND avg_hashrate_hs IS NULL
                AND avg_power_w IS NULL
                AND avg_temperature_c IS NULL
                AND error_count IS NULL
                AND sample_count IS NULL)
            OR
            (observed_at IS NOT NULL AND sample_count > 0)
        )
);

CREATE INDEX idx_firmware_rollout_evidence_rollout
    ON firmware_rollout_evidence(rollout_id, phase, member_id);

CREATE TRIGGER update_firmware_rollout_evidence_updated_at
    BEFORE UPDATE ON firmware_rollout_evidence
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
