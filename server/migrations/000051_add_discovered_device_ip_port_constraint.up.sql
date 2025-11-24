-- Add unique constraint on (org_id, ip_address, port) to prevent duplicate discoveries
-- This ensures that rescanning the same IP:port updates the existing record instead of inserting duplicates
-- The constraint works alongside the existing uk_discovered_device_org_identifier constraint:
--   - uk_discovered_device_org_identifier: ensures device_identifier uniqueness (after pairing when stable IDs are available)
--   - uk_discovered_device_org_ip_port: ensures network endpoint uniqueness (during discovery phase)
ALTER TABLE discovered_device
ADD UNIQUE KEY uk_discovered_device_org_ip_port (org_id, ip_address, port);
