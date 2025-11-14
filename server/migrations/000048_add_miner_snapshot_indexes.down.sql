-- Remove indexes added for miner snapshot optimization

DROP INDEX idx_discovered_device_org_active_id ON discovered_device;
DROP INDEX idx_discovered_device_org_type ON discovered_device;
DROP INDEX idx_device_discovered_org_deleted ON device;
DROP INDEX idx_device_pairing_device_status ON device_pairing;
