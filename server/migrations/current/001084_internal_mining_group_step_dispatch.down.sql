-- Fence step producers before checking their journals. Keep the guard and
-- removal atomic even when this file runs outside the checkpoint runner.
DO $$ BEGIN
 LOCK TABLE container_mining_group_state, mining_group_step_dispatch IN ACCESS EXCLUSIVE MODE;
 IF EXISTS (SELECT 1 FROM container_mining_group_state WHERE step_key IS NOT NULL AND status IN ('pending','applying','blocked')) THEN
  RAISE EXCEPTION 'Settle saved mining-group requests before removing dispatch tracking';
 END IF;
 DROP TABLE mining_group_step_dispatch;
END $$;
