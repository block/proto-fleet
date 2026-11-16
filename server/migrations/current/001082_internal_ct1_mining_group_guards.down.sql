-- Retained deactivation journals are physical-write evidence. Do not erase them
-- or make their retry identity unreadable during an application rollback.
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM container_mining_group_state WHERE operation_kind='deactivate') THEN
  RAISE EXCEPTION 'retain deactivation journals; use a coordinated application and database backup for rollback';
 END IF;
 IF EXISTS (SELECT 1 FROM pool_lifecycle_transition WHERE state='deferred') THEN
  RAISE EXCEPTION 'retain deferred pool lifecycle work; settle deactivation before database downgrade';
 END IF;
END $$;
DROP TRIGGER pool_lifecycle_mining_group_deactivation ON pool_lifecycle_transition;
DROP FUNCTION guard_pool_lifecycle_mining_group_deactivation();
DROP FUNCTION ct1_hashboard_allocation_ready(bigint,bigint);
DROP FUNCTION pool_lifecycle_clear_retryable(pool_lifecycle_transition,device_write_attempt);
DROP INDEX idx_pool_configuration_operation_target_open;
CREATE INDEX idx_pool_configuration_operation_target_open
 ON pool_configuration_operation_target(organization_id,target_kind,target_id)
 WHERE state IN ('pending','applying','failed','different','applied_unverified');
DROP INDEX idx_hosted_account_asset_allocated_hashboard_device;
ALTER TABLE pool_lifecycle_transition DROP CONSTRAINT chk_pool_lifecycle_transition_state;
ALTER TABLE pool_lifecycle_transition ADD CONSTRAINT pool_lifecycle_transition_state_check
 CHECK (state IN ('pending','ready','applying','verified','completed','failed','uncertain','superseded'));
DROP INDEX idx_pool_lifecycle_transition_open_asset;
CREATE INDEX idx_pool_lifecycle_transition_open_asset
 ON pool_lifecycle_transition(state,organization_id,asset_kind,asset_id)
 WHERE state IN ('pending','ready','applying','uncertain','failed');
ALTER TABLE container_mining_group_state DROP CONSTRAINT chk_container_mining_group_state_operation_kind;
ALTER TABLE container_mining_group_state ADD CONSTRAINT container_mining_group_state_operation_kind_check
 CHECK (operation_kind IN ('','activate','pools'));
DELETE FROM role_permission WHERE permission_id IN (SELECT id FROM permission WHERE key='miner:deactivate_mining_groups');
DELETE FROM permission WHERE key='miner:deactivate_mining_groups';

-- Restore the lifecycle trigger behavior from migration 1078.
CREATE OR REPLACE FUNCTION enqueue_ownership_pool_transition() RETURNS trigger LANGUAGE plpgsql AS $$
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

CREATE OR REPLACE FUNCTION enqueue_pairing_pool_transition() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE organization bigint; class integer; cycle uuid;
BEGIN
 -- Authentication/endpoint recovery is still the same pairing cycle. Only
 -- an explicit unpair resets it, including local unpairs without unpaired_at.
 IF NEW.pairing_status='UNPAIRED' THEN
  DELETE FROM pool_pairing_cycle WHERE device_id=NEW.device_id;
  -- Cancel unclaimed intent, including expanded CT1 children. Accepted work
  -- checks the cycle at dispatch; its physical evidence and recovery stay intact.
  UPDATE pool_lifecycle_transition SET state='superseded',result_code='pairing_cycle_ended',updated_at=clock_timestamp()
  WHERE organization_id=(SELECT org_id FROM device WHERE id=NEW.device_id) AND device_id=NEW.device_id
   AND reason='pairing' AND state IN ('pending','ready')
   AND operation_id IS NULL AND physical_attempt_id IS NULL;
  RETURN NEW;
 END IF;
 IF NEW.pairing_status NOT IN ('PAIRED','DEFAULT_PASSWORD') THEN RETURN NEW; END IF;
 SELECT d.org_id,dd.device_class INTO organization,class FROM device d JOIN discovered_device dd ON dd.id=d.discovered_device_id WHERE d.id=NEW.device_id AND d.deleted_at IS NULL;
 IF NOT FOUND THEN RETURN NEW; END IF;
 INSERT INTO pool_pairing_cycle(device_id) VALUES(NEW.device_id) ON CONFLICT DO NOTHING RETURNING cycle_id INTO cycle;
 IF NOT FOUND THEN RETURN NEW; END IF;
 IF class=2 THEN
  INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,reason,pairing_cycle_id,pairing_defaults)
  SELECT organization,NEW.device_id,0,NEW.device_id,'pairing',cycle,COALESCE(jsonb_object_agg(COALESCE(c.account_id,0)::text,jsonb_build_object('configuration_id',c.configuration_id,'revision_id',c.current_revision_id)),'{}')
  FROM pool_configuration c WHERE c.organization_id=organization AND c.is_default AND NOT c.archived
   AND (c.account_id IS NULL OR EXISTS(SELECT 1 FROM hosted_account_asset a WHERE a.organization_id=organization AND a.device_id=NEW.device_id AND COALESCE(a.subclient_id,a.client_id)=c.account_id));
 ELSE
  INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,owner_account_id,allocation_revision,reason,pairing_cycle_id,configuration_id,revision_id)
  SELECT organization,NEW.device_id,5,NEW.device_id,COALESCE(a.subclient_id,a.client_id,0),COALESCE(a.revision,0),'pairing',cycle,c.configuration_id,c.current_revision_id
  FROM (SELECT 1) seed LEFT JOIN hosted_account_asset a ON a.organization_id=organization AND a.kind=5 AND a.asset_id=NEW.device_id
  LEFT JOIN pool_configuration c ON c.organization_id=organization AND COALESCE(c.account_id,0)=COALESCE(a.subclient_id,a.client_id,0) AND c.is_default AND NOT c.archived;
 END IF;
 RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION retire_absent_board_pool_transition() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.asset_kind=7 AND NEW.state='pending' AND NEW.operation_id IS NULL AND NEW.physical_attempt_id IS NULL
  AND NOT EXISTS(SELECT 1 FROM hashboard_installation i WHERE i.hashboard_id=NEW.asset_id AND i.device_id=NEW.device_id AND i.removed_at IS NULL)
  AND EXISTS(SELECT 1 FROM hashboard_installation i WHERE i.hashboard_id=NEW.asset_id AND i.device_id=NEW.device_id AND i.removed_at IS NOT NULL) THEN
  NEW.state := 'superseded';
  NEW.result_code := 'board_removed';
 END IF;
 RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION retire_removed_board_pool_transitions() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 -- Accepted removal, not stale telemetry, ends this physical installation.
 -- Claimed operations and physical attempts retain their recovery machinery.
 UPDATE pool_lifecycle_transition SET state='superseded',result_code='board_removed',updated_at=clock_timestamp()
 WHERE device_id=NEW.device_id AND asset_kind=7 AND asset_id=NEW.hashboard_id
  AND state IN ('pending','ready') AND operation_id IS NULL AND physical_attempt_id IS NULL;
 RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION resume_reinstalled_board_pool_transition() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE organization bigint;
BEGIN
 SELECT org_id INTO organization FROM device WHERE id=NEW.device_id AND deleted_at IS NULL;
 IF NOT FOUND THEN RETURN NEW; END IF;
 -- Consume retired, undispatched intent once. A returning board gets a new
 -- policy for its current owner; an archived or changed default is not revived.
 UPDATE pool_lifecycle_transition SET result_code='board_reinstalled',updated_at=clock_timestamp()
 WHERE organization_id=organization AND asset_kind=7 AND asset_id=NEW.hashboard_id
  AND state='superseded' AND result_code='board_removed';
 IF NOT FOUND THEN RETURN NEW; END IF;
 INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,owner_account_id,allocation_revision,reason,configuration_id,revision_id)
 SELECT organization,NEW.device_id,7,NEW.hashboard_id,COALESCE(a.subclient_id,a.client_id,0),COALESCE(a.revision,0),'ownership',c.configuration_id,c.current_revision_id
 FROM (SELECT 1) seed LEFT JOIN hosted_account_asset a ON a.organization_id=organization AND a.kind=7 AND a.asset_id=NEW.hashboard_id
 LEFT JOIN pool_configuration c ON c.organization_id=organization
  AND COALESCE(c.account_id,0)=COALESCE(a.subclient_id,a.client_id,0) AND c.is_default AND NOT c.archived
 -- A controller move may already have captured the same current ownership
 -- policy when its observation updated placement, before installation insert.
 WHERE NOT EXISTS(SELECT 1 FROM pool_lifecycle_transition l
  WHERE l.organization_id=organization AND l.device_id=NEW.device_id AND l.asset_kind=7 AND l.asset_id=NEW.hashboard_id
   AND l.owner_account_id=COALESCE(a.subclient_id,a.client_id,0) AND l.allocation_revision=COALESCE(a.revision,0)
   AND l.state IN ('pending','ready','applying','uncertain'));
 RETURN NEW;
END $$;
