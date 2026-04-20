-- ============================================================================
-- Rollback: Remove miner list sorting indexes and generated column
-- ============================================================================

DROP INDEX IF EXISTS idx_discovered_device_sort_name;
DROP INDEX IF EXISTS idx_discovered_device_sort_ip;
DROP INDEX IF EXISTS idx_discovered_device_sort_type;
DROP INDEX IF EXISTS idx_discovered_device_sort_firmware;
DROP INDEX IF EXISTS idx_device_status_sort_status;
DROP INDEX IF EXISTS idx_device_sort_mac;
DROP INDEX IF EXISTS idx_errors_device_open;
