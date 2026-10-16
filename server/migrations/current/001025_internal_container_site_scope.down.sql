DROP TRIGGER container_class_placement ON discovered_device;
DROP FUNCTION enforce_container_class_placement();
DROP TRIGGER container_rack_placement ON device_set_membership;
DROP FUNCTION enforce_container_rack_placement();
DROP TRIGGER container_device_placement ON device;
DROP FUNCTION enforce_container_device_placement();
DROP VIEW container_scope;
ALTER TABLE container_inventory DROP CONSTRAINT fk_container_inventory_device_org;
ALTER TABLE container_inventory DROP COLUMN org_id;
