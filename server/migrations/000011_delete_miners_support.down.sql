-- ============================================================================
-- Rollback: Restore original unique constraints
-- ============================================================================
-- WARNING: Permanently deletes all soft-deleted device data.
-- Soft-deleted rows must be removed before restoring absolute unique constraints,
-- because a soft-deleted device and its re-paired replacement share the same
-- device_identifier/serial_number, violating a non-partial unique constraint.

-- Remove child records of soft-deleted devices (NO ACTION FKs)
DELETE FROM queue_message WHERE device_id IN (SELECT id FROM device WHERE deleted_at IS NOT NULL);
DELETE FROM command_on_device_log WHERE device_id IN (SELECT id FROM device WHERE deleted_at IS NOT NULL);
DELETE FROM device_pairing WHERE device_id IN (SELECT id FROM device WHERE deleted_at IS NOT NULL);
DELETE FROM device_status WHERE device_id IN (SELECT id FROM device WHERE deleted_at IS NOT NULL);

-- Remove soft-deleted devices (CASCADE handles errors + miner_credentials)
DELETE FROM device WHERE deleted_at IS NOT NULL;

-- Remove soft-deleted discovered devices (safe now — no device FKs remain)
DELETE FROM discovered_device WHERE deleted_at IS NOT NULL;

-- Device table: restore device_identifier unique constraint
DROP INDEX IF EXISTS uq_device_device_identifier;
ALTER TABLE device ADD CONSTRAINT uq_device_device_identifier UNIQUE (device_identifier);

-- Device table: restore serial_number unique constraint
DROP INDEX IF EXISTS uq_device_serial_number;
ALTER TABLE device ADD CONSTRAINT uq_device_serial_number UNIQUE (serial_number);

-- Discovered device table: restore (org_id, device_identifier) unique constraint
DROP INDEX IF EXISTS uk_discovered_device_org_identifier;
ALTER TABLE discovered_device ADD CONSTRAINT uk_discovered_device_org_identifier UNIQUE (org_id, device_identifier);
