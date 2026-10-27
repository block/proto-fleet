DROP TABLE container_ha_observation;
DROP INDEX idx_discovered_device_endpoint_key;
ALTER TABLE discovered_device DROP COLUMN endpoints, DROP COLUMN endpoint_key;
