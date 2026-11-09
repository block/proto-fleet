DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pool_configuration_operation WHERE kind<>'apply')
    OR EXISTS(SELECT 1 FROM pool_configuration_review WHERE kind='remove') THEN
  RAISE EXCEPTION 'retain pool operation history; rollback requires a coordinated backup restore';
 END IF;
END $$;
ALTER TABLE pool_configuration_operation_target DROP CONSTRAINT pool_configuration_operation_target_state_check;
ALTER TABLE pool_configuration_operation_target ADD CONSTRAINT pool_configuration_operation_target_state_check
    CHECK (state IN ('pending','applying','applied_unverified','verified','failed','different','superseded'));
ALTER TABLE pool_configuration_operation DROP COLUMN kind;
ALTER TABLE pool_configuration_review DROP CONSTRAINT pool_configuration_review_kind_check;
ALTER TABLE pool_configuration_review ADD CONSTRAINT pool_configuration_review_kind_check CHECK (kind IN ('edit','assign'));
