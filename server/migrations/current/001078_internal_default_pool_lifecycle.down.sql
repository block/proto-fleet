-- requires-shared: 1067
-- Fence producers before checking history, and retain every lock through DDL.
-- This single statement is atomic even outside the normal migration runner.
DO $$
BEGIN
 LOCK TABLE organization IN EXCLUSIVE MODE;
 LOCK TABLE device_pairing, hosted_account_asset, hashboard_installation IN ACCESS EXCLUSIVE MODE;
 LOCK TABLE pool_lifecycle_transition IN ACCESS EXCLUSIVE MODE;
 IF EXISTS(SELECT 1 FROM pool_lifecycle_transition) THEN
  RAISE EXCEPTION 'retain pool lifecycle history; restore a coordinated backup to roll back';
 END IF;

 DROP TRIGGER hashboard_reinstalled_pool_transition ON hashboard_installation;
 DROP FUNCTION resume_reinstalled_board_pool_transition();
 DROP TRIGGER hashboard_removed_pool_transitions ON hashboard_installation;
 DROP FUNCTION retire_removed_board_pool_transitions();
 DROP TRIGGER pool_lifecycle_absent_board ON pool_lifecycle_transition;
 DROP FUNCTION retire_absent_board_pool_transition();
 DROP TRIGGER device_pairing_pool_transition ON device_pairing;
 DROP FUNCTION enqueue_pairing_pool_transition();
 DROP TRIGGER hosted_allocation_pool_transition ON hosted_account_asset;
 DROP FUNCTION enqueue_ownership_pool_transition();
 DROP TABLE pool_pairing_cycle;

 ALTER TABLE pool_configuration_operation DROP CONSTRAINT ck_pool_configuration_operation_actor;
 ALTER TABLE pool_configuration_operation ADD CONSTRAINT pool_configuration_operation_actor_id_check CHECK(actor_id>0);
 ALTER TABLE pool_configuration_operation DROP COLUMN lifecycle_id;
 DROP VIEW pool_lifecycle_removal;
 DROP TABLE pool_lifecycle_transition;
 DROP FUNCTION validate_pool_lifecycle_transition();

 DROP INDEX idx_pool_configuration_owner_default;
 ALTER TABLE pool_configuration DROP CONSTRAINT ck_pool_configuration_active_default;
 ALTER TABLE pool_configuration DROP COLUMN is_default;
END;
$$;
