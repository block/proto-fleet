-- name: CreateMiningGroupPoolHandoff :exec
INSERT INTO mining_group_pool_handoff(organization_id,operation_id,device_id,captured) VALUES($1,$2,$3,$4);

-- name: GetMiningGroupPoolHandoff :one
SELECT * FROM mining_group_pool_handoff WHERE organization_id=$1 AND operation_id=$2 AND device_id=$3;

-- name: CompleteMiningGroupPoolHandoff :exec
UPDATE mining_group_pool_handoff SET completed_at=clock_timestamp()
WHERE organization_id=$1 AND operation_id=$2 AND device_id=$3 AND completed_at IS NULL;

-- name: SetMiningGroupPoolHandoffPlacementPending :exec
UPDATE mining_group_pool_handoff SET placement_pending=$4
WHERE organization_id=$1 AND operation_id=$2 AND device_id=$3;

-- name: HasPendingMiningGroupPoolHandoff :one
SELECT EXISTS(SELECT 1 FROM mining_group_pool_handoff
 WHERE organization_id=$1 AND device_id=$2 AND placement_pending)::boolean;

-- name: UpdateMiningGroupPoolHandoffCapture :exec
UPDATE mining_group_pool_handoff SET captured=$4
WHERE organization_id=$1 AND operation_id=$2 AND device_id=$3;

-- name: ListActivationPoolInventory :many
-- Resolve both aliases to the same physical board. Placement is not identity.
WITH input AS (SELECT value->>'mac_address' AS mac_address,value->>'serial' AS serial FROM jsonb_array_elements(sqlc.arg(boards)::jsonb))
SELECT h.id AS hashboard_id, x.mac_address::text AS mac_address, x.serial::text AS serial,
 COALESCE(a.subclient_id,a.client_id,0)::bigint AS account_id,
 COALESCE(a.revision,0)::bigint AS revision,
 (o.device_id=sqlc.arg(device_id) AND o.released_at IS NULL
  AND o.site_id IS NOT DISTINCT FROM d.site_id
  AND o.last_seen_at>now()-INTERVAL '2 minutes'
  AND o.last_seen_at<=now()+INTERVAL '5 seconds')::boolean AS ready
FROM input x
JOIN hashboard_identity mac ON mac.org_id=sqlc.arg(organization_id) AND mac.kind='mac' AND mac.value=x.mac_address
JOIN hashboard_identity serial ON serial.org_id=mac.org_id AND serial.kind='serial' AND serial.value=x.serial AND serial.hashboard_id=mac.hashboard_id
JOIN hashboard h ON h.id=mac.hashboard_id AND h.org_id=mac.org_id
JOIN hashboard_observation o ON o.hashboard_id=h.id
JOIN device d ON d.id=sqlc.arg(device_id) AND d.org_id=h.org_id AND d.deleted_at IS NULL
LEFT JOIN hosted_account_asset a ON a.organization_id=h.org_id AND a.kind=7 AND a.asset_id=h.id
ORDER BY h.id;

-- name: GetAdoptedPoolBoardSnapshot :one
-- Frozen effective usernames belong to the adopted assignment, not today's
-- worker metadata. A subsequent reviewed assignment uses its own rendering.
SELECT s.slots FROM pool_configuration_assignment a
JOIN pool_configuration_board_intent b ON b.organization_id=a.organization_id AND b.installation_id=a.target_id AND b.device_id=a.device_id
JOIN pool_configuration_group_snapshot s ON s.organization_id=b.organization_id AND s.device_id=b.device_id AND s.snapshot_id=b.group_snapshot_id
WHERE a.organization_id=$1 AND a.target_kind='hashboard_installation' AND a.target_id=$2
 AND a.retired_at IS NULL AND a.operation_id IS NULL
 AND s.configuration_id=a.configuration_id AND s.revision_id=a.revision_id;

-- name: HasActivationPoolInstallationAssignments :one
-- Include retired rows: activation must not overwrite an earlier assignment generation.
SELECT EXISTS(SELECT 1 FROM pool_configuration_assignment
 WHERE organization_id=sqlc.arg(organization_id) AND target_kind='hashboard_installation'
 AND target_id=ANY(sqlc.arg(installation_ids)::bigint[]))::boolean;

-- name: CreateActivationPoolBoardIntent :exec
INSERT INTO activation_pool_board_intent(organization_id,device_id,hashboard_id,operation_id,snapshot_id,mac_address,serial,account_id,ownership_revision)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9);

-- name: ListActivationPoolBoardIntents :many
SELECT * FROM activation_pool_board_intent WHERE organization_id=$1 AND device_id=$2 ORDER BY hashboard_id;

-- name: DeleteActivationPoolBoardIntent :exec
DELETE FROM activation_pool_board_intent WHERE organization_id=$1 AND device_id=$2 AND hashboard_id=$3;

-- name: HasRetainedContainerPoolHandoff :one
SELECT EXISTS(SELECT 1 FROM mining_group_pool_handoff
 WHERE organization_id=$1 AND device_id=$2 AND placement_pending
 AND NOT COALESCE((captured->>'PartialTransfer')::boolean,false))::boolean;

-- name: HasPendingActivationPoolTarget :one
SELECT COALESCE(EXISTS(SELECT 1 FROM mining_group_pool_handoff h
 WHERE h.organization_id=sqlc.arg(organization_id) AND h.device_id=sqlc.arg(device_id) AND h.placement_pending
 AND (sqlc.arg(target_kind)::text='device' OR NOT COALESCE((h.captured->>'PartialTransfer')::boolean,false)))
 OR EXISTS(SELECT 1 FROM activation_pool_board_intent p JOIN hashboard_installation i ON i.hashboard_id=p.hashboard_id
 WHERE p.organization_id=sqlc.arg(organization_id) AND i.id=sqlc.arg(target_id)
 AND sqlc.arg(target_kind)::text='hashboard_installation') ,false)::boolean AS pending;

-- name: ListPendingActivationBoardAssignments :many
SELECT COALESCE(i.id,0)::bigint AS installation_id,p.hashboard_id,COALESCE(i.module_id,0)::bigint AS module_id,
 p.device_id,d.device_identifier,d.site_id,c.configuration_id,c.name,''::text AS operation_id,
 'placement_pending'::text AS state,''::text AS result_code,d.created_at AS updated_at
FROM activation_pool_board_intent p
JOIN device d ON d.id=p.device_id AND d.org_id=p.organization_id AND d.deleted_at IS NULL
JOIN pool_configuration_group_snapshot s ON s.organization_id=p.organization_id AND s.snapshot_id=p.snapshot_id
JOIN pool_configuration c ON c.organization_id=s.organization_id AND c.configuration_id=s.configuration_id
LEFT JOIN hosted_account_asset owner ON owner.organization_id=p.organization_id AND owner.kind=7 AND owner.asset_id=p.hashboard_id
LEFT JOIN LATERAL (SELECT * FROM hashboard_installation inst WHERE inst.hashboard_id=p.hashboard_id
 AND inst.device_id=p.device_id AND inst.removed_at IS NULL ORDER BY inst.id DESC LIMIT 1) i ON true
WHERE p.organization_id=sqlc.arg(organization_id)
 AND c.account_id IS NOT DISTINCT FROM sqlc.narg(account_id)::bigint
 AND COALESCE(owner.subclient_id,owner.client_id,0)=COALESCE(c.account_id,0)
 AND (p.hashboard_id=ANY(sqlc.arg(hashboard_ids)::bigint[]) OR i.module_id=ANY(sqlc.arg(module_ids)::bigint[]))
ORDER BY p.hashboard_id;
