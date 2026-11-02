-- requires-shared: 1000
CREATE OR REPLACE VIEW repair_ticket_asset_option AS
SELECT 1::smallint AS kind,d.id,d.org_id,COALESCE(d.site_id,dsr.site_id) AS site_id,d.id AS parent_asset_id,5::int AS allocation_kind,
 NULL::bigint AS module_id,NULL::bigint AS installation_id,NULL::bigint AS container_id,NULL::int AS position,
 COALESCE(NULLIF(d.custom_name,''),d.device_identifier)::text AS label,COALESCE(dd.model,'')::text AS model,
 d.device_identifier::text AS miner_identifier,COALESCE(d.building_id,dsr.building_id) AS building_id,rack.id AS rack_id
FROM device d JOIN discovered_device dd ON dd.id=d.discovered_device_id AND dd.org_id=d.org_id
LEFT JOIN device_set_membership rm ON rm.device_id=d.id AND rm.org_id=d.org_id AND rm.device_set_type='rack'
LEFT JOIN device_set rack ON rack.id=rm.device_set_id AND rack.org_id=d.org_id AND rack.deleted_at IS NULL
LEFT JOIN device_set_rack dsr ON dsr.device_set_id=rack.id AND dsr.org_id=d.org_id
WHERE d.deleted_at IS NULL AND dd.deleted_at IS NULL AND dd.device_class<>2
UNION ALL
SELECT 3::smallint,m.id,d.org_id,d.site_id,m.id,6,m.id,NULL::bigint,d.id,m.position,
 ('Module '||m.id||CASE WHEN m.reported_id<>'' THEN ' / '||m.reported_id ELSE '' END)::text,m.model,''::text,NULL::bigint,NULL::bigint
FROM container_module m JOIN container_tank t ON t.id=m.tank_id JOIN device d ON d.id=t.device_id
WHERE m.retired_at IS NULL AND d.deleted_at IS NULL
UNION ALL
SELECT 4::smallint,b.id,b.org_id,d.site_id,b.id,7,m.id,i.id,d.id,i.position,
 ('Hashboard '||b.id||' / Module '||m.id||' / slot '||i.position)::text,m.model,''::text,NULL::bigint,NULL::bigint
FROM hashboard b JOIN hashboard_installation i ON i.hashboard_id=b.id AND i.removed_at IS NULL
JOIN container_module m ON m.id=i.module_id JOIN container_tank t ON t.id=m.tank_id JOIN device d ON d.id=t.device_id AND d.org_id=b.org_id
WHERE m.retired_at IS NULL AND d.deleted_at IS NULL;


-- Every ownership path, including automatic replacement, retires prior intent.
CREATE FUNCTION retire_reallocated_pool_assignments(organization bigint, controller bigint, asset_kind integer, asset bigint)
RETURNS void LANGUAGE SQL AS $$
 WITH retired AS (
  UPDATE pool_configuration_assignment a SET retired_at=CURRENT_TIMESTAMP,
   version=a.version+1,updated_at=CURRENT_TIMESTAMP
  WHERE a.organization_id=organization AND a.retired_at IS NULL
   AND ((a.target_kind='device' AND a.device_id=controller) OR
    (a.target_kind='hashboard_installation' AND EXISTS (
     SELECT 1 FROM hashboard_installation i WHERE i.id=a.target_id
      AND ((asset_kind=6 AND i.module_id=asset) OR (asset_kind=7 AND i.hashboard_id=asset)))))
  RETURNING a.organization_id,a.target_kind,a.target_id
 )
 UPDATE pool_configuration_operation_target t SET state='superseded',updated_at=CURRENT_TIMESTAMP
 FROM retired a WHERE t.organization_id=a.organization_id AND t.target_kind=a.target_kind AND t.target_id=a.target_id
  AND t.state IN ('pending','applying','failed','different','applied_unverified');
$$;
CREATE FUNCTION retire_hosted_allocation_pool_intent() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' THEN
  IF NEW.client_id IS NOT DISTINCT FROM OLD.client_id AND NEW.subclient_id IS NOT DISTINCT FROM OLD.subclient_id
   AND NEW.device_id=OLD.device_id THEN RETURN NEW; END IF;
  IF NEW.device_id<>OLD.device_id THEN
   PERFORM retire_reallocated_pool_assignments(OLD.organization_id,OLD.device_id,OLD.kind,OLD.asset_id);
  END IF;
 ELSIF NEW.client_id IS NULL THEN RETURN NEW;
 END IF;
 PERFORM retire_reallocated_pool_assignments(NEW.organization_id,NEW.device_id,NEW.kind,NEW.asset_id);
 RETURN NEW;
END $$;
CREATE TRIGGER hosted_allocation_pool_intent AFTER INSERT OR UPDATE ON hosted_account_asset
 FOR EACH ROW EXECUTE FUNCTION retire_hosted_allocation_pool_intent();
