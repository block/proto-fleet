DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM container_mining_group_state WHERE operation_kind='return_default') THEN
  RAISE EXCEPTION 'Cannot remove return-to-Default workflow history';
 END IF;
END $$;
ALTER TABLE container_mining_group_state DROP CONSTRAINT chk_container_mining_group_state_operation_kind;
ALTER TABLE container_mining_group_state ADD CONSTRAINT chk_container_mining_group_state_operation_kind
 CHECK (operation_kind IN ('','activate','deactivate','pools'));
ALTER TABLE container_mining_group_state DROP COLUMN default_return;
