-- name: LockLegacyPoolLibraryForImport :exec
-- A maintenance import must also exclude new rows from the legacy library.
LOCK TABLE pool IN SHARE MODE;

-- name: ListHostedPoolImportSources :many
SELECT r.revision_id, r.number, r.name, r.slots,
 CASE WHEN a.depth = 0 THEN 0 ELSE a.id END::bigint AS owner_account_id
FROM hosted_account_pool_revision r
JOIN hosted_account_pool p ON p.pool_id = r.pool_id AND p.organization_id = r.organization_id AND p.account_id = r.account_id
JOIN hosted_account a ON a.id = r.account_id AND a.organization_id = r.organization_id
WHERE r.organization_id = $1 AND (
 (NOT p.archived AND NOT EXISTS (
    SELECT 1 FROM hosted_account_pool_revision later WHERE later.pool_id = r.pool_id AND later.number > r.number
 )) OR EXISTS (
    SELECT 1 FROM hashboard_pool_intent intent WHERE intent.organization_id = r.organization_id AND intent.pool_revision_id = r.revision_id
 ) OR EXISTS (
    SELECT 1 FROM hosted_account_pool_profile profile WHERE profile.organization_id = r.organization_id AND profile.revision_id = r.revision_id
 ))
ORDER BY r.revision_id;

-- name: LockPoolAssignmentAdoptionSources :exec
LOCK TABLE hashboard_pool_intent, container_mining_group_state, hashboard_installation,
 container_module, container_tank, container_inventory, hashboard_slot, hashboard_observation,
 hosted_account_asset, device_write_attempt, queue_message,
 device, discovered_device, device_pairing, device_write_fence, fleet_node_device IN SHARE MODE;

-- name: ListUnsettledPoolAdoptionWork :many
SELECT attempt.device_id,attempt.operation_kind,attempt.command_type
FROM device_write_attempt attempt
WHERE attempt.organization_id=$1 AND attempt.status IN ('running','unknown')
UNION ALL
-- Queued commands have no physical attempt yet and survive the offline import.
SELECT message.device_id,'mutation'::text AS operation_kind,message.command_type
FROM queue_message message JOIN device ON device.id=message.device_id
WHERE device.org_id=$1 AND message.status IN ('PENDING','PROCESSING')
UNION ALL
SELECT journal.device_id,'mutation'::text AS operation_kind,'MiningGroups'::text AS command_type
FROM container_mining_group_state journal
WHERE journal.organization_id=$1 AND journal.status IN ('pending','applying','blocked');

-- name: ListLegacyHashboardPoolAdoptions :many
SELECT intent.hashboard_id,intent.installation_id,intent.device_id,intent.operation_id,
 intent.pool_revision_id,revision.number AS source_version,
 CASE WHEN owner.depth=0 THEN 0 ELSE owner.id END::bigint AS configuration_owner,
 COALESCE(asset.subclient_id,asset.client_id,0)::bigint AS equipment_owner,
 COALESCE(installation.removed_at IS NULL AND module.retired_at IS NULL AND tank.present
  AND device.deleted_at IS NULL AND installation.hashboard_id=intent.hashboard_id
  AND tank.device_id=intent.device_id AND installation.device_id=intent.device_id
  AND installation.site_id IS NOT DISTINCT FROM device.site_id
  AND intent.mac_address<>'' AND installation.metadata->>'macAddress'=intent.mac_address
  AND slot.hashboard_id=intent.hashboard_id AND slot.occupancy='occupied' AND slot.unavailable_reason='',false)::boolean AS physical_match
FROM hashboard_pool_intent intent
JOIN hosted_account_pool_revision revision ON revision.organization_id=intent.organization_id AND revision.revision_id=intent.pool_revision_id AND revision.account_id=intent.account_id
JOIN hosted_account owner ON owner.organization_id=intent.organization_id AND owner.id=intent.account_id
LEFT JOIN hashboard_installation installation ON installation.id=intent.installation_id
LEFT JOIN container_module module ON module.id=installation.module_id
LEFT JOIN container_tank tank ON tank.id=module.tank_id
LEFT JOIN device ON device.id=intent.device_id AND device.org_id=intent.organization_id
LEFT JOIN hashboard_slot slot ON slot.module_id=installation.module_id AND slot.position=installation.position
LEFT JOIN hosted_account_asset asset ON asset.organization_id=intent.organization_id AND asset.kind=7 AND asset.asset_id=installation.hashboard_id
WHERE intent.organization_id=$1 ORDER BY intent.hashboard_id;
