-- Allocation periods cannot be reconstructed after rollback. Preserve both
-- active and recalled periods; use a coordinated backup or fix forward.
DO $$
BEGIN
 LOCK TABLE device,hosted_account_asset,hosted_account_asset_interval IN ACCESS EXCLUSIVE MODE;
 IF EXISTS (SELECT 1 FROM hosted_account_asset_interval) THEN
  RAISE EXCEPTION 'preserve established allocation intervals; restore a coordinated backup or fix forward instead of rolling back hosted account history';
 END IF;
 DROP TRIGGER hosted_account_asset_placement ON device;
 DROP FUNCTION record_hosted_account_asset_placement();
 DROP TRIGGER hosted_account_asset_history ON hosted_account_asset;
 DROP FUNCTION record_hosted_account_asset_interval();
 DROP FUNCTION hosted_account_source_authorized(BIGINT,INTEGER,BIGINT,BIGINT,BIGINT,BIGINT,TEXT,TIMESTAMPTZ,TIMESTAMPTZ);
 DROP TABLE hosted_account_asset_interval;
 DROP TABLE hosted_account_history_epoch;
END;
$$;
