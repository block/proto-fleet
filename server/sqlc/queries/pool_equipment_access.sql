-- name: GetPoolEquipmentDevice :one
SELECT d.id,d.device_identifier,d.site_id,dd.device_class
FROM device d JOIN discovered_device dd ON dd.id=d.discovered_device_id AND dd.org_id=d.org_id
WHERE d.org_id=$1 AND d.id=$2 AND d.deleted_at IS NULL AND dd.deleted_at IS NULL
FOR SHARE OF d,dd;

-- name: GetPoolEquipmentMinerOwner :one
SELECT d.id AS asset_id,0::bigint AS hashboard_id,COALESCE(a.subclient_id,a.client_id,0)::bigint AS account_id,
 COALESCE(a.revision,0)::bigint AS revision
FROM device d LEFT JOIN hosted_account_asset a ON a.organization_id=d.org_id AND a.kind=5 AND a.asset_id=d.id
WHERE d.org_id=$1 AND d.id=$2;

-- name: ListPoolEquipmentModuleOwners :many
SELECT m.id AS asset_id,COALESCE(b.id,0)::bigint AS hashboard_id,
 CASE WHEN membership.module_id IS NULL THEN -1 ELSE COALESCE(a.subclient_id,a.client_id,0) END::bigint AS account_id,
 COALESCE(a.revision,0)::bigint AS revision
FROM container_module m JOIN container_tank t ON t.id=m.tank_id
JOIN device d ON d.id=t.device_id
LEFT JOIN hosted_module_membership membership ON membership.module_id=m.id AND membership.org_id=d.org_id
LEFT JOIN LATERAL unnest(membership.board_ids) b(id) ON true
LEFT JOIN hosted_account_asset a ON a.organization_id=d.org_id AND a.kind=7 AND a.asset_id=b.id
WHERE d.org_id=$1 AND d.id=$2 AND t.present AND m.retired_at IS NULL
ORDER BY m.id,b.id;

-- name: GetPoolEquipmentInstallationModule :one
SELECT i.module_id FROM hashboard_installation i
JOIN hashboard h ON h.id=i.hashboard_id
JOIN container_module m ON m.id=i.module_id JOIN container_tank t ON t.id=m.tank_id
JOIN device d ON d.id=t.device_id
JOIN hashboard_slot s ON s.module_id=i.module_id AND s.position=i.position AND s.hashboard_id=i.hashboard_id
WHERE h.org_id=$1 AND d.org_id=$1 AND i.id=$2 AND d.id=$3 AND i.device_id=$3
 AND i.removed_at IS NULL AND m.retired_at IS NULL AND t.present AND d.deleted_at IS NULL
 AND s.occupancy='occupied' AND s.unavailable_reason='' AND i.site_id IS NOT DISTINCT FROM d.site_id
FOR SHARE OF i,h,m,t,s;
