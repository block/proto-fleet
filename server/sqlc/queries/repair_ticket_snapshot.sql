-- The domain has already locked and revalidated physical placements. Resolve
-- opening identities through the same view used by the asset picker.
-- name: ResolveRepairTicketMinerTarget :one
SELECT * FROM repair_ticket_asset_option
WHERE org_id=sqlc.arg(org_id) AND kind=1 AND miner_identifier=sqlc.arg(miner_identifier);

-- Keep the opening owner/subowner stable until the ticket and audience commit.
-- name: LockRepairTicketTargetAllocation :one
SELECT client_id,subclient_id FROM hosted_account_asset
WHERE organization_id=sqlc.arg(org_id) AND kind=sqlc.arg(allocation_kind) AND asset_id=sqlc.arg(asset_id)
FOR SHARE;

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
