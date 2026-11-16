-- requires-shared: 1000
ALTER TABLE container_mining_group_state ADD COLUMN default_return JSONB;
ALTER TABLE container_mining_group_state DROP CONSTRAINT chk_container_mining_group_state_operation_kind;
ALTER TABLE container_mining_group_state ADD CONSTRAINT chk_container_mining_group_state_operation_kind
 CHECK (operation_kind IN ('','activate','deactivate','pools','return_default'));
