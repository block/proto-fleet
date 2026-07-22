DROP TABLE IF EXISTS cohort_reconciler_heartbeat;

DROP TRIGGER IF EXISTS update_device_enforcement_state_updated_at ON device_enforcement_state;
DROP INDEX IF EXISTS idx_device_enforcement_state_config_state;
DROP INDEX IF EXISTS idx_device_enforcement_state_firmware_state;
DROP TABLE IF EXISTS device_enforcement_state;

DROP TRIGGER IF EXISTS update_device_config_state_updated_at ON device_config_state;
DROP INDEX IF EXISTS idx_device_config_state_observed;
DROP TABLE IF EXISTS device_config_state;

DROP TRIGGER IF EXISTS update_device_firmware_state_updated_at ON device_firmware_state;
DROP INDEX IF EXISTS idx_device_firmware_state_observed;
DROP TABLE IF EXISTS device_firmware_state;
