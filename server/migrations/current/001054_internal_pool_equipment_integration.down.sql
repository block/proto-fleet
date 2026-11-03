-- Retained controller bindings must keep their immutable execution snapshots.
DO $$
BEGIN
    LOCK TABLE pool_configuration_group_snapshot, pool_configuration_operation_target IN ACCESS EXCLUSIVE MODE;
    IF EXISTS (SELECT 1 FROM pool_configuration_group_snapshot) THEN
        RAISE EXCEPTION 'cannot roll back shared board configurations with retained controller snapshots';
    END IF;

    ALTER TABLE device_write_attempt DROP COLUMN command_type;

    DROP INDEX idx_pool_configuration_operation_target_group_attempt;
    DROP INDEX idx_pool_configuration_operation_target_device_attempt;
    CREATE UNIQUE INDEX idx_pool_configuration_operation_target_attempt
     ON pool_configuration_operation_target(physical_attempt_id) WHERE physical_attempt_id IS NOT NULL;
    ALTER TABLE pool_configuration_operation_target DROP COLUMN group_snapshot_id;
    DROP TABLE pool_configuration_group_snapshot;

    DROP TRIGGER pool_assignment_adoption ON pool_configuration_assignment;
    DROP FUNCTION validate_pool_assignment_adoption();

END;
$$;
