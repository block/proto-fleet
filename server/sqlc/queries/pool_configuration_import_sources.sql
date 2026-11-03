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
 ) OR EXISTS (
    SELECT 1 FROM container_mining_group_state state,
      LATERAL jsonb_each(state.bindings) binding
    WHERE state.organization_id=r.organization_id AND binding.value->>'config_id'=r.revision_id
 ))
ORDER BY r.revision_id;

-- name: LockPoolAssignmentAdoptionSources :exec
LOCK TABLE hosted_account_pool, hosted_account_pool_revision, hosted_account_pool_profile,
 pool_configuration, pool_configuration_assignment, pool_configuration_operation_target, hashboard_pool_intent, container_mining_group_state, hashboard_installation,
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
 intent.pool_revision_id,intent.mac_address,revision.number AS source_version,
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

-- name: ListPoolImportControllerBindings :many
SELECT s.device_id,s.bindings,s.observed,s.status FROM container_mining_group_state s
WHERE s.organization_id=$1 ORDER BY s.device_id;

-- name: RewritePoolImportControllerBindings :exec
UPDATE container_mining_group_state SET bindings=$3,updated_at=clock_timestamp()
WHERE organization_id=$1 AND device_id=$2;

-- name: AdoptLegacyPoolBoardAssignments :many
-- Eligibility was checked under the maintenance source locks. Join the durable
-- source mapping once and never fire insert triggers for idempotent reruns.
WITH candidates AS (
 SELECT i.installation_id,i.device_id,m.configuration_id,m.revision_id
 FROM hashboard_pool_intent i
 JOIN hosted_account_pool_revision r ON r.organization_id=i.organization_id AND r.revision_id=i.pool_revision_id
 JOIN pool_configuration_import m ON m.organization_id=i.organization_id
  AND m.source_kind='hosted_revision' AND m.source_id=i.pool_revision_id AND m.source_version=r.number::text
 JOIN pool_configuration c ON c.organization_id=m.organization_id AND c.configuration_id=m.configuration_id
 WHERE i.organization_id=sqlc.arg(organization_id) AND i.installation_id=ANY(sqlc.arg(installation_ids)::bigint[])
  AND NOT c.archived AND c.current_revision_id=m.revision_id
  AND NOT EXISTS(SELECT 1 FROM pool_configuration_assignment a WHERE a.organization_id=m.organization_id
   AND a.device_id=i.device_id AND a.target_kind<>'hashboard_installation' AND a.retired_at IS NULL)
), inserted AS (
 INSERT INTO pool_configuration_assignment(organization_id,target_kind,target_id,device_id,configuration_id,revision_id,version,adopted_at)
 SELECT sqlc.arg(organization_id),'hashboard_installation',c.installation_id,c.device_id,c.configuration_id,c.revision_id,1,clock_timestamp()
 FROM candidates c WHERE NOT EXISTS(SELECT 1 FROM pool_configuration_assignment a
  WHERE a.organization_id=sqlc.arg(organization_id) AND a.target_kind='hashboard_installation' AND a.target_id=c.installation_id)
 RETURNING target_id,device_id,configuration_id,revision_id
)
SELECT * FROM inserted
UNION ALL
SELECT a.target_id,a.device_id,a.configuration_id,a.revision_id
FROM candidates c JOIN pool_configuration_assignment a ON a.organization_id=sqlc.arg(organization_id)
 AND a.target_kind='hashboard_installation' AND a.target_id=c.installation_id AND a.device_id=c.device_id
 AND a.configuration_id=c.configuration_id AND a.revision_id=c.revision_id
WHERE a.operation_id IS NULL AND a.retired_at IS NULL
ORDER BY target_id;
