DROP INDEX IF EXISTS idx_channel_firmware_enforcement_model_identity;
DROP INDEX IF EXISTS idx_firmware_rollout_group_child;

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
         AND membership.device_set_id = declaration.current_channel_id
        WHERE declaration.id = NEW.lane_model_id
          AND declaration.lane_id = NEW.lane_id
          AND declaration.org_id = NEW.org_id
          AND declaration.current_channel_id = NEW.channel_id
          AND declaration.model_identity_key = NEW.model_identity_key
    ) THEN
        RAISE EXCEPTION
            'active rollout lane model binding must match physical membership and declaration'
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

ALTER TABLE channel_firmware_enforcement
    DROP CONSTRAINT IF EXISTS ck_channel_firmware_enforcement_model_identity,
    DROP COLUMN IF EXISTS model_identity_validated_at,
    DROP COLUMN IF EXISTS expected_model_identity_key;

ALTER TABLE firmware_rollout_control
    DROP CONSTRAINT IF EXISTS ck_firmware_rollout_control_admission_attempt,
    DROP COLUMN IF EXISTS admission_attempt;

ALTER TABLE firmware_rollout_member
    DROP CONSTRAINT IF EXISTS ck_firmware_rollout_member_model_identity,
    DROP COLUMN IF EXISTS model_identity_validated_at,
    DROP COLUMN IF EXISTS model_identity_key;

ALTER TABLE firmware_rollout_batch
    DROP CONSTRAINT IF EXISTS ck_firmware_rollout_batch_admission_attempt,
    DROP COLUMN IF EXISTS admission_attempt;

ALTER TABLE firmware_rollout
    DROP CONSTRAINT IF EXISTS ck_firmware_rollout_model_child_shape,
    DROP CONSTRAINT IF EXISTS fk_firmware_rollout_target_release_target,
    DROP CONSTRAINT IF EXISTS fk_firmware_rollout_source_release_target,
    DROP CONSTRAINT IF EXISTS fk_firmware_rollout_lane_model,
    DROP CONSTRAINT IF EXISTS fk_firmware_rollout_group,
    DROP COLUMN IF EXISTS target_release_target_id,
    DROP COLUMN IF EXISTS source_release_target_id,
    DROP COLUMN IF EXISTS model_identity_validated_at,
    DROP COLUMN IF EXISTS model_identity_key,
    DROP COLUMN IF EXISTS lane_model_id,
    DROP COLUMN IF EXISTS lane_id,
    DROP COLUMN IF EXISTS group_id;
