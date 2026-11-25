-- name: LockMaintenanceSiteForTicket :one
SELECT id
FROM site
WHERE org_id = sqlc.arg('org_id')
  AND id = sqlc.arg('site_id')
  AND deleted_at IS NULL
FOR SHARE;

-- name: LockMaintenanceBuildingForTicket :one
SELECT id
FROM building
WHERE org_id = sqlc.arg('org_id')
  AND id = sqlc.arg('building_id')
  AND deleted_at IS NULL
FOR SHARE;

-- name: LockMaintenanceRackForTicket :one
-- Match rack placement writers' dsr → ds table order.
SELECT ds.id
FROM device_set_rack dsr
JOIN device_set ds
  ON ds.id = dsr.device_set_id AND ds.org_id = dsr.org_id
WHERE dsr.org_id = sqlc.arg('org_id')
  AND dsr.device_set_id = sqlc.arg('rack_id')
  AND ds.deleted_at IS NULL
FOR SHARE OF dsr, ds;

-- name: LockMaintenanceMinerForTicket :one
SELECT id
FROM device
WHERE org_id = sqlc.arg('org_id')
  AND device_identifier = sqlc.arg('miner_identifier')
  AND deleted_at IS NULL
FOR SHARE;

-- name: ResolveMaintenanceMinerContext :one
SELECT
    d.device_identifier AS miner_identifier,
    COALESCE(d.site_id, dsr.site_id) AS site_id,
    COALESCE(s.name, '') AS site_name,
    COALESCE(d.building_id, dsr.building_id) AS building_id,
    COALESCE(b.name, '') AS building_name,
    dsr.zone,
    rack.id AS rack_id,
    rack.label AS rack_label,
    COALESCE(grp.label, '') AS group_label
FROM device d
LEFT JOIN device_set_membership rack_member
  ON rack_member.device_id = d.id AND rack_member.org_id = d.org_id AND rack_member.device_set_type = 'rack'
LEFT JOIN device_set rack
  ON rack.id = rack_member.device_set_id AND rack.org_id = d.org_id AND rack.deleted_at IS NULL
LEFT JOIN device_set_rack dsr
  ON dsr.device_set_id = rack.id AND dsr.org_id = d.org_id
LEFT JOIN site s
  ON s.id = COALESCE(d.site_id, dsr.site_id) AND s.org_id = d.org_id AND s.deleted_at IS NULL
LEFT JOIN building b
  ON b.id = COALESCE(d.building_id, dsr.building_id) AND b.org_id = d.org_id AND b.deleted_at IS NULL
LEFT JOIN LATERAL (
    SELECT sets.label
    FROM device_set_membership member
    JOIN device_set sets ON sets.id = member.device_set_id AND sets.org_id = member.org_id
    WHERE member.device_id = d.id AND member.org_id = d.org_id
      AND member.device_set_type = 'group' AND sets.deleted_at IS NULL
    ORDER BY sets.label, sets.id
    LIMIT 1
) grp ON TRUE
WHERE d.org_id = sqlc.arg('org_id') AND d.device_identifier = sqlc.arg('miner_identifier')
  AND d.deleted_at IS NULL;

-- name: ResolveMaintenanceLocationContext :one
SELECT
    s.id AS site_id,
    COALESCE(s.name, '') AS site_name,
    b.id AS building_id,
    COALESCE(b.name, '') AS building_name
FROM (SELECT 1) singleton
LEFT JOIN building b
  ON b.id = sqlc.narg('building_id') AND b.org_id = sqlc.arg('org_id') AND b.deleted_at IS NULL
LEFT JOIN site s
  ON s.id = COALESCE(sqlc.narg('site_id'), b.site_id)
 AND s.org_id = sqlc.arg('org_id') AND s.deleted_at IS NULL
WHERE (sqlc.narg('site_id')::bigint IS NULL OR s.id IS NOT NULL)
  AND (sqlc.narg('building_id')::bigint IS NULL OR b.id IS NOT NULL)
  AND (sqlc.narg('site_id')::bigint IS NULL OR sqlc.narg('building_id')::bigint IS NULL OR b.site_id = s.id);

-- name: ResolveMaintenanceAssignee :one
-- Assignment writes call this inside their transaction. Shared locks on the
-- membership and user serialize with membership removal and user deactivation.
SELECT u.id AS user_id, u.username, COALESCE(r.name, '') AS role_name
FROM user_organization uo
JOIN "user" u ON u.id = uo.user_id AND u.deleted_at IS NULL
JOIN role r ON r.id = uo.role_id AND r.organization_id = uo.organization_id AND r.deleted_at IS NULL
WHERE uo.organization_id = sqlc.arg('org_id') AND uo.user_id = sqlc.arg('user_id')
  AND uo.deleted_at IS NULL
FOR SHARE OF uo, u;

-- name: ListMaintenanceAssignees :many
SELECT u.id AS user_id, u.username, COALESCE(r.name, '') AS role_name
FROM user_organization uo
JOIN "user" u ON u.id = uo.user_id AND u.deleted_at IS NULL
JOIN role r ON r.id = uo.role_id AND r.organization_id = uo.organization_id AND r.deleted_at IS NULL
WHERE uo.organization_id = sqlc.arg('org_id') AND uo.deleted_at IS NULL
ORDER BY LOWER(u.username), u.id;

-- name: UpdateRepairTicketTargetEvidence :execrows
UPDATE repair_ticket_target SET
 diagnosis=COALESCE(sqlc.narg(diagnosis),diagnosis),
 warranty_evidence=COALESCE(sqlc.narg(warranty_evidence),warranty_evidence),
 repair_outcome=COALESCE(sqlc.narg(repair_outcome),repair_outcome),
 rma_outcome=COALESCE(sqlc.narg(rma_outcome),rma_outcome),
 model=COALESCE(sqlc.narg(module_model),model),
 replacement_module_id=CASE WHEN sqlc.arg(clear_replacement_module)::boolean THEN NULL ELSE COALESCE(sqlc.narg(replacement_module_id),replacement_module_id) END,
 replacement_hashboard_id=CASE WHEN sqlc.arg(clear_replacement_hashboard)::boolean THEN NULL ELSE COALESCE(sqlc.narg(replacement_hashboard_id),replacement_hashboard_id) END
WHERE org_id=sqlc.arg(org_id) AND ticket_id=sqlc.arg(ticket_id) AND kind=sqlc.arg(kind) AND asset_id=sqlc.arg(asset_id);

-- name: RecordMaintenanceModuleModel :execrows
UPDATE container_module m SET model=sqlc.arg(model)
FROM container_tank t,device d WHERE m.id=sqlc.arg(module_id) AND t.id=m.tank_id AND d.id=t.device_id AND d.org_id=sqlc.arg(org_id);

-- name: LockMaintenanceModelDevice :one
SELECT id FROM device WHERE id=sqlc.arg(device_id) AND org_id=sqlc.arg(org_id) AND deleted_at IS NULL FOR SHARE;

-- name: LockMaintenanceModuleModelSite :one
SELECT d.site_id FROM device d
JOIN container_tank t ON t.device_id=d.id
JOIN container_module m ON m.tank_id=t.id
WHERE d.org_id=sqlc.arg(org_id) AND d.id=sqlc.arg(device_id) AND m.id=sqlc.arg(module_id)
 AND d.deleted_at IS NULL AND m.retired_at IS NULL
FOR SHARE OF t FOR UPDATE OF m;

-- name: TouchRepairTicket :exec
UPDATE repair_ticket SET updated_at=clock_timestamp() WHERE org_id=$1 AND id=$2;

-- name: ResolveMaintenanceMinerIdentifierByID :one
SELECT device_identifier FROM device WHERE org_id=$1 AND id=$2 AND deleted_at IS NULL;

-- name: LockMaintenanceTicketInstallation :one
SELECT i.id FROM hashboard_installation i
JOIN hashboard b ON b.id=i.hashboard_id
WHERE b.org_id=sqlc.arg(org_id) AND i.id=sqlc.arg(installation_id)
 AND i.module_id=sqlc.arg(module_id) AND i.removed_at IS NULL
FOR SHARE OF i;

-- name: CanAccountCloseRepairTicket :one
-- The service holds the ticket row lock, also used by reservation/RMA writers.
SELECT EXISTS (
 SELECT 1 FROM repair_ticket r
 WHERE r.org_id=sqlc.arg(org_id) AND r.id=sqlc.arg(ticket_id)
  AND r.created_by_account_id=sqlc.arg(account_id) AND r.status=1
  AND r.deleted_at IS NULL
  AND COALESCE(btrim(r.rma_vendor),'')=''
  AND COALESCE(btrim(r.rma_tracking),'')='' AND r.rma_eta IS NULL
  AND NOT EXISTS (
   SELECT 1 FROM repair_ticket_part p
   WHERE p.org_id=r.org_id AND p.ticket_id=r.id AND p.consumed_at IS NULL
  )
)::boolean AS allowed;
