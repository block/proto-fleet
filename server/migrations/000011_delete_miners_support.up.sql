-- ============================================================================
-- Migration: Enable soft-delete for devices and discovered_devices
-- ============================================================================
-- Replace unique constraints with partial unique indexes that only enforce
-- uniqueness among non-deleted rows. This allows soft-deleted devices to be
-- re-discovered and re-paired with new records.

-- Device table: device_identifier uniqueness
ALTER TABLE device DROP CONSTRAINT uq_device_device_identifier;
CREATE UNIQUE INDEX uq_device_device_identifier ON device (device_identifier) WHERE deleted_at IS NULL;

-- Device table: serial_number uniqueness
ALTER TABLE device DROP CONSTRAINT uq_device_serial_number;
CREATE UNIQUE INDEX uq_device_serial_number ON device (serial_number) WHERE deleted_at IS NULL;

-- Discovered device table: (org_id, device_identifier) uniqueness
ALTER TABLE discovered_device DROP CONSTRAINT uk_discovered_device_org_identifier;
CREATE UNIQUE INDEX uk_discovered_device_org_identifier ON discovered_device (org_id, device_identifier) WHERE deleted_at IS NULL;
