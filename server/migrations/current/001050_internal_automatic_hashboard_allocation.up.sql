-- requires-shared: 1000
CREATE INDEX idx_hashboard_installation_slot_history
 ON hashboard_installation(module_id,position,installed_at DESC,id DESC);

-- Called after the complete controller map has been reconciled. The shared
-- organization lock serializes this with manual allocation and other polls.
CREATE FUNCTION reconcile_hosted_hashboard_replacements(target_org bigint, controller bigint, observed timestamptz)
RETURNS bigint LANGUAGE plpgsql AS $$
DECLARE
 replacement record;
 previous_boundary text := COALESCE(current_setting('fleet.allocation_boundary',true),'');
 transferred bigint := 0;
BEGIN
 PERFORM id FROM organization WHERE id=target_org AND deleted_at IS NULL FOR NO KEY UPDATE;
 IF NOT FOUND THEN RETURN 0; END IF;

 FOR replacement IN
  SELECT a.asset_id AS old_board, current.hashboard_id AS new_board, current.module_id, current.position,
   a.client_id,a.subclient_id,root.account_id AS root_uuid,client.account_id AS client_uuid,
   COALESCE(child.account_id,client.account_id) AS owner_uuid
  FROM hashboard_installation current
  JOIN container_module module ON module.id=current.module_id AND module.retired_at IS NULL
   AND module.identity_confirmed AND module.last_seen_at=observed
  JOIN container_tank tank ON tank.id=module.tank_id AND tank.device_id=controller AND tank.present
  JOIN container_inventory inventory ON inventory.device_id=controller AND inventory.observed_at=observed
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
   AND period.subclient_id IS NOT DISTINCT FROM a.subclient_id AND period.started_at<=prior.removed_at
  JOIN hashboard_observation outgoing ON outgoing.hashboard_id=prior.hashboard_id
   AND outgoing.released_at IS NOT NULL
  JOIN hosted_account client ON client.id=a.client_id
  JOIN hosted_account root ON root.organization_id=target_org AND root.depth=0
  LEFT JOIN hosted_account child ON child.id=a.subclient_id
  LEFT JOIN hosted_account_asset destination ON destination.organization_id=target_org AND destination.kind=7
   AND destination.asset_id=current.hashboard_id
  WHERE current.device_id=controller AND current.removed_at IS NULL AND current.last_seen_at=observed
   AND prior.hashboard_id<>current.hashboard_id AND prior.removed_at<=current.installed_at
   -- Never take an allocated board, or override a manual decision made after
   -- the incoming board was installed. Recalled spare boards remain usable.
   AND destination.client_id IS NULL
   AND (destination.asset_id IS NULL OR destination.updated_at<=current.installed_at)
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
