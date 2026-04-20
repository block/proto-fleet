-- ============================================================================
-- Rollback: Remove model sort index
-- ============================================================================

-- Drop the model sorting index
DROP INDEX IF EXISTS idx_discovered_device_sort_model;
