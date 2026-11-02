DROP TRIGGER hosted_allocation_pool_intent ON hosted_account_asset;
DROP FUNCTION retire_hosted_allocation_pool_intent();
DROP FUNCTION retire_reallocated_pool_assignments(bigint,bigint,integer,bigint);
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
SELECT 4::smallint,b.id,b.org_id,d.site_id,m.id,6,m.id,i.id,d.id,i.position,
 ('Hashboard '||b.id||' / Module '||m.id||' / slot '||i.position)::text,m.model,''::text,NULL::bigint,NULL::bigint
FROM hashboard b JOIN hashboard_installation i ON i.hashboard_id=b.id AND i.removed_at IS NULL
JOIN container_module m ON m.id=i.module_id JOIN container_tank t ON t.id=m.tank_id JOIN device d ON d.id=t.device_id AND d.org_id=b.org_id
WHERE m.retired_at IS NULL AND d.deleted_at IS NULL;
