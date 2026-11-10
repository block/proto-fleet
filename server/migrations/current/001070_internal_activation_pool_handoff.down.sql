-- Keep the lock, refusal guard, and DROP atomic without relying on the runner
-- to supply an outer transaction. Completed rows also retain recovery evidence.
DO $$
BEGIN
 LOCK TABLE mining_group_pool_handoff IN ACCESS EXCLUSIVE MODE;
 IF EXISTS (SELECT 1 FROM mining_group_pool_handoff) THEN
  RAISE EXCEPTION 'retain activation pool handoff evidence; rollback requires a coordinated backup restore';
 END IF;
 DROP TABLE mining_group_pool_handoff;
END;
$$;
