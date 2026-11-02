DO $rollback$ BEGIN
 LOCK TABLE device,hosted_account_asset,hosted_account_asset_interval,hashboard_observation IN ACCESS EXCLUSIVE MODE;
 IF EXISTS (SELECT 1 FROM hosted_account_asset WHERE kind=7) OR EXISTS (SELECT 1 FROM hosted_account_asset_interval WHERE kind=7) THEN
  RAISE EXCEPTION 'hashboard allocations contain retained history; restore a coordinated backup to roll back';
 END IF;
DROP TRIGGER record_hosted_hashboard_history ON hashboard_metrics;
DROP TRIGGER record_hosted_hashboard_history ON hashboard_diagnostic_observation;
DROP FUNCTION record_hosted_hashboard_history_sample();
DROP INDEX idx_hosted_account_asset_interval_capture_hashboard;
DROP VIEW hosted_mining_asset;
DROP VIEW hosted_account_asset_projection;
DROP TRIGGER hosted_hashboard_placement ON hashboard_observation;
DROP FUNCTION move_hosted_hashboard_allocation();
DROP FUNCTION hosted_account_source_authorized(bigint,integer,bigint,bigint,bigint,bigint,text,timestamptz,timestamptz);
DROP VIEW hosted_module_membership;
ALTER TABLE hosted_account_asset DROP CONSTRAINT ck_hosted_account_asset_kind;
ALTER TABLE hosted_account_asset DROP CONSTRAINT ck_hosted_account_asset_identity;
ALTER TABLE hosted_account_asset DROP COLUMN hashboard_id;
ALTER TABLE hosted_account_asset ADD CONSTRAINT hosted_account_asset_kind_check CHECK (kind IN (5,6));
ALTER TABLE hosted_account_asset ADD CONSTRAINT hosted_account_asset_check CHECK (
 (kind=5 AND miner_id IS NOT NULL AND miner_id=asset_id AND module_id IS NULL AND device_id=asset_id) OR
 (kind=6 AND module_id IS NOT NULL AND module_id=asset_id AND miner_id IS NULL));
ALTER TABLE hosted_account_asset_interval DROP CONSTRAINT ck_hosted_account_asset_interval_kind;
ALTER TABLE hosted_account_asset_interval ADD CONSTRAINT hosted_account_asset_interval_kind_check CHECK (kind IN (5,6));
CREATE OR REPLACE FUNCTION check_hosted_account_asset() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.kind=6 AND NOT EXISTS (SELECT 1 FROM container_module m JOIN container_tank t ON t.id=m.tank_id WHERE m.id=NEW.module_id AND t.device_id=NEW.device_id) THEN
  RAISE EXCEPTION 'module controller mismatch' USING ERRCODE='23514';
 END IF;
 IF NEW.client_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM hosted_account a WHERE a.id=NEW.client_id AND a.organization_id=NEW.organization_id AND a.depth=1) THEN
  RAISE EXCEPTION 'allocation requires customer' USING ERRCODE='23514';
 END IF;
 IF NEW.subclient_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM hosted_account a WHERE a.id=NEW.subclient_id AND a.organization_id=NEW.organization_id AND a.parent_id=NEW.client_id AND a.depth=2) THEN
  RAISE EXCEPTION 'allocation requires direct subclient' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE OR REPLACE FUNCTION record_hosted_account_asset_interval() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE boundary TIMESTAMPTZ := clock_timestamp();
BEGIN
 IF TG_OP='UPDATE' AND NEW.client_id IS NOT DISTINCT FROM OLD.client_id AND NEW.subclient_id IS NOT DISTINCT FROM OLD.subclient_id THEN RETURN NEW; END IF;
 UPDATE hosted_account_asset_interval SET ended_at=boundary
 WHERE organization_id=NEW.organization_id AND kind=NEW.kind AND asset_id=NEW.asset_id AND ended_at IS NULL;
 IF NEW.client_id IS NOT NULL THEN
  INSERT INTO hosted_account_asset_interval(organization_id,kind,asset_id,device_id,client_id,subclient_id,started_at,site_id,device_identifier)
  SELECT NEW.organization_id,NEW.kind,NEW.asset_id,NEW.device_id,NEW.client_id,NEW.subclient_id,boundary,d.site_id,d.device_identifier FROM device d WHERE d.id=NEW.device_id AND d.deleted_at IS NULL;
 END IF;
 RETURN NEW;
END;
$$;
CREATE FUNCTION hosted_account_source_authorized(
 organization BIGINT, asset_kind INTEGER, asset BIGINT, client BIGINT, subclient BIGINT,
 sample_site BIGINT, identifier TEXT, oldest TIMESTAMPTZ, newest TIMESTAMPTZ
) RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
 SELECT COALESCE(oldest IS NOT NULL AND newest IS NOT NULL AND newest>=oldest AND EXISTS (
  SELECT 1 FROM hosted_account_asset_interval i
  WHERE i.organization_id=organization AND i.kind=asset_kind AND i.asset_id=asset
   AND i.client_id=client AND COALESCE(i.subclient_id,0)=COALESCE(subclient,0)
   AND COALESCE(i.site_id,0)=COALESCE(sample_site,0) AND i.device_identifier=identifier
   AND oldest>=i.started_at AND (i.ended_at IS NULL OR newest<i.ended_at)
 ),false);
$$;
END;
$rollback$;
