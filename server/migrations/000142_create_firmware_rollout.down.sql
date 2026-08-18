DROP TRIGGER IF EXISTS update_firmware_rollout_evidence_updated_at
    ON firmware_rollout_evidence;
DROP TABLE IF EXISTS firmware_rollout_evidence;

DROP TABLE IF EXISTS firmware_rollout_cause;

DROP TRIGGER IF EXISTS update_firmware_rollout_control_updated_at
    ON firmware_rollout_control;
DROP TABLE IF EXISTS firmware_rollout_control;

DROP TRIGGER IF EXISTS update_firmware_rollout_member_updated_at
    ON firmware_rollout_member;
DROP TABLE IF EXISTS firmware_rollout_member;

DROP TRIGGER IF EXISTS update_firmware_rollout_batch_updated_at
    ON firmware_rollout_batch;
DROP TABLE IF EXISTS firmware_rollout_batch;

DROP TRIGGER IF EXISTS update_firmware_rollout_updated_at
    ON firmware_rollout;
DROP TABLE IF EXISTS firmware_rollout;

ALTER TABLE channel_firmware_enforcement
    DROP CONSTRAINT IF EXISTS uq_channel_firmware_enforcement_id_org_device;
