-- requires-shared: 1073
-- A durable event captures the destination policy in the ownership transaction.
-- Preparation resolves physical targets; device I/O happens only after commit.
CREATE TABLE pool_lifecycle_transition (
 transition_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 organization_id BIGINT NOT NULL REFERENCES organization(id),
 device_id BIGINT NOT NULL,
 asset_kind INTEGER NOT NULL CHECK (asset_kind IN (0,5,7)),
 asset_id BIGINT NOT NULL,
 owner_account_id BIGINT NOT NULL DEFAULT 0,
 allocation_revision BIGINT NOT NULL DEFAULT 0,
 pairing_defaults JSONB NOT NULL DEFAULT '{}',
 reason TEXT NOT NULL CHECK (reason IN ('pairing','ownership')),
 configuration_id VARCHAR(36), revision_id VARCHAR(36),
 target_kind TEXT NOT NULL DEFAULT '', target_id BIGINT NOT NULL DEFAULT 0,
 reviewed_binding TEXT NOT NULL DEFAULT '',
 state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','ready','applying','verified','completed','failed','uncertain','superseded')),
 result_code TEXT NOT NULL DEFAULT '',
 operation_id VARCHAR(36), physical_attempt_id UUID REFERENCES device_write_attempt(attempt_id),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 CONSTRAINT fk_pool_lifecycle_device FOREIGN KEY (device_id,organization_id) REFERENCES device(id,org_id),
 CONSTRAINT fk_pool_lifecycle_revision FOREIGN KEY (organization_id,configuration_id,revision_id)
  REFERENCES pool_configuration_revision(organization_id,configuration_id,revision_id),
 CONSTRAINT ck_pool_lifecycle_configuration CHECK ((configuration_id IS NULL)=(revision_id IS NULL))
);
CREATE INDEX idx_pool_lifecycle_pending ON pool_lifecycle_transition(next_attempt_at) WHERE state IN ('pending','ready');
CREATE INDEX idx_pool_lifecycle_device ON pool_lifecycle_transition(organization_id,device_id);

CREATE FUNCTION validate_pool_lifecycle_transition() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' THEN
  IF ROW(NEW.organization_id,NEW.device_id,NEW.asset_kind,NEW.asset_id,NEW.owner_account_id,NEW.allocation_revision,NEW.reason,NEW.configuration_id,NEW.revision_id,NEW.pairing_defaults,NEW.created_at)
   IS DISTINCT FROM ROW(OLD.organization_id,OLD.device_id,OLD.asset_kind,OLD.asset_id,OLD.owner_account_id,OLD.allocation_revision,OLD.reason,OLD.configuration_id,OLD.revision_id,OLD.pairing_defaults,OLD.created_at) THEN
   RAISE EXCEPTION 'pool lifecycle policy is immutable' USING ERRCODE='23514';
  END IF;
  IF OLD.state<>'pending' AND ROW(NEW.target_kind,NEW.target_id,NEW.reviewed_binding) IS DISTINCT FROM ROW(OLD.target_kind,OLD.target_id,OLD.reviewed_binding) THEN
   RAISE EXCEPTION 'prepared pool lifecycle binding is immutable' USING ERRCODE='23514';
  END IF;
  IF OLD.physical_attempt_id IS NOT NULL AND NEW.physical_attempt_id IS DISTINCT FROM OLD.physical_attempt_id AND NOT (
   NEW.physical_attempt_id IS NULL AND OLD.state='failed' AND NEW.state='ready' AND EXISTS(
    SELECT 1 FROM device_write_attempt w WHERE w.attempt_id=OLD.physical_attempt_id AND w.status='complete' AND w.write_outcome='{}'::jsonb
   )) THEN
   RAISE EXCEPTION 'pool lifecycle attempt is immutable after dispatch' USING ERRCODE='23514';
  END IF;
 END IF;
 IF NEW.physical_attempt_id IS NOT NULL AND (TG_OP='INSERT' OR OLD.physical_attempt_id IS NULL) AND NOT EXISTS(
  SELECT 1 FROM device_write_attempt w WHERE w.attempt_id=NEW.physical_attempt_id AND w.organization_id=NEW.organization_id
   AND w.device_id=NEW.device_id AND w.operation_kind='mutation' AND w.status='running'
   AND NEW.state='applying' AND NEW.configuration_id IS NULL AND NEW.reason='ownership'
 ) THEN
  RAISE EXCEPTION 'pool lifecycle clearing requires its current physical admission' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pool_lifecycle_transition_binding BEFORE INSERT OR UPDATE ON pool_lifecycle_transition
 FOR EACH ROW EXECUTE FUNCTION validate_pool_lifecycle_transition();

CREATE FUNCTION enqueue_ownership_pool_transition() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE owner bigint;
BEGIN
 IF TG_OP='UPDATE' AND NEW.client_id IS NOT DISTINCT FROM OLD.client_id
  AND NEW.subclient_id IS NOT DISTINCT FROM OLD.subclient_id AND NEW.device_id=OLD.device_id THEN RETURN NEW; END IF;
 IF TG_OP='INSERT' AND NEW.client_id IS NULL THEN RETURN NEW; END IF;
 owner := COALESCE(NEW.subclient_id,NEW.client_id,0);
 UPDATE pool_lifecycle_transition SET state='superseded',updated_at=clock_timestamp()
 WHERE organization_id=NEW.organization_id AND ((asset_kind=NEW.kind AND asset_id=NEW.asset_id) OR (asset_kind=0 AND NEW.kind=5 AND device_id=NEW.device_id))
  AND state IN ('pending','ready','failed');
 INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,owner_account_id,allocation_revision,reason,configuration_id,revision_id)
 SELECT NEW.organization_id,NEW.device_id,NEW.kind,NEW.asset_id,owner,NEW.revision,'ownership',c.configuration_id,c.current_revision_id
 FROM (SELECT 1) seed LEFT JOIN pool_configuration c ON c.organization_id=NEW.organization_id
  AND COALESCE(c.account_id,0)=owner AND c.is_default AND NOT c.archived;
 RETURN NEW;
END $$;
CREATE TRIGGER hosted_allocation_pool_transition AFTER INSERT OR UPDATE ON hosted_account_asset
 FOR EACH ROW EXECUTE FUNCTION enqueue_ownership_pool_transition();

CREATE FUNCTION enqueue_pairing_pool_transition() RETURNS trigger LANGUAGE plpgsql AS $$
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
CREATE TRIGGER device_pairing_pool_transition AFTER INSERT OR UPDATE ON device_pairing
 FOR EACH ROW EXECUTE FUNCTION enqueue_pairing_pool_transition();

ALTER TABLE pool_configuration_operation ADD COLUMN lifecycle_id UUID REFERENCES pool_lifecycle_transition(transition_id);
ALTER TABLE pool_configuration_operation DROP CONSTRAINT pool_configuration_operation_actor_id_check;
ALTER TABLE pool_configuration_operation ADD CONSTRAINT ck_pool_configuration_operation_actor
 CHECK (actor_id>0 OR (actor_id=0 AND lifecycle_id IS NOT NULL));

CREATE VIEW pool_lifecycle_removal AS
SELECT l.transition_id AS removal_id,l.organization_id,NULLIF(l.owner_account_id,0)::bigint AS account_id,0::bigint AS actor_id,
 l.target_kind,l.target_id,l.device_id,''::varchar(36) AS configuration_id,''::varchar(36) AS revision_id,
 l.allocation_revision AS assignment_version,l.reviewed_binding,'different'::text AS verification_state,true AS clear_supported,
 false AS keep_device_settings,CASE WHEN l.state='ready' THEN 'pending' ELSE l.state END::text AS state,l.result_code,
 l.physical_attempt_id,l.created_at,l.updated_at,COALESCE(w.status,'')::text AS attempt_status
FROM pool_lifecycle_transition l LEFT JOIN device_write_attempt w ON w.attempt_id=l.physical_attempt_id
WHERE l.configuration_id IS NULL AND l.reason='ownership' AND l.target_id>0;
