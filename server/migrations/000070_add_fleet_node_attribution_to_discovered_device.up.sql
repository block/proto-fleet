ALTER TABLE discovered_device
    ADD COLUMN discovered_by_fleet_node_id BIGINT NULL,
    ADD CONSTRAINT fk_discovered_device_fleet_node
        FOREIGN KEY (discovered_by_fleet_node_id)
        REFERENCES fleet_node(id) ON DELETE SET NULL;

CREATE INDEX idx_discovered_device_fleet_node_attribution
    ON discovered_device(discovered_by_fleet_node_id)
    WHERE discovered_by_fleet_node_id IS NOT NULL;
