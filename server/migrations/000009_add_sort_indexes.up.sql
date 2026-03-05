-- ============================================================================
-- Migration: Add indexes to support miner list sorted pagination
-- ============================================================================

-- Index for name sorting (manufacturer then model)
CREATE INDEX idx_discovered_device_sort_name
ON discovered_device (org_id, manufacturer, model, id);

-- Index for IP address sorting (numeric sort via INET)
CREATE INDEX idx_discovered_device_sort_ip
ON discovered_device (org_id, INET(COALESCE(NULLIF(ip_address, ''), '0.0.0.0')), id);

-- Index for device type sorting
CREATE INDEX idx_discovered_device_sort_type
ON discovered_device (org_id, type, id);

-- Index for status sorting (alphabetical)
CREATE INDEX idx_device_status_sort_status
ON device_status (status, device_id);

-- Index for MAC address sorting
CREATE INDEX idx_device_sort_mac
ON device (mac_address, discovered_device_id);

-- Index for firmware version sorting
CREATE INDEX idx_discovered_device_sort_firmware
ON discovered_device (org_id, firmware_version, id);

-- Partial index for counting open errors per device (issues sorting)
CREATE INDEX idx_errors_device_open
ON errors (device_id)
WHERE closed_at IS NULL;
