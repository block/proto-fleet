-- requires-shared: 1000
CREATE FUNCTION ct1_hashboard_allocation_ready(target_org bigint, controller bigint)
RETURNS boolean LANGUAGE sql VOLATILE AS $$
 SELECT EXISTS (
  SELECT 1 FROM container_mining_group_state s
  JOIN device_write_fence f ON f.device_id=s.device_id AND f.organization_id=s.organization_id
  WHERE s.organization_id=target_org AND s.device_id=controller
   AND f.state='host' AND s.status NOT IN ('pending','applying','blocked')
   AND s.observed_at>=f.updated_at
   AND s.observed_at>clock_timestamp()-interval '2 minutes' AND s.observed_at<=clock_timestamp()+interval '5 seconds'
   AND COALESCE((s.observed->>'revision')::bigint,0)>0
   AND s.observed @> '{"supported":true,"enabled":true,"suspended":false,"pending":false}'::jsonb
   AND NOT EXISTS (SELECT 1 FROM device_write_attempt a WHERE a.device_id=controller
    AND a.organization_id=target_org AND a.operation_kind='mutation' AND a.status IN ('running','unknown'))
 );
$$;

ALTER TABLE container_mining_group_state DROP CONSTRAINT container_mining_group_state_operation_kind_check;
ALTER TABLE container_mining_group_state ADD CONSTRAINT chk_container_mining_group_state_operation_kind
 CHECK (operation_kind IN ('','activate','deactivate','pools'));

ALTER TABLE pool_lifecycle_transition DROP CONSTRAINT pool_lifecycle_transition_state_check;
ALTER TABLE pool_lifecycle_transition ADD CONSTRAINT chk_pool_lifecycle_transition_state
 CHECK (state IN ('pending','deferred','ready','applying','verified','completed','failed','uncertain','superseded'));

-- Extend the parent's open-work index for deferred trigger and reconciliation
-- lookups. Retained terminal history stays outside the indexed working set.
DROP INDEX idx_pool_lifecycle_transition_open_asset;
CREATE INDEX idx_pool_lifecycle_transition_open_asset
 ON pool_lifecycle_transition(state,organization_id,asset_kind,asset_id)
 WHERE state IN ('pending','deferred','ready','applying','uncertain','failed');

-- Allocation guards also inspect uncertain work. Keep those rows in the
-- assignment-scoped working set instead of scanning retained target history.
DROP INDEX idx_pool_configuration_operation_target_open;
CREATE INDEX idx_pool_configuration_operation_target_open
 ON pool_configuration_operation_target(organization_id,target_kind,target_id)
 WHERE state IN ('pending','applying','uncertain','failed','different','applied_unverified');

-- All workflow guards and the lifecycle worker use the same safe retry rule.
CREATE FUNCTION pool_lifecycle_clear_retryable(l pool_lifecycle_transition,w device_write_attempt)
RETURNS boolean LANGUAGE sql IMMUTABLE AS $$
 SELECT l.state='failed' AND l.result_code='clear_not_dispatched' AND l.reason='ownership'
  AND l.configuration_id IS NULL AND l.target_kind='device' AND w.status='complete'
  AND w.operation_kind='mutation' AND w.command_type='UpdateMiningPools' AND w.write_outcome='{}'::jsonb;
$$;

CREATE INDEX idx_hosted_account_asset_allocated_hashboard_device
 ON hosted_account_asset(organization_id,device_id)
 WHERE kind=7 AND client_id IS NOT NULL AND device_id IS NOT NULL;

LOCK TABLE organization IN SHARE ROW EXCLUSIVE MODE;
INSERT INTO permission(key,description) VALUES ('miner:deactivate_mining_groups','Disable per-hashboard pool configuration on an unallocated container.') ON CONFLICT(key) DO NOTHING;
INSERT INTO role_permission(role_id,permission_id)
SELECT r.id,p.id FROM role r CROSS JOIN permission p
WHERE r.builtin_key='ADMIN' AND r.deleted_at IS NULL AND p.key='miner:deactivate_mining_groups'
ON CONFLICT(role_id,permission_id) DO NOTHING;

-- Retain automatic policy without rejecting its inventory or pairing change.
-- Deferred work waits for the admitted deactivation without blocking it.
CREATE FUNCTION guard_pool_lifecycle_mining_group_deactivation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 -- Rigs cannot run mining-group deactivation. Keep their ownership and pairing
 -- writes independent of physical-attempt finalization on the device fence.
 IF NEW.state<>'pending' OR NOT EXISTS (
  SELECT 1 FROM device d JOIN discovered_device dd ON dd.id=d.discovered_device_id
  WHERE d.id=NEW.device_id AND d.org_id=NEW.organization_id AND dd.device_class=2
 ) THEN
  RETURN NEW;
 END IF;
 PERFORM 1 FROM device_write_fence WHERE device_id=NEW.device_id AND organization_id=NEW.organization_id FOR UPDATE;
 IF NEW.state='pending' AND EXISTS (SELECT 1 FROM container_mining_group_state WHERE device_id=NEW.device_id
  AND organization_id=NEW.organization_id AND operation_kind='deactivate' AND status IN ('pending','applying','blocked')) THEN
  NEW.state := 'deferred';
  NEW.result_code := 'mining_groups_deactivation';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pool_lifecycle_mining_group_deactivation BEFORE INSERT ON pool_lifecycle_transition
 FOR EACH ROW EXECUTE FUNCTION guard_pool_lifecycle_mining_group_deactivation();

-- Deferred policies have no physical admission. Retire and deduplicate them
-- with pending policy when ownership, pairing or physical installation changes.
CREATE OR REPLACE FUNCTION enqueue_ownership_pool_transition() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE owner bigint;
BEGIN
 IF TG_OP='UPDATE' AND NEW.client_id IS NOT DISTINCT FROM OLD.client_id
  AND NEW.subclient_id IS NOT DISTINCT FROM OLD.subclient_id AND NEW.device_id=OLD.device_id THEN RETURN NEW; END IF;
 IF TG_OP='INSERT' AND NEW.client_id IS NULL THEN RETURN NEW; END IF;
 owner := COALESCE(NEW.subclient_id,NEW.client_id,0);
 UPDATE pool_lifecycle_transition SET state='superseded',updated_at=clock_timestamp()
 WHERE organization_id=NEW.organization_id AND ((asset_kind=NEW.kind AND asset_id=NEW.asset_id) OR (asset_kind=0 AND NEW.kind=5 AND device_id=NEW.device_id))
  AND state IN ('pending','deferred','ready','failed');
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
   AND reason='pairing' AND state IN ('pending','deferred','ready')
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
 IF NEW.asset_kind=7 AND NEW.state IN ('pending','deferred') AND NEW.operation_id IS NULL AND NEW.physical_attempt_id IS NULL
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
  AND state IN ('pending','deferred','ready') AND operation_id IS NULL AND physical_attempt_id IS NULL;
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
   AND l.state IN ('pending','deferred','ready','applying','uncertain'));
 RETURN NEW;
END $$;
