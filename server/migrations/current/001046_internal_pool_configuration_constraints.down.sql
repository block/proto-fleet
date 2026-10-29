DO $$
BEGIN
    LOCK TABLE pool_configuration, pool_configuration_assignment IN ACCESS EXCLUSIVE MODE;
    IF EXISTS (SELECT 1 FROM pool_configuration_assignment WHERE target_kind = 'hashboard_installation') THEN
        RAISE EXCEPTION 'cannot roll back with hashboard pool assignment history';
    END IF;
    DROP TRIGGER pool_configuration_assignment_installation ON pool_configuration_assignment;
    DROP FUNCTION validate_pool_assignment_installation();
    ALTER TABLE pool_configuration DROP CONSTRAINT fk_pool_configuration_hosted_owner;
END;
$$;
