-- name: ListPoolAssignmentPhysicalBindings :many
-- Called around accepted inventory reconciliation under its organization lock.
-- Include incoming module identities so a destination poll also invalidates
-- assignments left behind on the source controller. Telemetry timestamps are
-- deliberately absent: only a physical change ends the assignment generation.
SELECT a.target_id AS installation_id,
 jsonb_build_array(i.hashboard_id,i.module_id,i.position,i.device_id,i.site_id,
  i.metadata->>'macAddress',i.removed_at,m.tank_id,m.position,m.observation_site_id,
  m.reported_id,m.retired_at,t.present,d.deleted_at)::text AS physical_binding,
 COALESCE(d.id IS NOT NULL AND d.deleted_at IS NULL AND i.id IS NOT NULL AND i.removed_at IS NULL
  AND m.id IS NOT NULL AND m.retired_at IS NULL AND t.present
  AND i.device_id=a.device_id AND t.device_id=a.device_id
  AND i.tank_id=m.tank_id AND i.module_position=m.position
  AND i.site_id IS NOT DISTINCT FROM d.site_id,FALSE)::boolean AS valid
FROM pool_configuration_assignment a
LEFT JOIN hashboard_installation i ON i.id=a.target_id
LEFT JOIN container_module m ON m.id=i.module_id
LEFT JOIN container_tank t ON t.id=m.tank_id
LEFT JOIN device d ON d.id=a.device_id AND d.org_id=a.organization_id
WHERE a.organization_id=sqlc.arg(organization_id) AND a.target_kind='hashboard_installation' AND a.retired_at IS NULL
 AND (a.device_id=sqlc.arg(device_id) OR t.device_id=sqlc.arg(device_id)
  OR EXISTS (SELECT 1 FROM container_module_identity identity
   WHERE identity.org_id=a.organization_id AND identity.module_id=m.id
    AND identity.reported_module_id=ANY(sqlc.arg(reported_module_ids)::text[])))
ORDER BY a.target_id;

-- name: RetirePoolConfigurationEquipmentAssignments :exec
-- Explicit lifecycle action. The caller retains inventory/deletion locks and
-- commits retirement and supersession with the physical change. Never erase
-- the device-write journal: superseding intent is not proof of physical drain.
WITH retired AS (
 UPDATE pool_configuration_assignment a
 SET retired_at=clock_timestamp(),version=a.version+1,updated_at=clock_timestamp()
 WHERE a.organization_id=sqlc.arg(organization_id) AND a.retired_at IS NULL
  AND ((a.target_kind='hashboard_installation' AND a.target_id=ANY(sqlc.arg(installation_ids)::bigint[]))
   OR EXISTS (SELECT 1 FROM device d WHERE d.org_id=a.organization_id AND d.id=a.device_id
    AND d.device_identifier=ANY(sqlc.arg(device_identifiers)::text[])))
 RETURNING a.organization_id,a.target_kind,a.target_id
)
UPDATE pool_configuration_operation_target t SET state='superseded',updated_at=clock_timestamp()
FROM retired a WHERE t.organization_id=a.organization_id AND t.target_kind=a.target_kind AND t.target_id=a.target_id
 AND t.state IN ('pending','applying','failed','different','applied_unverified');
