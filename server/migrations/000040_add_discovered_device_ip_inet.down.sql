DROP INDEX IF EXISTS idx_discovered_device_ip_inet_gist;

ALTER TABLE discovered_device
DROP COLUMN IF EXISTS ip_address_inet;
