-- name: ListPoolEquipmentAssignments :many
SELECT d.id AS device_id, d.device_identifier, d.site_id,
 COALESCE(d.custom_name,'')::text AS custom_name, COALESCE(dd.manufacturer,'')::text AS manufacturer, COALESCE(dd.model,'')::text AS model,d.mac_address,
 COALESCE(c.configuration_id,'')::text AS configuration_id, COALESCE(c.name,'')::text AS name, COALESCE(a.operation_id,'')::text AS operation_id,
 COALESCE(ob.state,t.state,CASE WHEN c.configuration_id IS NULL THEN 'unavailable' ELSE 'adopted' END)::text AS state, COALESCE(ob.result_code,t.result_code,'')::text AS result_code,
 COALESCE(ob.observed_at,t.updated_at,a.updated_at,d.created_at)::timestamptz AS updated_at
FROM device d
JOIN discovered_device dd ON dd.id=d.discovered_device_id AND dd.org_id=d.org_id AND dd.deleted_at IS NULL
LEFT JOIN pool_configuration_assignment a ON a.organization_id=d.org_id AND a.device_id=d.id
 AND a.target_kind='device' AND a.target_id=d.id AND a.retired_at IS NULL
LEFT JOIN pool_configuration c ON c.organization_id=a.organization_id AND c.configuration_id=a.configuration_id
LEFT JOIN pool_configuration_operation_target t ON t.organization_id=a.organization_id
 AND t.operation_id=a.operation_id AND t.target_kind=a.target_kind AND t.target_id=a.target_id
 AND t.assignment_version=a.version
LEFT JOIN pool_configuration_observation ob ON ob.organization_id=a.organization_id AND ob.target_kind=a.target_kind
 AND ob.target_id=a.target_id AND ob.assignment_version=a.version AND ob.observed_at>COALESCE(t.updated_at,a.updated_at)
WHERE d.org_id=sqlc.arg(organization_id) AND d.deleted_at IS NULL
 AND (d.id=ANY(sqlc.arg(device_ids)::bigint[]) OR d.device_identifier=ANY(sqlc.arg(device_identifiers)::text[]))
 AND (c.configuration_id IS NULL OR c.account_id IS NOT DISTINCT FROM sqlc.narg(account_id)::bigint)
 AND ((dd.device_class=1 AND COALESCE((SELECT COALESCE(asset.subclient_id,asset.client_id) FROM hosted_account_asset asset
   WHERE asset.organization_id=d.org_id AND asset.kind=5 AND asset.asset_id=d.id),0)=COALESCE(sqlc.narg(account_id)::bigint,0))
 OR (dd.device_class=2 AND EXISTS (SELECT 1 FROM container_module m JOIN container_tank tank ON tank.id=m.tank_id
   WHERE tank.device_id=d.id AND tank.present AND m.retired_at IS NULL)
  AND NOT EXISTS (SELECT 1 FROM container_module m JOIN container_tank tank ON tank.id=m.tank_id
   LEFT JOIN hosted_module_membership membership ON membership.module_id=m.id AND membership.org_id=d.org_id
   LEFT JOIN LATERAL unnest(membership.board_ids) b(id) ON true
   LEFT JOIN hosted_account_asset asset ON asset.organization_id=d.org_id AND asset.kind=7 AND asset.asset_id=b.id
   WHERE tank.device_id=d.id AND tank.present AND m.retired_at IS NULL
    AND (membership.module_id IS NULL OR COALESCE(asset.subclient_id,asset.client_id,0)<>COALESCE(sqlc.narg(account_id)::bigint,0)))))
ORDER BY d.id;

-- name: ListRetainedDeviceAssignmentsForRecovery :many
-- Recovery lookup for a retained whole-container (device-target) assignment that
-- ListPoolEquipmentAssignments hides when a container's module membership is
-- incomplete. It deliberately omits that membership-completeness gate, but still
-- returns ONLY the caller's own assignment (c.account_id IS NOT DISTINCT FROM
-- account_id) on a container (device_class=2). An incomplete membership map can
-- therefore no longer turn an authorized retained assignment into a false
-- "no assignment remains"; customer-owned equipment is never exposed.
SELECT d.id AS device_id, d.device_identifier, d.site_id,
 COALESCE(d.custom_name,'')::text AS custom_name, COALESCE(dd.manufacturer,'')::text AS manufacturer, COALESCE(dd.model,'')::text AS model, d.mac_address,
 a.configuration_id, COALESCE(c.name,'')::text AS name, COALESCE(a.operation_id,'')::text AS operation_id,
 a.updated_at::timestamptz AS updated_at
FROM device d
JOIN discovered_device dd ON dd.id=d.discovered_device_id AND dd.org_id=d.org_id AND dd.deleted_at IS NULL
JOIN pool_configuration_assignment a ON a.organization_id=d.org_id AND a.device_id=d.id
 AND a.target_kind='device' AND a.target_id=d.id AND a.retired_at IS NULL
JOIN pool_configuration c ON c.organization_id=a.organization_id AND c.configuration_id=a.configuration_id
WHERE d.org_id=sqlc.arg(organization_id) AND d.deleted_at IS NULL AND dd.device_class=2
 AND (d.id=ANY(sqlc.arg(device_ids)::bigint[]) OR d.device_identifier=ANY(sqlc.arg(device_identifiers)::text[]))
 AND c.account_id IS NOT DISTINCT FROM sqlc.narg(account_id)::bigint
ORDER BY d.id;
