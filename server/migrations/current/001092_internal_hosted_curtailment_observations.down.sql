LOCK TABLE hosted_curtailment_confirmation,hosted_curtailment_asset IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM hosted_curtailment_confirmation) THEN
  RAISE EXCEPTION 'cannot discard captured curtailment confirmations; fix forward or restore a coordinated backup';
 END IF;
END $$;
DROP TABLE hosted_curtailment_asset;
DROP TABLE hosted_curtailment_confirmation;
