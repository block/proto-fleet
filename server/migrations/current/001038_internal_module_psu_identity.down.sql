-- A position-keyed runtime cannot safely interpret retained PSU movement history.
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM container_module_placement WHERE ended_at IS NOT NULL) THEN
  RAISE EXCEPTION 'module placement history exists; preserve evidence and fix forward';
 END IF;
END $$;
DROP TRIGGER container_module_site_readings ON device;
DROP FUNCTION withdraw_container_module_site_readings();
DROP TRIGGER container_module_placement ON container_module;
DROP FUNCTION record_container_module_placement();
DROP TRIGGER hashboard_installation_placement ON hashboard_installation;
DROP FUNCTION capture_hashboard_installation_placement();
ALTER TABLE hashboard_installation DROP COLUMN device_id,DROP COLUMN tank_id,DROP COLUMN module_position;
DROP TABLE container_module_placement;
DROP TRIGGER container_module_placement_org ON container_module;
DROP FUNCTION check_container_module_placement_org();
DROP TABLE container_module_identity;
DROP FUNCTION check_container_module_identity_org();
