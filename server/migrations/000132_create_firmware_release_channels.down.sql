DROP TRIGGER IF EXISTS firmware_release_target_immutable ON firmware_release_target;
DROP TRIGGER IF EXISTS firmware_release_set_immutable ON firmware_release_set;
DROP FUNCTION IF EXISTS reject_firmware_release_mutation();

DROP TABLE IF EXISTS device_set_channel;
DROP TABLE IF EXISTS firmware_release_target;
DROP TABLE IF EXISTS firmware_release_set;

DELETE FROM device_set_membership
WHERE device_set_type = 'channel';

DELETE FROM device_set
WHERE type = 'channel';

ALTER TABLE device_set_membership
    DROP CONSTRAINT IF EXISTS fk_device_set_membership_device_set_org;

DROP INDEX IF EXISTS idx_one_channel_per_device;
