-- Add indexes to optimize miner snapshot queries

-- Primary index for discovered_device base query
-- Covers: WHERE dd.org_id = ? AND dd.is_active = TRUE AND dd.deleted_at IS NULL ORDER BY dd.id
CREATE INDEX idx_discovered_device_org_active_id 
    ON discovered_device(org_id, is_active, deleted_at, id);

-- For type filtering on discovered_device
-- Covers: WHERE dd.org_id = ? AND dd.type IN (...)
CREATE INDEX idx_discovered_device_org_type 
    ON discovered_device(org_id, type, is_active, deleted_at, id);

-- For device LEFT JOIN optimization
-- Covers: LEFT JOIN device d ON dd.id = d.discovered_device_id AND d.deleted_at IS NULL AND d.org_id = ?
CREATE INDEX idx_device_discovered_org_deleted 
    ON device(discovered_device_id, org_id, deleted_at, id);

-- For device_pairing LEFT JOIN optimization
-- Covers: LEFT JOIN device_pairing dp ON d.id = dp.device_id
CREATE INDEX idx_device_pairing_device_status 
    ON device_pairing(device_id, pairing_status, id);

-- Analyze tables to update statistics
ANALYZE TABLE discovered_device, device, device_pairing, device_status;
