-- name: ListPoolDeviceImportSources :many
-- Only unmanaged paired devices. A tombstone is deliberately not eligible for
-- automatic adoption when hardware is recalled or returns to a previous owner.
WITH ownership_sources AS (
 SELECT d.id AS device_id,d.id AS asset_id,COALESCE(a.subclient_id,a.client_id,0)::bigint AS owner_id,COALESCE(a.revision,0)::bigint AS revision
 FROM device d JOIN discovered_device dd ON dd.id=d.discovered_device_id
 LEFT JOIN hosted_account_asset a ON a.organization_id=d.org_id AND a.kind=5 AND a.asset_id=d.id
 WHERE d.org_id=$1 AND dd.device_class=1
 UNION ALL
 SELECT d.id,COALESCE(b.id,0)::bigint,
 CASE WHEN membership.module_id IS NULL THEN -1 ELSE COALESCE(a.subclient_id,a.client_id,0) END::bigint,
 COALESCE(a.revision,0)::bigint
 FROM device d JOIN discovered_device dd ON dd.id=d.discovered_device_id
 JOIN container_tank tank ON tank.device_id=d.id JOIN container_module m ON m.tank_id=tank.id
 LEFT JOIN hosted_module_membership membership ON membership.module_id=m.id AND membership.org_id=d.org_id
 LEFT JOIN LATERAL unnest(membership.board_ids) b(id) ON true
 LEFT JOIN hosted_account_asset a ON a.organization_id=d.org_id AND a.kind=7 AND a.asset_id=b.id
 WHERE d.org_id=$1 AND dd.device_class=2 AND tank.present AND m.retired_at IS NULL
), owners AS (
 SELECT device_id,min(owner_id)::bigint AS account_id,count(DISTINCT owner_id)::bigint AS owner_count,
 jsonb_agg(jsonb_build_array(asset_id,owner_id,revision) ORDER BY asset_id) AS bindings
 FROM ownership_sources GROUP BY device_id
)
SELECT d.id, d.device_identifier, d.site_id, d.discovered_device_id,
 COALESCE(d.worker_name,'')::text AS worker_name, dd.device_class,
 COALESCE(f.epoch,0)::bigint AS epoch, COALESCE(f.state,'host')::text AS state, device_write_identity_keys(d.id)::text[] AS identity_keys,
 COALESCE(owners.account_id,0)::bigint AS account_id,
 COALESCE(owners.owner_count,0)::bigint AS owner_count,
 COALESCE(owners.bindings,'[]')::jsonb AS ownership,
 EXISTS(SELECT 1 FROM fleet_node_device n WHERE n.org_id=d.org_id AND n.device_id=d.id) AS remote,
 dd.updated_at AS discovery_updated_at
FROM device d JOIN discovered_device dd ON dd.id=d.discovered_device_id AND dd.org_id=d.org_id
JOIN device_pairing pairing ON pairing.device_id=d.id AND pairing.pairing_status IN ('PAIRED','DEFAULT_PASSWORD')
LEFT JOIN device_write_fence f ON f.organization_id=d.org_id AND f.device_id=d.id
LEFT JOIN owners ON owners.device_id=d.id
WHERE d.org_id=$1 AND d.deleted_at IS NULL AND dd.deleted_at IS NULL AND dd.device_class IN (1,2)
 AND NOT EXISTS(SELECT 1 FROM pool_configuration_assignment a WHERE a.organization_id=d.org_id AND a.device_id=d.id)
 AND NOT EXISTS(SELECT 1 FROM hashboard_pool_intent intent WHERE intent.organization_id=d.org_id AND intent.device_id=d.id)
ORDER BY d.id;
