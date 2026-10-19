-- requires-shared: 1000
-- Reporting-only ownership; recalled rows remain to prevent ABA updates.
CREATE TABLE hosted_account_asset (
 organization_id BIGINT NOT NULL REFERENCES organization(id),
 kind INTEGER NOT NULL CHECK (kind IN (5,6)),
 asset_id BIGINT NOT NULL CHECK (asset_id > 0),
 device_id BIGINT NOT NULL,
 miner_id BIGINT REFERENCES device(id),
 module_id BIGINT REFERENCES container_module(id),
 client_id BIGINT,
 subclient_id BIGINT,
 revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 PRIMARY KEY (organization_id,kind,asset_id),
 FOREIGN KEY (device_id,organization_id) REFERENCES device(id,org_id),
 FOREIGN KEY (organization_id,client_id) REFERENCES hosted_account(organization_id,id),
 FOREIGN KEY (organization_id,subclient_id) REFERENCES hosted_account(organization_id,id),
 CHECK ((kind=5 AND miner_id IS NOT NULL AND miner_id=asset_id AND module_id IS NULL AND device_id=asset_id) OR
        (kind=6 AND module_id IS NOT NULL AND module_id=asset_id AND miner_id IS NULL)),
 CHECK (subclient_id IS NULL OR client_id IS NOT NULL)
);
CREATE INDEX idx_hosted_account_asset_client ON hosted_account_asset(organization_id,client_id);
CREATE INDEX idx_hosted_account_asset_subclient ON hosted_account_asset(organization_id,subclient_id);
CREATE FUNCTION check_hosted_account_asset() RETURNS TRIGGER LANGUAGE plpgsql AS $$
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
CREATE TRIGGER hosted_account_asset_chain BEFORE INSERT OR UPDATE ON hosted_account_asset FOR EACH ROW EXECUTE FUNCTION check_hosted_account_asset();
