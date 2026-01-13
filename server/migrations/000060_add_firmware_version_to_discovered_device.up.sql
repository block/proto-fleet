ALTER TABLE discovered_device
ADD COLUMN firmware_version VARCHAR(255) NULL AFTER type;
