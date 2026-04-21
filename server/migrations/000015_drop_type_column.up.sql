-- Drop the legacy type column from discovered_device.
-- All routing now uses driver_name (added in migration 000014).

DROP INDEX IF EXISTS idx_discovered_device_type;
ALTER TABLE discovered_device DROP COLUMN type;
