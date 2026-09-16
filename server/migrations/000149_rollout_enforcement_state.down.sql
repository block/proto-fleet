DROP TABLE IF EXISTS firmware_rollout_reservation;

ALTER TABLE device_firmware_deployment
    DROP COLUMN last_command_batch_uuid;

ALTER TABLE release_channel_firmware
    DROP COLUMN previous_firmware_checksum,
    DROP COLUMN previous_firmware_version;
