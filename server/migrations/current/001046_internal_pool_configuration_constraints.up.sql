-- requires-shared: 1045
ALTER TABLE pool_configuration ADD CONSTRAINT fk_pool_configuration_hosted_owner
    FOREIGN KEY (organization_id, account_id)
    REFERENCES hosted_account(organization_id, id);

-- Private topology adds installation validation to the shared assignment store.
CREATE FUNCTION validate_pool_assignment_installation() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.target_kind = 'hashboard_installation' AND NEW.retired_at IS NULL AND NOT EXISTS (
        SELECT 1 FROM hashboard_installation i
        JOIN hashboard h ON h.id = i.hashboard_id
        JOIN container_module m ON m.id = i.module_id
        JOIN container_tank t ON t.id = m.tank_id
        WHERE i.id = NEW.target_id AND h.org_id = NEW.organization_id
          AND t.device_id = NEW.device_id AND i.device_id = NEW.device_id AND i.removed_at IS NULL
          AND m.retired_at IS NULL AND t.present
    ) THEN
        RAISE EXCEPTION 'pool assignment requires the current hashboard installation' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER pool_configuration_assignment_installation
    BEFORE INSERT OR UPDATE ON pool_configuration_assignment
    FOR EACH ROW EXECUTE FUNCTION validate_pool_assignment_installation();
