-- Restore indexes removed in 000030_remove_unused_indexes.up.sql

CREATE INDEX idx_discovered_device_sort_name
    ON discovered_device (org_id, manufacturer, model, id);

CREATE INDEX idx_discovered_device_org ON discovered_device(org_id);

CREATE INDEX idx_discovered_device_identifier ON discovered_device(device_identifier);

CREATE INDEX idx_discovered_device_driver_name
    ON discovered_device(org_id, driver_name);
