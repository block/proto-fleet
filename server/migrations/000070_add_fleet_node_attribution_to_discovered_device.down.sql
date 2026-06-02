DROP INDEX IF EXISTS idx_discovered_device_fleet_node_attribution;

ALTER TABLE discovered_device
    DROP CONSTRAINT IF EXISTS fk_discovered_device_fleet_node,
    DROP COLUMN IF EXISTS discovered_by_fleet_node_id;
