-- Re-add the type column to discovered_device.
-- Backfill from driver_name since they were semantically equivalent.

ALTER TABLE discovered_device ADD COLUMN type VARCHAR(255) NOT NULL DEFAULT '';

UPDATE discovered_device SET type = driver_name WHERE deleted_at IS NULL;

CREATE INDEX idx_discovered_device_type ON discovered_device(org_id, type);
