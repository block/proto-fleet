-- requires-shared: 1000
-- Establish ownership prospectively. Existing ownership proves only the period
-- beginning at this migration; it must never be projected into older samples.
-- Fence assignments and placement changes before the launch boundary and seed.
LOCK TABLE device,hosted_account_asset IN SHARE ROW EXCLUSIVE MODE;
CREATE TABLE hosted_account_history_epoch (
 singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
 started_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
INSERT INTO hosted_account_history_epoch(singleton) VALUES(TRUE);
CREATE TABLE hosted_account_asset_interval (
 id BIGSERIAL PRIMARY KEY,
 organization_id BIGINT NOT NULL REFERENCES organization(id),
 kind INTEGER NOT NULL CHECK (kind IN (5,6)),
 asset_id BIGINT NOT NULL,
 device_id BIGINT NOT NULL,
 site_id BIGINT,
 device_identifier TEXT NOT NULL,
 client_id BIGINT NOT NULL,
 subclient_id BIGINT,
 started_at TIMESTAMPTZ NOT NULL,
 ended_at TIMESTAMPTZ,
 FOREIGN KEY (device_id,organization_id) REFERENCES device(id,org_id),
 FOREIGN KEY (organization_id,client_id) REFERENCES hosted_account(organization_id,id),
 FOREIGN KEY (organization_id,subclient_id) REFERENCES hosted_account(organization_id,id),
 CHECK (ended_at IS NULL OR ended_at >= started_at)
);
CREATE UNIQUE INDEX idx_hosted_account_asset_interval_open ON hosted_account_asset_interval(organization_id,kind,asset_id) WHERE ended_at IS NULL;
CREATE INDEX idx_hosted_account_asset_interval_asset ON hosted_account_asset_interval(organization_id,kind,asset_id,started_at);
CREATE INDEX idx_hosted_account_asset_interval_device ON hosted_account_asset_interval(device_id) WHERE ended_at IS NULL;
CREATE INDEX idx_hosted_account_asset_interval_client ON hosted_account_asset_interval(organization_id,client_id,started_at);
CREATE INDEX idx_hosted_account_asset_interval_subclient ON hosted_account_asset_interval(organization_id,subclient_id,started_at);
INSERT INTO hosted_account_asset_interval(organization_id,kind,asset_id,device_id,client_id,subclient_id,started_at,site_id,device_identifier)
SELECT a.organization_id,a.kind,a.asset_id,a.device_id,a.client_id,a.subclient_id,e.started_at,d.site_id,d.device_identifier
FROM hosted_account_asset a JOIN device d ON d.id=a.device_id CROSS JOIN hosted_account_history_epoch e WHERE client_id IS NOT NULL AND d.deleted_at IS NULL;
CREATE FUNCTION record_hosted_account_asset_interval() RETURNS TRIGGER LANGUAGE plpgsql AS $$
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
CREATE TRIGGER hosted_account_asset_history AFTER INSERT OR UPDATE ON hosted_account_asset FOR EACH ROW EXECUTE FUNCTION record_hosted_account_asset_interval();

CREATE FUNCTION record_hosted_account_asset_placement() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE boundary TIMESTAMPTZ := clock_timestamp();
BEGIN
 IF NEW.deleted_at IS NOT NULL THEN
  UPDATE hosted_account_asset_interval SET ended_at=boundary WHERE device_id=NEW.id AND ended_at IS NULL;
  RETURN NEW;
 END IF;
 IF NEW.site_id IS NOT DISTINCT FROM OLD.site_id AND NEW.device_identifier=OLD.device_identifier THEN RETURN NEW; END IF;
 WITH closed AS (
  UPDATE hosted_account_asset_interval SET ended_at=boundary WHERE device_id=NEW.id AND ended_at IS NULL RETURNING *
 )
 INSERT INTO hosted_account_asset_interval(organization_id,kind,asset_id,device_id,client_id,subclient_id,started_at,site_id,device_identifier)
 SELECT organization_id,kind,asset_id,device_id,client_id,subclient_id,boundary,NEW.site_id,NEW.device_identifier FROM closed;
 RETURN NEW;
END;
$$;
CREATE TRIGGER hosted_account_asset_placement AFTER UPDATE OF site_id,device_identifier,deleted_at ON device FOR EACH ROW EXECUTE FUNCTION record_hosted_account_asset_placement();

-- A current allocation does not authorize cached observations from a previous
-- owner or placement. Every contributor must fit one established interval.
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
