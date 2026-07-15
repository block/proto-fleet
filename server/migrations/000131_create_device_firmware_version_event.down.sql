DROP TRIGGER IF EXISTS record_device_firmware_version_event ON device_firmware_state;
DROP FUNCTION IF EXISTS record_device_firmware_version_event();
DROP INDEX IF EXISTS idx_device_firmware_version_event_device_observed;
DROP TABLE IF EXISTS device_firmware_version_event;
