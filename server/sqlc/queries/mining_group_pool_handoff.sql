-- name: CreateMiningGroupPoolHandoff :exec
INSERT INTO mining_group_pool_handoff(organization_id,operation_id,device_id,captured) VALUES($1,$2,$3,$4);

-- name: GetMiningGroupPoolHandoff :one
SELECT * FROM mining_group_pool_handoff WHERE organization_id=$1 AND operation_id=$2 AND device_id=$3;

-- name: CompleteMiningGroupPoolHandoff :exec
UPDATE mining_group_pool_handoff SET completed_at=clock_timestamp()
WHERE organization_id=$1 AND operation_id=$2 AND device_id=$3 AND completed_at IS NULL;

-- name: GetAdoptedPoolBoardSnapshot :one
-- Frozen effective usernames belong to the adopted assignment, not today's
-- worker metadata. A subsequent reviewed assignment uses its own rendering.
SELECT s.slots FROM pool_configuration_assignment a
JOIN pool_configuration_board_intent b ON b.organization_id=a.organization_id AND b.installation_id=a.target_id AND b.device_id=a.device_id
JOIN pool_configuration_group_snapshot s ON s.organization_id=b.organization_id AND s.device_id=b.device_id AND s.snapshot_id=b.group_snapshot_id
WHERE a.organization_id=$1 AND a.target_kind='hashboard_installation' AND a.target_id=$2
 AND a.retired_at IS NULL AND a.operation_id IS NULL
 AND s.configuration_id=a.configuration_id AND s.revision_id=a.revision_id;
