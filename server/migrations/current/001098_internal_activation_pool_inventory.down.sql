LOCK TABLE mining_group_pool_handoff IN EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM mining_group_pool_handoff WHERE placement_pending) THEN
  RAISE EXCEPTION 'Complete activation pool assignment transfers before rollback.';
 END IF;
END $$;

DROP TRIGGER protect_activation_hashboard_owner ON hosted_account_asset;
DROP FUNCTION protect_activation_hashboard_owner();
DROP TRIGGER protect_activation_pool_handoff ON pool_configuration_assignment;
DROP FUNCTION protect_activation_pool_handoff();
DROP INDEX ux_mining_group_pool_handoff_pending_device;
ALTER TABLE mining_group_pool_handoff DROP COLUMN placement_pending;
