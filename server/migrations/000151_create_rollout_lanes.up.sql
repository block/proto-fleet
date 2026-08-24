ALTER TABLE channel_firmware_enforcement
    DROP CONSTRAINT uq_channel_firmware_enforcement_device;

CREATE UNIQUE INDEX uq_channel_firmware_enforcement_active_device
    ON channel_firmware_enforcement(device_id)
    WHERE state IN (
        'pending',
        'held',
        'dispatching',
        'dispatched',
        'verifying'
    );

CREATE UNIQUE INDEX uq_channel_firmware_enforcement_authority_device
    ON channel_firmware_enforcement(authority_id, device_id);

ALTER TABLE firmware_rollout_member
    ADD COLUMN source_release_set_id BIGINT NULL,
    ADD COLUMN source_release_target_id BIGINT NULL,
    ADD COLUMN target_release_set_id BIGINT NULL,
    ADD COLUMN target_release_target_id BIGINT NULL,
    ADD COLUMN revert_selected_at TIMESTAMPTZ NULL,
    ADD CONSTRAINT fk_firmware_rollout_member_source_target
        FOREIGN KEY (
            source_release_target_id,
            source_release_set_id,
            org_id
        )
        REFERENCES firmware_release_target(id, release_set_id, org_id)
        ON DELETE RESTRICT,
    ADD CONSTRAINT fk_firmware_rollout_member_target_target
        FOREIGN KEY (
            target_release_target_id,
            target_release_set_id,
            org_id
        )
        REFERENCES firmware_release_target(id, release_set_id, org_id)
        ON DELETE RESTRICT,
    ADD CONSTRAINT ck_firmware_rollout_member_source_target_pair
        CHECK (
            (source_release_set_id IS NULL AND source_release_target_id IS NULL)
            OR
            (source_release_set_id IS NOT NULL AND source_release_target_id IS NOT NULL)
        ),
    ADD CONSTRAINT ck_firmware_rollout_member_target_target_pair
        CHECK (
            (target_release_set_id IS NULL AND target_release_target_id IS NULL)
            OR
            (target_release_set_id IS NOT NULL AND target_release_target_id IS NOT NULL)
        );

CREATE TABLE rollout_lane (
    id UUID PRIMARY KEY,
    org_id BIGINT NOT NULL,
    label TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    current_channel_id BIGINT NOT NULL,
    revision BIGINT NOT NULL DEFAULT 1,
    idempotency_key VARCHAR(256) NOT NULL,
    create_fingerprint VARCHAR(64) NOT NULL,
    created_by_user_id BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_rollout_lane_org
        FOREIGN KEY (org_id) REFERENCES organization(id) ON DELETE RESTRICT,
    CONSTRAINT fk_rollout_lane_current_channel
        FOREIGN KEY (current_channel_id, org_id)
        REFERENCES device_set(id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_rollout_lane_user
        FOREIGN KEY (created_by_user_id) REFERENCES "user"(id) ON DELETE RESTRICT,
    CONSTRAINT uq_rollout_lane_id_org UNIQUE (id, org_id),
    CONSTRAINT uq_rollout_lane_label UNIQUE (org_id, label),
    CONSTRAINT uq_rollout_lane_idempotency UNIQUE (org_id, idempotency_key),
    CONSTRAINT ck_rollout_lane_label CHECK (btrim(label) <> ''),
    CONSTRAINT ck_rollout_lane_revision CHECK (revision > 0),
    CONSTRAINT ck_rollout_lane_idempotency CHECK (btrim(idempotency_key) <> ''),
    CONSTRAINT ck_rollout_lane_fingerprint
        CHECK (create_fingerprint ~ '^[0-9a-f]{64}$')
);

CREATE TABLE rollout_lane_channel (
    lane_id UUID NOT NULL,
    org_id BIGINT NOT NULL,
    channel_id BIGINT NOT NULL,
    position INT NOT NULL,
    rollout_id UUID NULL,
    start_idempotency_key VARCHAR(256) NULL,
    start_fingerprint VARCHAR(64) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT pk_rollout_lane_channel PRIMARY KEY (lane_id, channel_id),
    CONSTRAINT fk_rollout_lane_channel_lane
        FOREIGN KEY (lane_id, org_id)
        REFERENCES rollout_lane(id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_rollout_lane_channel_channel
        FOREIGN KEY (channel_id, org_id)
        REFERENCES device_set(id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_rollout_lane_channel_rollout
        FOREIGN KEY (rollout_id, org_id)
        REFERENCES firmware_rollout(id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT uq_rollout_lane_channel_identity
        UNIQUE (lane_id, org_id, channel_id),
    CONSTRAINT uq_rollout_lane_channel_position UNIQUE (lane_id, position),
    CONSTRAINT uq_rollout_lane_physical_channel UNIQUE (channel_id),
    CONSTRAINT uq_rollout_lane_rollout UNIQUE (rollout_id),
    CONSTRAINT uq_rollout_lane_start_key
        UNIQUE (lane_id, start_idempotency_key),
    CONSTRAINT ck_rollout_lane_channel_position CHECK (position >= 0),
    CONSTRAINT ck_rollout_lane_channel_start_pair CHECK (
        (rollout_id IS NULL
            AND start_idempotency_key IS NULL
            AND start_fingerprint IS NULL)
        OR
        (rollout_id IS NOT NULL
            AND start_idempotency_key IS NOT NULL
            AND start_fingerprint IS NOT NULL
            AND btrim(start_idempotency_key) <> ''
            AND start_fingerprint ~ '^[0-9a-f]{64}$')
    )
);

ALTER TABLE rollout_lane
    ADD CONSTRAINT fk_rollout_lane_current_attachment
    FOREIGN KEY (id, org_id, current_channel_id)
    REFERENCES rollout_lane_channel(lane_id, org_id, channel_id)
    DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX idx_rollout_lane_org_updated
    ON rollout_lane(org_id, updated_at DESC, id);

CREATE INDEX idx_rollout_lane_channel_rollout
    ON rollout_lane_channel(rollout_id)
    WHERE rollout_id IS NOT NULL;

CREATE INDEX idx_rollout_member_between_channel_finalize
    ON firmware_rollout_member(enforcement_id)
    WHERE owner_released_at IS NULL
      AND state IN ('admitted', 'reverting')
      AND enforcement_id IS NOT NULL;

CREATE TRIGGER update_rollout_lane_updated_at
    BEFORE UPDATE ON rollout_lane
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

CREATE FUNCTION reject_rollout_lane_channel_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'rollout lane channel attachments are immutable'
        USING ERRCODE = '55000';
END;
$$;

CREATE TRIGGER rollout_lane_channel_immutable
    BEFORE UPDATE OR DELETE ON rollout_lane_channel
    FOR EACH ROW
    EXECUTE FUNCTION reject_rollout_lane_channel_mutation();

CREATE FUNCTION reject_rollout_lane_physical_channel_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM rollout_lane_channel attachment
        WHERE attachment.channel_id = OLD.device_set_id
    ) THEN
        RAISE EXCEPTION 'rollout lane physical channels are immutable'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER rollout_lane_physical_channel_immutable
    BEFORE UPDATE ON device_set_channel
    FOR EACH ROW
    EXECUTE FUNCTION reject_rollout_lane_physical_channel_mutation();

CREATE FUNCTION reject_rollout_lane_device_set_deletion()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM rollout_lane_channel attachment
        WHERE attachment.channel_id = OLD.id
    ) AND (
        TG_OP = 'DELETE'
        OR (OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL)
    ) THEN
        RAISE EXCEPTION 'rollout lane physical channels cannot be deleted'
            USING ERRCODE = '55000';
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER rollout_lane_device_set_deletion_guard
    BEFORE UPDATE OF deleted_at OR DELETE ON device_set
    FOR EACH ROW
    EXECUTE FUNCTION reject_rollout_lane_device_set_deletion();
