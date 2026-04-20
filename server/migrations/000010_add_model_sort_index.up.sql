-- ============================================================================
-- Migration: Add model sort index
-- ============================================================================

-- Create index for model sorting
CREATE INDEX idx_discovered_device_sort_model
ON discovered_device (org_id, model, id);
