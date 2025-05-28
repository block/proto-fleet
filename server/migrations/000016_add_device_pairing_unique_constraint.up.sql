-- Add unique constraint on device_id to prevent duplicate pairing records
ALTER TABLE device_pairing ADD CONSTRAINT uq_device_pairing_device_id UNIQUE (device_id);