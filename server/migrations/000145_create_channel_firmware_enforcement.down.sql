DROP TRIGGER IF EXISTS update_channel_firmware_enforcement_updated_at
    ON channel_firmware_enforcement;
DROP TABLE IF EXISTS channel_firmware_enforcement;

DROP TRIGGER IF EXISTS update_channel_firmware_authority_updated_at
    ON channel_firmware_authority;
DROP TABLE IF EXISTS channel_firmware_authority;

ALTER TABLE queue_message
    DROP CONSTRAINT IF EXISTS ck_queue_message_max_attempts,
    DROP COLUMN IF EXISTS max_attempts;

ALTER TABLE firmware_release_target
    DROP CONSTRAINT IF EXISTS uq_firmware_release_target_id_set_org;
