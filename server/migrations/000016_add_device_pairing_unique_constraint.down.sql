-- Remove unique constraint on device_id
ALTER TABLE device_pairing DROP CONSTRAINT uq_device_pairing_device_id;