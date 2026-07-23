DROP TRIGGER IF EXISTS clear_infrastructure_rack_on_placement_change ON device_set_rack;
DROP FUNCTION IF EXISTS clear_infrastructure_rack_on_placement_change();
DROP TRIGGER IF EXISTS sync_infrastructure_rack_label ON device_set;
DROP FUNCTION IF EXISTS sync_infrastructure_rack_label();

ALTER TABLE infrastructure_device
    DROP COLUMN rack_name;
