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
