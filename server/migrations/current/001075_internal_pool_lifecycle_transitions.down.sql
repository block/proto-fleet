-- requires-shared: 1073
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pool_lifecycle_transition) THEN
  RAISE EXCEPTION 'retain pool lifecycle history; restore a coordinated backup to roll back';
 END IF;
END $$;
ALTER TABLE pool_configuration_operation DROP CONSTRAINT ck_pool_configuration_operation_actor;
ALTER TABLE pool_configuration_operation ADD CONSTRAINT pool_configuration_operation_actor_id_check CHECK(actor_id>0);
ALTER TABLE pool_configuration_operation DROP COLUMN lifecycle_id;
DROP TRIGGER device_pairing_pool_transition ON device_pairing;
DROP FUNCTION enqueue_pairing_pool_transition();
DROP TRIGGER hosted_allocation_pool_transition ON hosted_account_asset;
DROP FUNCTION enqueue_ownership_pool_transition();
DROP VIEW pool_lifecycle_removal;
DROP TABLE pool_lifecycle_transition;
DROP FUNCTION validate_pool_lifecycle_transition();
