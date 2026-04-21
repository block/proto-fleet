-- Add driver_name column to discovered_device for plugin routing.
-- driver_name is the routing key (e.g., "pyasic", "proto", "antminer").

ALTER TABLE discovered_device
    ADD COLUMN driver_name VARCHAR(255);

-- Backfill: direct type matches
UPDATE discovered_device SET driver_name = 'antminer' WHERE type = 'antminer' AND driver_name IS NULL AND deleted_at IS NULL;
UPDATE discovered_device SET driver_name = 'proto' WHERE type = 'proto' AND driver_name IS NULL AND deleted_at IS NULL;
UPDATE discovered_device SET driver_name = 'virtual' WHERE type = 'virtual' AND driver_name IS NULL AND deleted_at IS NULL;

-- Backfill: legacy proto_miner type
UPDATE discovered_device SET driver_name = 'proto' WHERE type = 'proto_miner' AND driver_name IS NULL AND deleted_at IS NULL;

-- Backfill: disambiguate generic "asic" type by model prefix
UPDATE discovered_device SET driver_name = 'antminer' WHERE type = 'asic' AND LOWER(COALESCE(model, '')) LIKE 'antminer%' AND driver_name IS NULL AND deleted_at IS NULL;
UPDATE discovered_device SET driver_name = 'proto' WHERE type = 'asic' AND LOWER(COALESCE(model, '')) LIKE 'rig%' AND driver_name IS NULL AND deleted_at IS NULL;

-- Catch-all: remaining rows (active or soft-deleted) get their type value
-- as driver_name, falling back to 'unknown' if type is also NULL.
UPDATE discovered_device SET driver_name = COALESCE(type, 'unknown') WHERE driver_name IS NULL;

ALTER TABLE discovered_device ALTER COLUMN driver_name SET NOT NULL;

CREATE INDEX idx_discovered_device_driver_name
    ON discovered_device(org_id, driver_name);
