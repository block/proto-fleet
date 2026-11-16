DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM container_mining_group_state WHERE step_key IS NOT NULL AND status IN ('pending','applying','blocked')) THEN
  RAISE EXCEPTION 'Settle saved mining-group requests before removing dispatch tracking';
 END IF;
END $$;
DROP TABLE mining_group_step_dispatch;
