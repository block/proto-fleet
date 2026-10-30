-- requires-shared: 1000
-- Allocation writes and inventory take the organization row lock first. Fence
-- those writers before adding ordering markers to their shared tables.
LOCK TABLE organization IN EXCLUSIVE MODE;

CREATE SEQUENCE hosted_hashboard_allocation_order;
ALTER TABLE hosted_account_asset ADD COLUMN allocation_decision_order bigint NOT NULL DEFAULT 0;
ALTER TABLE hashboard_installation ADD COLUMN installed_order bigint, ADD COLUMN removed_order bigint;
ALTER TABLE container_inventory ADD COLUMN hashboard_inventory_order bigint,
 ADD COLUMN hashboard_inventory_complete boolean NOT NULL DEFAULT false;

-- Accepted inventory may be slightly ahead of the server clock. A controller
-- move or deletion must close all of its placements at one valid boundary.
CREATE OR REPLACE FUNCTION record_hosted_account_asset_placement() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE boundary TIMESTAMPTZ;
BEGIN
 IF NEW.deleted_at IS NULL AND NEW.site_id IS NOT DISTINCT FROM OLD.site_id AND NEW.device_identifier=OLD.device_identifier THEN RETURN NEW; END IF;
 SELECT GREATEST(clock_timestamp(),max(started_at)) INTO boundary
 FROM hosted_account_asset_interval WHERE device_id=NEW.id AND ended_at IS NULL;
 IF NEW.deleted_at IS NOT NULL THEN
  UPDATE hosted_account_asset_interval SET ended_at=boundary WHERE device_id=NEW.id AND ended_at IS NULL;
  RETURN NEW;
 END IF;
 WITH closed AS (
  UPDATE hosted_account_asset_interval SET ended_at=boundary WHERE device_id=NEW.id AND ended_at IS NULL RETURNING *
 )
 INSERT INTO hosted_account_asset_interval(organization_id,kind,asset_id,device_id,client_id,subclient_id,started_at,site_id,device_identifier)
 SELECT organization_id,kind,asset_id,device_id,client_id,subclient_id,boundary,NEW.site_id,NEW.device_identifier FROM closed;
 RETURN NEW;
END;
$$;

-- Ownership writes include explicit decisions to keep the same owner. Physical
-- placement updates do not target these columns and must not advance this marker.
CREATE FUNCTION record_hosted_hashboard_allocation_order() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 NEW.allocation_decision_order := nextval('hosted_hashboard_allocation_order');
 RETURN NEW;
END;
$$;
CREATE TRIGGER hosted_hashboard_allocation_order BEFORE INSERT OR UPDATE OF client_id,subclient_id ON hosted_account_asset
 FOR EACH ROW WHEN (NEW.kind=7) EXECUTE FUNCTION record_hosted_hashboard_allocation_order();

-- These markers order accepted database operations, independent of device or
-- server clocks. Existing installations keep NULL markers: past ordering cannot
-- be inferred safely. Pending pre-upgrade replacements use the manual fallback.
CREATE FUNCTION record_hashboard_installation_order() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  NEW.installed_order := nextval('hosted_hashboard_allocation_order');
  IF NEW.removed_at IS NOT NULL THEN
   NEW.removed_order := nextval('hosted_hashboard_allocation_order');
  END IF;
 ELSIF OLD.removed_at IS NULL AND NEW.removed_at IS NOT NULL THEN
  NEW.removed_order := nextval('hosted_hashboard_allocation_order');
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER hashboard_installation_order BEFORE INSERT OR UPDATE OF removed_at ON hashboard_installation
 FOR EACH ROW EXECUTE FUNCTION record_hashboard_installation_order();

CREATE OR REPLACE FUNCTION reconcile_hosted_hashboard_replacements(target_org bigint, controller bigint, observed timestamptz)
RETURNS bigint LANGUAGE plpgsql AS $$
DECLARE
 replacement record;
 previous_boundary text := COALESCE(current_setting('fleet.allocation_boundary',true),'');
 transferred bigint := 0;
 peer_inventory_order bigint;
 peer_observed_at timestamptz;
BEGIN
 PERFORM id FROM organization WHERE id=target_org AND deleted_at IS NULL FOR NO KEY UPDATE;
 IF NOT FOUND THEN RETURN 0; END IF;

 -- Absence on this controller cannot distinguish a replacement from a move
 -- whose destination has not reported yet. Require every other registered
 -- controller to accept a complete map after the candidate was installed.
 -- Read this once per poll, not once per replacement board.
 SELECT min(CASE WHEN inventory.hashboard_inventory_complete
   THEN COALESCE(inventory.hashboard_inventory_order,0) ELSE 0 END),min(inventory.observed_at)
 INTO peer_inventory_order,peer_observed_at
 FROM device peer JOIN discovered_device discovered ON discovered.id=peer.discovered_device_id
 LEFT JOIN container_inventory inventory ON inventory.device_id=peer.id
 WHERE peer.org_id=target_org AND peer.id<>controller AND peer.deleted_at IS NULL
  AND discovered.deleted_at IS NULL AND discovered.device_class=2;

 FOR replacement IN
  SELECT a.asset_id AS old_board, current.hashboard_id AS new_board, current.module_id, current.position,
   a.client_id,a.subclient_id,root.account_id AS root_uuid,client.account_id AS client_uuid,
   COALESCE(child.account_id,client.account_id) AS owner_uuid
  FROM hashboard_installation current
  JOIN container_module module ON module.id=current.module_id AND module.retired_at IS NULL
   AND module.identity_confirmed AND module.last_seen_at=observed
  JOIN container_tank tank ON tank.id=module.tank_id AND tank.device_id=controller AND tank.present
  JOIN container_inventory inventory ON inventory.device_id=controller AND inventory.observed_at=observed
   AND inventory.hashboard_inventory_complete
  JOIN device device ON device.id=controller AND device.org_id=target_org AND device.deleted_at IS NULL
   AND device.site_id IS NOT DISTINCT FROM current.site_id
  JOIN container_module_identity identity ON identity.module_id=module.id
   AND identity.org_id=target_org AND identity.reported_module_id=module.reported_id
  JOIN hashboard_slot slot ON slot.module_id=current.module_id AND slot.position=current.position
   AND slot.hashboard_id=current.hashboard_id AND slot.occupancy='occupied'
   AND slot.unavailable_reason='' AND slot.observed_at=observed
  JOIN hashboard_observation incoming ON incoming.hashboard_id=current.hashboard_id
   AND incoming.device_id=controller AND incoming.site_id IS NOT DISTINCT FROM current.site_id
   AND incoming.released_at IS NULL AND incoming.last_seen_at=observed
  CROSS JOIN LATERAL (
   SELECT p.* FROM hashboard_installation p
   WHERE p.module_id=current.module_id AND p.position=current.position
    AND (p.installed_at,p.id)<(current.installed_at,current.id)
   ORDER BY p.installed_at DESC,p.id DESC LIMIT 1
  ) prior
  JOIN hosted_account_asset a ON a.organization_id=target_org AND a.kind=7
   AND a.asset_id=prior.hashboard_id AND a.client_id IS NOT NULL AND a.device_id=prior.device_id
  JOIN hosted_account_asset_interval period ON period.organization_id=a.organization_id AND period.kind=7
   AND period.asset_id=a.asset_id AND period.ended_at IS NULL AND period.client_id=a.client_id
   AND period.subclient_id IS NOT DISTINCT FROM a.subclient_id AND period.started_at<=observed
   AND a.allocation_decision_order<=prior.removed_order
  JOIN hashboard_observation outgoing ON outgoing.hashboard_id=prior.hashboard_id
   AND outgoing.released_at IS NOT NULL
  JOIN hosted_account client ON client.id=a.client_id
  JOIN hosted_account root ON root.organization_id=target_org AND root.depth=0
  LEFT JOIN hosted_account child ON child.id=a.subclient_id
  LEFT JOIN hosted_account_asset destination ON destination.organization_id=target_org AND destination.kind=7
   AND destination.asset_id=current.hashboard_id
  WHERE current.device_id=controller AND current.removed_at IS NULL AND current.last_seen_at=observed
   AND prior.hashboard_id<>current.hashboard_id AND prior.removed_at<=current.installed_at
   AND (peer_inventory_order IS NULL OR (peer_inventory_order>GREATEST(current.installed_order,prior.removed_order)
    AND peer_observed_at>=current.installed_at))
   -- Never take an allocated board, or override a manual decision made after
   -- the incoming board was installed. Recalled spare boards remain usable.
   AND destination.client_id IS NULL
   AND COALESCE(destination.allocation_decision_order,0)<=current.installed_order
   -- A late report can precede the spare's previous ownership period. Wait for
   -- a later accepted poll instead of backdating or overlapping that history.
   AND NOT EXISTS (
    SELECT 1 FROM hosted_account_asset_interval history
    WHERE history.organization_id=target_org AND history.kind=7 AND history.asset_id=current.hashboard_id
     AND COALESCE(history.ended_at,history.started_at)>observed
   )
   -- The outgoing board must have disappeared from its last known installation,
   -- not moved to another slot or appeared elsewhere in this same controller map.
   AND NOT EXISTS (SELECT 1 FROM hashboard_installation later WHERE later.hashboard_id=prior.hashboard_id
    AND (later.removed_at IS NULL OR (later.installed_at,later.id)>(prior.installed_at,prior.id)))
   AND NOT EXISTS (
    SELECT 1 FROM hosted_account policy
    WHERE policy.organization_id=target_org AND policy.id IN (root.id,a.client_id,a.subclient_id)
     AND (policy.suspended
      OR (policy.sites_restricted AND NOT EXISTS (SELECT 1 FROM hosted_account_site allowed
       WHERE allowed.account_id=policy.id AND NOT allowed.parent_controlled AND allowed.site_id=current.site_id))
      OR (policy.site_ceiling_restricted AND NOT EXISTS (SELECT 1 FROM hosted_account_site allowed
       WHERE allowed.account_id=policy.id AND allowed.parent_controlled AND allowed.site_id=current.site_id)))
   )
 LOOP
  -- Both ownership periods meet at the accepted observation. A delayed
  -- confirmation starts ownership now; it never backfills earlier telemetry.
  PERFORM set_config('fleet.allocation_boundary',observed::text,true);
  UPDATE hosted_account_asset SET client_id=NULL,subclient_id=NULL,revision=revision+1,updated_at=clock_timestamp()
   WHERE organization_id=target_org AND kind=7 AND asset_id=replacement.old_board;
  INSERT INTO hosted_account_asset(organization_id,kind,asset_id,hashboard_id,device_id,client_id,subclient_id)
   VALUES(target_org,7,replacement.new_board,replacement.new_board,controller,replacement.client_id,replacement.subclient_id)
   ON CONFLICT(organization_id,kind,asset_id) DO UPDATE SET device_id=EXCLUDED.device_id,
    client_id=EXCLUDED.client_id,subclient_id=EXCLUDED.subclient_id,revision=hosted_account_asset.revision+1,updated_at=clock_timestamp();
  INSERT INTO activity_log(event_id,event_category,event_type,description,result,actor_type,organization_id,scope_type,scope_count,metadata)
   VALUES(gen_random_uuid(),'fleet_management','account_hashboard_replaced','Automatically replaced allocated hashboard','success','system',target_org,'account',1,
    jsonb_build_object('account_id',replacement.root_uuid,'child_account_id',replacement.client_uuid,
     'allocated_account_id',replacement.owner_uuid,'asset_count',1,'automatic',true,
     'old_hashboard_id',replacement.old_board,'replacement_hashboard_id',replacement.new_board,
     'module_id',replacement.module_id,'position',replacement.position,'observed_at',observed));
  transferred := transferred+1;
 END LOOP;
 PERFORM set_config('fleet.allocation_boundary',previous_boundary,true);
 RETURN transferred;
END;
$$;
