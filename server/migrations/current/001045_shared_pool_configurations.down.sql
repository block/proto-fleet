DO $$
BEGIN
    LOCK TABLE pool_configuration, pool_configuration_revision, pool_configuration_import,
        pool_configuration_assignment, pool_configuration_operation, pool_configuration_operation_target, pool_configuration_review IN ACCESS EXCLUSIVE MODE;
    IF EXISTS (SELECT 1 FROM pool_configuration) THEN
        RAISE EXCEPTION 'cannot roll back with pool configuration data';
    END IF;
    IF EXISTS (SELECT 1 FROM pool_configuration_assignment) OR EXISTS (SELECT 1 FROM pool_configuration_operation)
       OR EXISTS (SELECT 1 FROM pool_configuration_review WHERE saved_version IS NOT NULL) THEN
        RAISE EXCEPTION 'cannot roll back with pool assignment or operation history';
    END IF;
    DROP TABLE pool_configuration_review;
    DROP FUNCTION keep_pool_configuration_review_immutable();
    DROP TABLE pool_configuration_operation_target;
    DROP TABLE pool_configuration_assignment;
    DROP TABLE pool_configuration_operation;
    DROP FUNCTION advance_pool_configuration_assignments();
    DROP TABLE pool_configuration_import;
    ALTER TABLE pool_configuration DROP CONSTRAINT fk_pool_configuration_current_revision;
    DROP TABLE pool_configuration_revision;
    DROP TABLE pool_configuration;
    DROP FUNCTION keep_pool_configuration_revision_immutable();
    DROP FUNCTION keep_pool_configuration_owner_immutable();
    DROP FUNCTION valid_pool_configuration_slots(JSONB);
END;
$$;
