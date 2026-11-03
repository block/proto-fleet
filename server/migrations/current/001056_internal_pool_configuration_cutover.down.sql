DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pool_configuration_cutover) OR EXISTS(SELECT 1 FROM pool_configuration_board_intent) THEN
  RAISE EXCEPTION 'pool cutover rollback requires a coordinated backup; retained execution intent cannot be discarded';
 END IF;
END $$;
DROP TRIGGER legacy_pool_command_cutover ON queue_message;
DROP FUNCTION reject_legacy_pool_command();
DROP TRIGGER legacy_board_intent_cutover ON hashboard_pool_intent;
DROP TRIGGER legacy_pool_profile_cutover ON hosted_account_pool_profile;
DROP TRIGGER legacy_pool_revision_cutover ON hosted_account_pool_revision;
DROP TRIGGER legacy_hosted_pool_cutover ON hosted_account_pool;
DROP TRIGGER legacy_pool_cutover ON pool;
DROP FUNCTION reject_legacy_pool_writer();
DROP TABLE pool_configuration_cutover;
DROP TABLE pool_configuration_board_intent;
ALTER TABLE pool_configuration_group_snapshot DROP CONSTRAINT uq_pool_group_snapshot_device;
