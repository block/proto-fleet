DROP INDEX IF EXISTS idx_discovered_device_driver_name;

ALTER TABLE discovered_device
    DROP COLUMN IF EXISTS driver_name;
