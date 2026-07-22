ALTER TABLE infrastructure_device
    ADD COLUMN rack_name VARCHAR(100) NOT NULL DEFAULT '';

-- Infrastructure devices currently expose rack placement by label. Keep that
-- denormalized label synchronized with the rack catalog so renames cannot
-- leave stale assignments. A rack move/unassign or delete clears the
-- assignment: the operator must explicitly place the infrastructure device in
-- a rack at its new location.
CREATE FUNCTION sync_infrastructure_rack_label()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.type = 'rack' AND OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL THEN
        UPDATE infrastructure_device
        SET rack_name = ''
        WHERE org_id = OLD.org_id
          AND rack_name = OLD.label
          AND deleted_at IS NULL;
    ELSIF OLD.type = 'rack' AND OLD.label IS DISTINCT FROM NEW.label THEN
        UPDATE infrastructure_device
        SET rack_name = NEW.label
        WHERE org_id = OLD.org_id
          AND rack_name = OLD.label
          AND deleted_at IS NULL;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER sync_infrastructure_rack_label
    AFTER UPDATE OF label, deleted_at ON device_set
    FOR EACH ROW
    EXECUTE FUNCTION sync_infrastructure_rack_label();

CREATE FUNCTION clear_infrastructure_rack_on_placement_change()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.site_id IS DISTINCT FROM NEW.site_id
        OR OLD.building_id IS DISTINCT FROM NEW.building_id THEN
        UPDATE infrastructure_device AS infrastructure
        SET rack_name = ''
        FROM device_set AS rack
        WHERE rack.id = NEW.device_set_id
          AND infrastructure.org_id = rack.org_id
          AND infrastructure.rack_name = rack.label
          AND infrastructure.deleted_at IS NULL;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER clear_infrastructure_rack_on_placement_change
    AFTER UPDATE OF site_id, building_id ON device_set_rack
    FOR EACH ROW
    EXECUTE FUNCTION clear_infrastructure_rack_on_placement_change();
