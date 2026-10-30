-- Reviewed identity cannot be reconstructed after dropping this column.
DO $$
BEGIN
    LOCK TABLE pool_configuration_operation_target IN ACCESS EXCLUSIVE MODE;
    IF EXISTS (SELECT 1 FROM pool_configuration_operation_target WHERE reviewed_binding <> '') THEN
        RAISE EXCEPTION 'cannot roll back with retained pool operation bindings';
    END IF;
    IF EXISTS (SELECT 1 FROM pool_configuration_operation_target WHERE physical_attempt_id IS NOT NULL) THEN
        RAISE EXCEPTION 'cannot roll back with retained pool update attempts';
    END IF;
    DROP TRIGGER pool_update_attempt ON pool_configuration_operation_target;
    DROP FUNCTION validate_pool_update_attempt();
    DROP INDEX idx_pool_configuration_operation_target_recovery;
    DROP INDEX idx_pool_configuration_operation_target_open;
    DROP INDEX idx_pool_configuration_operation_target_attempt;
    ALTER TABLE pool_configuration_operation_target DROP COLUMN physical_attempt_id;
    DROP TRIGGER pool_operation_target_identity ON pool_configuration_operation_target;
    DROP FUNCTION keep_pool_operation_target_identity();
    ALTER TABLE pool_configuration_operation_target DROP COLUMN reviewed_binding;
END;
$$;
