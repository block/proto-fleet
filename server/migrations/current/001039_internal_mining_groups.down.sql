-- Lock before inspecting state so a concurrent writer cannot commit a journal
-- between the refusal guard and DROP. Keep the guard and DDL in one statement.
DO $$
BEGIN
 LOCK TABLE container_mining_group_state, hashboard_pool_intent, mining_group_request IN ACCESS EXCLUSIVE MODE;
 IF EXISTS (SELECT 1 FROM container_mining_group_state
            WHERE status='failed' OR (transfer_failure_count>0 AND status IN ('pending','applying','blocked'))) THEN
  RAISE EXCEPTION 'mining group transfer recovery state must be resolved before rollback';
 END IF;
 IF EXISTS(SELECT 1 FROM container_mining_group_state WHERE status IN ('pending','applying','blocked')) THEN
  RAISE EXCEPTION 'finish mining group operations before rollback';
 END IF;
 IF EXISTS(SELECT 1 FROM container_mining_group_state WHERE bindings <> '{}'::jsonb)
    OR EXISTS(SELECT 1 FROM hashboard_pool_intent) THEN
  RAISE EXCEPTION 'preserve or remove mining group bindings and intents before rollback';
 END IF;
 DROP TABLE mining_group_request;
 DROP TABLE hashboard_pool_intent;
 DROP TABLE container_mining_group_state;
 DELETE FROM role_permission WHERE permission_id IN (SELECT id FROM permission WHERE key='miner:activate_mining_groups');
 DELETE FROM permission WHERE key='miner:activate_mining_groups';
END;
$$;
