-- The domain has already locked and revalidated physical placements. Resolve
-- opening identities through the same view used by the asset picker.
-- name: ResolveRepairTicketMinerTarget :one
SELECT * FROM repair_ticket_asset_option
WHERE org_id=sqlc.arg(org_id) AND kind=1 AND miner_identifier=sqlc.arg(miner_identifier);

-- Keep the opening owner/subowner stable until the ticket and audience commit.
-- name: LockRepairTicketTargetAllocation :one
WITH selections AS MATERIALIZED (
 SELECT sqlc.arg(allocation_kind)::int AS kind,sqlc.arg(asset_id)::bigint AS asset_id
 WHERE sqlc.arg(allocation_kind)::int<>6
 UNION ALL
 SELECT 7,unnest(m.board_ids) FROM hosted_module_membership m
 WHERE sqlc.arg(allocation_kind)::int=6 AND m.org_id=sqlc.arg(org_id) AND m.module_id=sqlc.arg(asset_id)
), locked AS MATERIALIZED (
 SELECT a.asset_id,a.client_id,a.subclient_id FROM hosted_account_asset a
 JOIN selections s ON s.kind=a.kind AND s.asset_id=a.asset_id
 WHERE a.organization_id=sqlc.arg(org_id)
 FOR SHARE OF a
), owners AS (
 SELECT COALESCE(a.client_id,0) AS client_id,COALESCE(a.subclient_id,0) AS subclient_id
 FROM selections s LEFT JOIN locked a ON a.asset_id=s.asset_id
)
SELECT CASE WHEN min(client_id)=max(client_id) THEN min(client_id) ELSE 0 END::bigint AS client_id,
 CASE WHEN min(subclient_id)=max(subclient_id) THEN min(subclient_id) ELSE 0 END::bigint AS subclient_id
FROM owners HAVING count(*)>0;

-- name: InsertRepairTicketOpeningTarget :exec
INSERT INTO repair_ticket_target (
 org_id,ticket_id,kind,asset_id,miner_id,miner_identifier,building_id,rack_id,
 module_id,hashboard_id,installation_id,container_id,site_id,label,model,position,client_id,subclient_id
) VALUES (
 sqlc.arg(org_id),sqlc.arg(ticket_id),sqlc.arg(kind),sqlc.arg(asset_id),sqlc.narg(miner_id),
 sqlc.narg(miner_identifier),sqlc.narg(building_id),sqlc.narg(rack_id),sqlc.narg(module_id),
 sqlc.narg(hashboard_id),sqlc.narg(installation_id),sqlc.narg(container_id),sqlc.narg(site_id),
 sqlc.arg(label),sqlc.arg(model),sqlc.narg(position),sqlc.narg(client_id),sqlc.narg(subclient_id)
)
ON CONFLICT(org_id,ticket_id,kind,asset_id) DO NOTHING;

-- name: InsertRepairTicketOpeningAudience :exec
INSERT INTO repair_ticket_audience(org_id,ticket_id,account_id)
SELECT sqlc.arg(org_id),sqlc.arg(ticket_id),unnest(sqlc.arg(account_ids)::bigint[])
ON CONFLICT(org_id,ticket_id,account_id) DO NOTHING;
