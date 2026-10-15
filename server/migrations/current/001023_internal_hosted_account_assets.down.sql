-- Preserve allocation history and its stale-write witnesses on manual rollback.
DO $$
BEGIN
 LOCK TABLE hosted_account_asset IN ACCESS EXCLUSIVE MODE;
 IF EXISTS (SELECT 1 FROM hosted_account_asset) THEN
  RAISE EXCEPTION 'preserve or remove reporting allocation data before rolling back hosted account assets';
 END IF;
 DROP TABLE hosted_account_asset;
 DROP FUNCTION check_hosted_account_asset();
END;
$$;
