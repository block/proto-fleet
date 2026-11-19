LOCK TABLE mining_group_pool_handoff IN EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM mining_group_pool_handoff WHERE placement_pending) THEN
  RAISE EXCEPTION 'Complete activation pool assignment transfers before rollback.';
 END IF;
END $$;

CREATE OR REPLACE FUNCTION retire_reallocated_pool_assignments(organization bigint, controller bigint, asset_kind integer, asset bigint)
RETURNS void LANGUAGE SQL AS $$
 WITH retired AS (
  UPDATE pool_configuration_assignment a SET retired_at=CURRENT_TIMESTAMP,
   version=a.version+1,updated_at=CURRENT_TIMESTAMP
  WHERE a.organization_id=organization AND a.retired_at IS NULL
   AND ((a.target_kind='device' AND a.device_id=controller) OR
    (a.target_kind='hashboard_installation' AND EXISTS (
     SELECT 1 FROM hashboard_installation i WHERE i.id=a.target_id
      AND ((asset_kind=6 AND i.module_id=asset) OR (asset_kind=7 AND i.hashboard_id=asset)))))
  RETURNING a.organization_id,a.target_kind,a.target_id
 )
 UPDATE pool_configuration_operation_target t SET state='superseded',updated_at=CURRENT_TIMESTAMP
 FROM retired a WHERE t.organization_id=a.organization_id AND t.target_kind=a.target_kind AND t.target_id=a.target_id
  AND t.state IN ('pending','applying','failed','different','applied_unverified');
$$;

DROP TRIGGER protect_activation_hashboard_owner ON hosted_account_asset;
DROP FUNCTION protect_activation_hashboard_owner();
DROP TRIGGER protect_activation_pool_handoff ON pool_configuration_assignment;
DROP FUNCTION protect_activation_pool_handoff();
DROP INDEX idx_mining_group_pool_handoff_pending_device;
ALTER TABLE mining_group_pool_handoff DROP COLUMN placement_pending;
