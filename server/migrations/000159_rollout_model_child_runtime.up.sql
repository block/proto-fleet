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
