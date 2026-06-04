-- Covers ListFleetNodeDiscoveredDevices: the operator listing filters by
-- (org_id, discovered_by_fleet_node_id) over active, non-deleted rows. The
-- existing org_active and fleet_node_attribution indexes each cover only part
-- of that predicate, so the node-filtered listing fell back to a wider scan.
CREATE INDEX idx_discovered_device_fleet_node_active
    ON discovered_device(org_id, discovered_by_fleet_node_id)
    WHERE is_active = TRUE AND deleted_at IS NULL;
