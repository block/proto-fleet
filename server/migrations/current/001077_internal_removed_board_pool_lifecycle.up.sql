-- requires-shared: 1073
-- Inventory and ownership writers already serialize on the organization row.
LOCK TABLE organization IN EXCLUSIVE MODE;

CREATE INDEX idx_pool_lifecycle_removed_board ON pool_lifecycle_transition(organization_id,asset_id)
 WHERE asset_kind=7 AND state='superseded' AND result_code='board_removed';

-- Replacement recalls the outgoing board after closing its installation.
-- Retain that policy as history without trying to configure absent hardware.
CREATE FUNCTION retire_absent_board_pool_transition() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.asset_kind=7 AND NEW.state='pending' AND NEW.operation_id IS NULL AND NEW.physical_attempt_id IS NULL
  AND NOT EXISTS(SELECT 1 FROM hashboard_installation i WHERE i.hashboard_id=NEW.asset_id AND i.device_id=NEW.device_id AND i.removed_at IS NULL)
  AND EXISTS(SELECT 1 FROM hashboard_installation i WHERE i.hashboard_id=NEW.asset_id AND i.device_id=NEW.device_id AND i.removed_at IS NOT NULL) THEN
  NEW.state := 'superseded';
  NEW.result_code := 'board_removed';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pool_lifecycle_absent_board BEFORE INSERT ON pool_lifecycle_transition
 FOR EACH ROW EXECUTE FUNCTION retire_absent_board_pool_transition();

CREATE FUNCTION retire_removed_board_pool_transitions() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 -- Accepted removal, not stale telemetry, ends this physical installation.
 -- Claimed operations and physical attempts retain their recovery machinery.
 UPDATE pool_lifecycle_transition SET state='superseded',result_code='board_removed',updated_at=clock_timestamp()
 WHERE device_id=NEW.device_id AND asset_kind=7 AND asset_id=NEW.hashboard_id
  AND state IN ('pending','ready') AND operation_id IS NULL AND physical_attempt_id IS NULL;
 RETURN NEW;
END $$;
CREATE TRIGGER hashboard_removed_pool_transitions AFTER UPDATE OF removed_at ON hashboard_installation
 FOR EACH ROW WHEN (OLD.removed_at IS NULL AND NEW.removed_at IS NOT NULL)
 EXECUTE FUNCTION retire_removed_board_pool_transitions();

CREATE FUNCTION resume_reinstalled_board_pool_transition() RETURNS trigger LANGUAGE plpgsql AS $$
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
CREATE TRIGGER hashboard_reinstalled_pool_transition AFTER INSERT ON hashboard_installation
 FOR EACH ROW WHEN (NEW.removed_at IS NULL) EXECUTE FUNCTION resume_reinstalled_board_pool_transition();

-- Retire work left by older backends before writers resume. Future removals
-- retire in the inventory transaction, so even a prompt reinstall gets new work.
UPDATE pool_lifecycle_transition l SET state='superseded',result_code='board_removed',updated_at=clock_timestamp()
WHERE l.asset_kind=7 AND l.state IN ('pending','ready') AND l.operation_id IS NULL AND l.physical_attempt_id IS NULL
 AND NOT EXISTS(SELECT 1 FROM hashboard_installation i WHERE i.hashboard_id=l.asset_id AND i.device_id=l.device_id AND i.removed_at IS NULL)
 AND EXISTS(SELECT 1 FROM hashboard_installation i WHERE i.hashboard_id=l.asset_id AND i.device_id=l.device_id AND i.removed_at IS NOT NULL);
