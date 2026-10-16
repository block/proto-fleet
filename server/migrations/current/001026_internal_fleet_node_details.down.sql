DROP INDEX idx_pending_enrollment_confirmed_node;
ALTER TABLE pending_enrollment DROP COLUMN confirmed_by;
ALTER TABLE fleet_node_heartbeat DROP COLUMN version, DROP COLUMN ip_addresses;
