-- name: GetPoolEquipmentDevice :one
SELECT d.id,d.device_identifier,d.site_id,dd.device_class
FROM device d JOIN discovered_device dd ON dd.id=d.discovered_device_id AND dd.org_id=d.org_id
WHERE d.org_id=$1 AND d.id=$2 AND d.deleted_at IS NULL AND dd.deleted_at IS NULL
FOR SHARE OF d,dd;

-- name: GetPoolEquipmentMinerOwner :one
SELECT d.id AS asset_id,COALESCE(a.subclient_id,a.client_id,0)::bigint AS account_id,
 COALESCE(a.revision,0)::bigint AS revision
FROM device d LEFT JOIN hosted_account_asset a ON a.organization_id=d.org_id AND a.kind=5 AND a.asset_id=d.id
WHERE d.org_id=$1 AND d.id=$2;

-- name: ListPoolEquipmentModuleOwners :many
SELECT m.id AS asset_id,COALESCE(a.subclient_id,a.client_id,0)::bigint AS account_id,
 COALESCE(a.revision,0)::bigint AS revision
FROM container_module m JOIN container_tank t ON t.id=m.tank_id
JOIN device d ON d.id=t.device_id
LEFT JOIN hosted_account_asset a ON a.organization_id=d.org_id AND a.kind=6 AND a.asset_id=m.id
WHERE d.org_id=$1 AND d.id=$2 AND t.present AND m.retired_at IS NULL
ORDER BY m.id;

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

-- name: RetireReallocatedPoolAssignments :exec
-- Allocation changes end the previous owner's intent; they never apply a new
-- owner's settings. Keep assignment generations and immutable operation history.
WITH retired AS (
 UPDATE pool_configuration_assignment a SET retired_at=CURRENT_TIMESTAMP,
  version=a.version+1,updated_at=CURRENT_TIMESTAMP
 WHERE a.organization_id=sqlc.arg(organization_id) AND a.retired_at IS NULL
  AND a.device_id=sqlc.arg(device_id)
  AND (a.target_kind='device' OR (
   sqlc.arg(asset_kind)::int=6 AND a.target_kind='hashboard_installation' AND EXISTS (
    SELECT 1 FROM hashboard_installation i WHERE i.id=a.target_id AND i.module_id=sqlc.arg(asset_id)
   )
  ))
 RETURNING a.organization_id,a.target_kind,a.target_id
)
UPDATE pool_configuration_operation_target t SET state='superseded',updated_at=CURRENT_TIMESTAMP
FROM retired a WHERE t.organization_id=a.organization_id AND t.target_kind=a.target_kind AND t.target_id=a.target_id
 AND t.state IN ('pending','applying','failed','different','applied_unverified');
