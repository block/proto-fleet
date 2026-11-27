DROP TRIGGER unlocated_hashboard_site_readings ON device;
DROP FUNCTION withdraw_unlocated_hashboard_site_readings();
ALTER TABLE container_inventory DROP COLUMN unlocated_hashboards;
