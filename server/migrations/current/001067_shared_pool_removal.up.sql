-- Removal uses the same review, operation and target lifecycle as assignment.
ALTER TABLE pool_configuration_review DROP CONSTRAINT pool_configuration_review_kind_check;
ALTER TABLE pool_configuration_review ADD CONSTRAINT pool_configuration_review_kind_check
    CHECK (kind IN ('edit','assign','remove'));
ALTER TABLE pool_configuration_operation ADD COLUMN kind TEXT NOT NULL DEFAULT 'apply'
    CHECK (kind IN ('apply','clear','unassign'));
ALTER TABLE pool_configuration_operation_target DROP CONSTRAINT pool_configuration_operation_target_state_check;
ALTER TABLE pool_configuration_operation_target ADD CONSTRAINT pool_configuration_operation_target_state_check
    CHECK (state IN ('pending','applying','applied_unverified','verified','failed','different','superseded','completed','uncertain'));
