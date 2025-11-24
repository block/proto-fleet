-- Remove the unique constraint on (org_id, ip_address, port)
ALTER TABLE discovered_device
DROP INDEX uk_discovered_device_org_ip_port;
