DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM channel_firmware_enforcement
        GROUP BY device_id
        HAVING COUNT(*) > 1
    ) THEN
        RAISE EXCEPTION
            'cannot downgrade while devices have multiple firmware enforcement records';
    END IF;
END;
$$;

DROP TRIGGER IF EXISTS rollout_lane_device_set_deletion_guard
    ON device_set;
DROP FUNCTION IF EXISTS reject_rollout_lane_device_set_deletion();

DROP TRIGGER IF EXISTS rollout_lane_physical_channel_immutable
    ON device_set_channel;
DROP FUNCTION IF EXISTS reject_rollout_lane_physical_channel_mutation();

DROP TRIGGER IF EXISTS rollout_lane_channel_immutable
    ON rollout_lane_channel;
DROP FUNCTION IF EXISTS reject_rollout_lane_channel_mutation();

ALTER TABLE rollout_lane
    DROP CONSTRAINT IF EXISTS fk_rollout_lane_current_attachment;

DROP INDEX IF EXISTS idx_rollout_member_between_channel_finalize;

DROP TABLE IF EXISTS rollout_lane_channel;
DROP TABLE IF EXISTS rollout_lane;

ALTER TABLE firmware_rollout_member
    DROP CONSTRAINT IF EXISTS fk_firmware_rollout_member_source_target,
    DROP CONSTRAINT IF EXISTS fk_firmware_rollout_member_target_target,
    DROP CONSTRAINT IF EXISTS ck_firmware_rollout_member_source_target_pair,
    DROP CONSTRAINT IF EXISTS ck_firmware_rollout_member_target_target_pair,
    DROP COLUMN IF EXISTS source_release_set_id,
    DROP COLUMN IF EXISTS source_release_target_id,
    DROP COLUMN IF EXISTS target_release_set_id,
    DROP COLUMN IF EXISTS target_release_target_id,
    DROP COLUMN IF EXISTS revert_selected_at;

DROP INDEX IF EXISTS uq_channel_firmware_enforcement_authority_device;
DROP INDEX IF EXISTS uq_channel_firmware_enforcement_active_device;

ALTER TABLE channel_firmware_enforcement
    ADD CONSTRAINT uq_channel_firmware_enforcement_device UNIQUE (device_id);
