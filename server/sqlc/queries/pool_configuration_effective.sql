-- name: HasPoolConfigurationCutover :one
SELECT EXISTS(SELECT 1 FROM pool_configuration_cutover WHERE organization_id=$1 AND completed_at IS NOT NULL);

-- name: RecordPoolConfigurationCutover :exec
UPDATE pool_configuration_cutover SET completed_at=clock_timestamp(),report=$2 WHERE organization_id=$1 AND completed_at IS NULL;

-- name: RetainAppliedPoolGroupIntents :exec
-- Called with a settled controller observation or a successful configuration
-- receipt, before operation outcomes change. Never invent a successful attempt.
INSERT INTO pool_configuration_board_intent(organization_id,installation_id,device_id,hashboard_id,mac_address,group_snapshot_id,operation_id,physical_attempt_id)
SELECT a.organization_id,a.target_id,a.device_id,i.hashboard_id,i.metadata->>'macAddress',t.group_snapshot_id,t.operation_id,t.physical_attempt_id
FROM pool_configuration_operation_target t
JOIN pool_configuration_assignment a ON a.organization_id=t.organization_id AND a.target_kind=t.target_kind AND a.target_id=t.target_id
JOIN hashboard_installation i ON i.id=a.target_id
WHERE t.organization_id=sqlc.arg(organization_id) AND t.physical_attempt_id=sqlc.narg(physical_attempt_id)::uuid
 AND t.target_kind='hashboard_installation' AND t.group_snapshot_id=sqlc.arg(group_snapshot_id)
 AND i.metadata->>'macAddress'=ANY(sqlc.arg(mac_addresses)::text[]) AND i.removed_at IS NULL
ON CONFLICT(organization_id,installation_id) DO UPDATE SET
 group_snapshot_id=EXCLUDED.group_snapshot_id,operation_id=EXCLUDED.operation_id,
 physical_attempt_id=EXCLUDED.physical_attempt_id,adopted_at=NULL,updated_at=clock_timestamp()
WHERE (pool_configuration_board_intent.group_snapshot_id,pool_configuration_board_intent.operation_id,pool_configuration_board_intent.physical_attempt_id)
 IS DISTINCT FROM (EXCLUDED.group_snapshot_id,EXCLUDED.operation_id,EXCLUDED.physical_attempt_id);

-- name: AdoptPoolGroupIntent :exec
INSERT INTO pool_configuration_board_intent(organization_id,installation_id,device_id,hashboard_id,mac_address,group_snapshot_id,adopted_at)
SELECT a.organization_id,a.target_id,a.device_id,i.hashboard_id,i.metadata->>'macAddress',sqlc.arg(group_snapshot_id),a.adopted_at
FROM pool_configuration_assignment a JOIN hashboard_installation i ON i.id=a.target_id
WHERE a.organization_id=sqlc.arg(organization_id) AND a.target_id=sqlc.arg(installation_id)
 AND a.target_kind='hashboard_installation' AND a.operation_id IS NULL AND a.retired_at IS NULL
ON CONFLICT(organization_id,installation_id) DO NOTHING;

-- name: HasDetachedPoolGroupIntents :one
SELECT EXISTS (
 SELECT 1 FROM pool_configuration_board_intent p WHERE p.organization_id=$1 AND p.device_id=$2
 AND NOT EXISTS (
  SELECT 1 FROM hashboard_installation i JOIN container_module m ON m.id=i.module_id
  JOIN container_tank t ON t.id=m.tank_id
  WHERE i.id=p.installation_id AND i.hashboard_id=p.hashboard_id AND i.removed_at IS NULL
   AND m.retired_at IS NULL AND t.device_id=p.device_id AND i.device_id=p.device_id
   AND i.metadata->>'macAddress'=p.mac_address
 )
);

-- name: PruneDetachedPoolGroupIntents :exec
DELETE FROM pool_configuration_board_intent p WHERE p.organization_id=$1 AND p.device_id=$2
AND NOT EXISTS (
 SELECT 1 FROM hashboard_installation i JOIN container_module m ON m.id=i.module_id
 JOIN container_tank t ON t.id=m.tank_id
 WHERE i.id=p.installation_id AND i.hashboard_id=p.hashboard_id AND i.removed_at IS NULL
  AND m.retired_at IS NULL AND t.device_id=p.device_id AND i.device_id=p.device_id
  AND i.metadata->>'macAddress'=p.mac_address
);
