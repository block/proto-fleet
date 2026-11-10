-- requires-shared: 1073
LOCK TABLE device_pairing IN SHARE ROW EXCLUSIVE MODE;

CREATE OR REPLACE FUNCTION enqueue_pairing_pool_transition() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE organization bigint; class integer;
BEGIN
 IF NEW.pairing_status NOT IN ('PAIRED','DEFAULT_PASSWORD') THEN RETURN NEW; END IF;
 IF TG_OP='UPDATE' AND OLD.pairing_status IN ('PAIRED','DEFAULT_PASSWORD') AND OLD.unpaired_at IS NULL THEN RETURN NEW; END IF;
 SELECT d.org_id,dd.device_class INTO organization,class FROM device d JOIN discovered_device dd ON dd.id=d.discovered_device_id WHERE d.id=NEW.device_id AND d.deleted_at IS NULL;
 IF NOT FOUND THEN RETURN NEW; END IF;
 IF class=2 THEN
  INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,reason,pairing_defaults)
  SELECT organization,NEW.device_id,0,NEW.device_id,'pairing',COALESCE(jsonb_object_agg(COALESCE(c.account_id,0)::text,jsonb_build_object('configuration_id',c.configuration_id,'revision_id',c.current_revision_id)),'{}')
  FROM pool_configuration c WHERE c.organization_id=organization AND c.is_default AND NOT c.archived
   AND (c.account_id IS NULL OR EXISTS(SELECT 1 FROM hosted_account_asset a WHERE a.organization_id=organization AND a.device_id=NEW.device_id AND COALESCE(a.subclient_id,a.client_id)=c.account_id));
 ELSE
  INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,owner_account_id,allocation_revision,reason,configuration_id,revision_id)
  SELECT organization,NEW.device_id,5,NEW.device_id,COALESCE(a.subclient_id,a.client_id,0),COALESCE(a.revision,0),'pairing',c.configuration_id,c.current_revision_id
  FROM (SELECT 1) seed LEFT JOIN hosted_account_asset a ON a.organization_id=organization AND a.kind=5 AND a.asset_id=NEW.device_id
  LEFT JOIN pool_configuration c ON c.organization_id=organization AND COALESCE(c.account_id,0)=COALESCE(a.subclient_id,a.client_id,0) AND c.is_default AND NOT c.archived;
 END IF;
 RETURN NEW;
END $$;
DROP TABLE pool_pairing_cycle;
