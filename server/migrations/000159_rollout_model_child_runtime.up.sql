ALTER TABLE firmware_rollout
    ADD COLUMN group_id UUID NULL,
    ADD COLUMN lane_id UUID NULL,
    ADD COLUMN lane_model_id UUID NULL,
    ADD COLUMN model_identity_key TEXT NULL,
    ADD COLUMN model_identity_validated_at TIMESTAMPTZ NULL,
    ADD COLUMN source_release_target_id BIGINT NULL,
    ADD COLUMN target_release_target_id BIGINT NULL,
    ADD CONSTRAINT fk_firmware_rollout_group
        FOREIGN KEY (group_id, org_id)
        REFERENCES firmware_rollout_group(id, org_id)
        ON DELETE RESTRICT,
    ADD CONSTRAINT fk_firmware_rollout_lane_model
        FOREIGN KEY (lane_model_id, lane_id, org_id)
        REFERENCES rollout_lane_model(id, lane_id, org_id)
        ON DELETE RESTRICT,
    ADD CONSTRAINT fk_firmware_rollout_source_release_target
        FOREIGN KEY (source_release_target_id, source_release_set_id, org_id)
        REFERENCES firmware_release_target(id, release_set_id, org_id)
        ON DELETE RESTRICT,
    ADD CONSTRAINT fk_firmware_rollout_target_release_target
        FOREIGN KEY (target_release_target_id, target_release_set_id, org_id)
        REFERENCES firmware_release_target(id, release_set_id, org_id)
        ON DELETE RESTRICT,
    ADD CONSTRAINT fk_firmware_rollout_source_physical_release
        FOREIGN KEY (source_channel_id, org_id, source_release_set_id)
        REFERENCES device_set_channel(device_set_id, org_id, release_set_id)
        ON DELETE RESTRICT,
    ADD CONSTRAINT fk_firmware_rollout_target_physical_release
        FOREIGN KEY (target_channel_id, org_id, target_release_set_id)
        REFERENCES device_set_channel(device_set_id, org_id, release_set_id)
        ON DELETE RESTRICT,
    ADD CONSTRAINT uq_firmware_rollout_child_topology
        UNIQUE (id, org_id, group_id, lane_id, lane_model_id),
    ADD CONSTRAINT ck_firmware_rollout_model_child_shape CHECK (
        (
            group_id IS NULL
            AND lane_id IS NULL
            AND lane_model_id IS NULL
            AND model_identity_key IS NULL
            AND model_identity_validated_at IS NULL
            AND source_release_target_id IS NULL
            AND target_release_target_id IS NULL
        )
        OR
        (
            group_id IS NOT NULL
            AND lane_id IS NOT NULL
            AND lane_model_id IS NOT NULL
            AND model_identity_key LIKE 'v1:%'
            AND model_identity_validated_at IS NOT NULL
            AND source_release_target_id IS NOT NULL
            AND target_release_target_id IS NOT NULL
        )
    );

ALTER TABLE firmware_rollout_group_model
    DROP CONSTRAINT fk_firmware_rollout_group_model_child,
    ADD CONSTRAINT fk_firmware_rollout_group_model_child
        FOREIGN KEY (
            child_rollout_id,
            org_id,
            group_id,
            lane_id,
            lane_model_id
        )
        REFERENCES firmware_rollout(
            id,
            org_id,
            group_id,
            lane_id,
            lane_model_id
        )
        ON DELETE RESTRICT;

CREATE FUNCTION validate_legacy_rollout_lane_attachment()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    attached_group_id UUID;
    topology_enabled BOOLEAN;
BEGIN
    IF NEW.rollout_id IS NULL THEN
        RETURN NEW;
    END IF;

    SELECT rollout.group_id
    INTO attached_group_id
    FROM firmware_rollout rollout
    WHERE rollout.id = NEW.rollout_id
      AND rollout.org_id = NEW.org_id;

    IF NOT FOUND THEN
        RETURN NEW;
    END IF;
    IF attached_group_id IS NOT NULL THEN
        RETURN NEW;
    END IF;

    INSERT INTO rollout_lane_topology_cutover (org_id)
    VALUES (NEW.org_id)
    ON CONFLICT (org_id) DO NOTHING;

    SELECT cutover.enabled
    INTO topology_enabled
    FROM rollout_lane_topology_cutover cutover
    WHERE cutover.org_id = NEW.org_id
    FOR UPDATE;

    IF topology_enabled THEN
        RAISE EXCEPTION
            'legacy rollout attachment is disabled after topology cutover'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER rollout_lane_channel_legacy_attachment_gate
    BEFORE INSERT OR UPDATE OF rollout_id ON rollout_lane_channel
    FOR EACH ROW
    WHEN (NEW.rollout_id IS NOT NULL)
    EXECUTE FUNCTION validate_legacy_rollout_lane_attachment();

ALTER TABLE firmware_rollout_batch
    ADD COLUMN admission_attempt INT NOT NULL DEFAULT 0,
    ADD CONSTRAINT ck_firmware_rollout_batch_admission_attempt
        CHECK (admission_attempt >= 0);

ALTER TABLE firmware_rollout_member
    ADD COLUMN model_identity_key TEXT NULL,
    ADD COLUMN model_identity_validated_at TIMESTAMPTZ NULL,
    ADD CONSTRAINT ck_firmware_rollout_member_model_identity CHECK (
        (
            model_identity_key IS NULL
            AND model_identity_validated_at IS NULL
        )
        OR
        (
            model_identity_key LIKE 'v1:%'
            AND model_identity_validated_at IS NOT NULL
        )
    );

ALTER TABLE firmware_rollout_control
    ADD COLUMN admission_attempt INT NULL,
    ADD CONSTRAINT ck_firmware_rollout_control_admission_attempt CHECK (
        admission_attempt IS NULL OR admission_attempt >= 0
    );

ALTER TABLE channel_firmware_enforcement
    ADD COLUMN expected_model_identity_key TEXT NULL,
    ADD COLUMN model_identity_validated_at TIMESTAMPTZ NULL,
    ADD CONSTRAINT ck_channel_firmware_enforcement_model_identity CHECK (
        (
            expected_model_identity_key IS NULL
            AND model_identity_validated_at IS NULL
        )
        OR
        (
            expected_model_identity_key LIKE 'v1:%'
            AND model_identity_validated_at IS NOT NULL
        )
    );

CREATE INDEX idx_firmware_rollout_group_child
    ON firmware_rollout(org_id, group_id, lane_model_id)
    WHERE group_id IS NOT NULL;

CREATE INDEX idx_channel_firmware_enforcement_model_identity
    ON channel_firmware_enforcement(org_id, state, model_identity_validated_at)
    WHERE expected_model_identity_key IS NOT NULL
      AND state IN ('pending', 'held', 'verifying');

CREATE OR REPLACE FUNCTION validate_rollout_lane_model_binding()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.ended_at IS NULL AND NOT EXISTS (
        SELECT 1
        FROM rollout_lane_model declaration
        JOIN device_set_membership membership
          ON membership.org_id = declaration.org_id
         AND membership.device_id = NEW.device_id
         AND membership.device_set_type = 'channel'
         AND membership.device_set_id = NEW.channel_id
        WHERE declaration.id = NEW.lane_model_id
          AND declaration.lane_id = NEW.lane_id
          AND declaration.org_id = NEW.org_id
          AND declaration.model_identity_key = NEW.model_identity_key
          AND (
              NEW.channel_id = declaration.current_channel_id
              OR EXISTS (
                  SELECT 1
                  FROM firmware_rollout_group_model grouped
                  JOIN firmware_rollout child
                    ON child.id = grouped.child_rollout_id
                   AND child.org_id = grouped.org_id
                  JOIN firmware_rollout_member child_member
                    ON child_member.rollout_id = child.id
                   AND child_member.org_id = child.org_id
                   AND child_member.device_id = NEW.device_id
                  WHERE grouped.lane_model_id = declaration.id
                    AND grouped.lane_id = declaration.lane_id
                    AND grouped.org_id = declaration.org_id
                    AND grouped.target_channel_id = NEW.channel_id
                    AND child_member.state = 'succeeded'
                    AND child.state IN (
                        'running',
                        'paused',
                        'review',
                        'completed',
                        'completed_with_failures'
                    )
              )
          )
    ) THEN
        RAISE EXCEPTION
            'active rollout lane model binding must match physical membership and declaration or frozen child target'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.ended_at IS NOT NULL AND EXISTS (
        SELECT 1
        FROM rollout_lane_topology_cutover cutover
        JOIN device_set_membership membership
          ON membership.org_id = NEW.org_id
         AND membership.device_id = NEW.device_id
         AND membership.device_set_type = 'channel'
         AND membership.device_set_id = NEW.channel_id
        WHERE cutover.org_id = NEW.org_id
          AND cutover.enabled
    ) THEN
        RAISE EXCEPTION
            'ended rollout lane model binding cannot retain physical membership'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
