-- name: SetHostedAccountTicketing :exec
UPDATE hosted_account SET ticketing_enabled=$1,revision=revision+1,authority_revision=authority_revision+1,updated_at=CURRENT_TIMESTAMP WHERE organization_id=$2 AND id=$3;

-- name: ListRepairTicketAssetOptions :many
SELECT o.* FROM repair_ticket_asset_option o
LEFT JOIN hosted_account_asset_projection a ON a.organization_id=o.org_id AND a.kind=o.allocation_kind AND a.asset_id=o.parent_asset_id
WHERE o.org_id=sqlc.arg(org_id)
 AND (sqlc.narg(kind)::smallint IS NULL OR o.kind=sqlc.narg(kind))
 AND (sqlc.narg(account_internal_id)::bigint IS NULL OR a.client_id=sqlc.narg(account_internal_id) OR a.subclient_id=sqlc.narg(account_internal_id))
 AND (sqlc.narg(site_ids)::bigint[] IS NULL OR o.site_id=ANY(sqlc.narg(site_ids)::bigint[]))
 AND (sqlc.narg(excluded_site_ids)::bigint[] IS NULL OR o.site_id IS NULL OR NOT(o.site_id=ANY(sqlc.narg(excluded_site_ids)::bigint[])))
 AND (sqlc.arg(search)::text='' OR strpos(lower(o.label),lower(sqlc.arg(search)))>0)
ORDER BY o.kind,o.id LIMIT 200;

-- name: ResolveRepairTicketAsset :one
SELECT o.* FROM repair_ticket_asset_option o
WHERE o.org_id=$1 AND o.kind=$2 AND o.id=$3;

-- name: GetRepairTicketMetadata :many
SELECT r.id,r.shared_with_clients,r.description,COALESCE(c.account_id,'')::text AS created_by_account_id,
 COALESCE((SELECT jsonb_agg(jsonb_strip_nulls(jsonb_build_object('kind',t.kind,'id',t.asset_id,'label',t.label,'model',t.model,'minerIdentifier',t.miner_identifier,'moduleId',t.module_id,'installationId',t.installation_id,'containerId',t.container_id,'position',t.position,'siteId',t.site_id,'replacementModuleId',t.replacement_module_id,'replacementHashboardId',t.replacement_hashboard_id,'diagnosis',t.diagnosis,'warrantyEvidence',t.warranty_evidence,'repairOutcome',t.repair_outcome,'rmaOutcome',t.rma_outcome)) ORDER BY t.kind,t.asset_id) FROM repair_ticket_target t WHERE t.org_id=r.org_id AND t.ticket_id=r.id),'[]'::jsonb)::jsonb AS targets,
 COALESCE((SELECT jsonb_agg(jsonb_build_object('accountId',a.account_id,'name',a.name) ORDER BY a.account_id) FROM repair_ticket_audience v JOIN hosted_account a ON a.id=v.account_id AND a.organization_id=v.org_id WHERE v.org_id=r.org_id AND v.ticket_id=r.id),'[]'::jsonb)::jsonb AS audiences
FROM repair_ticket r LEFT JOIN hosted_account c ON c.id=r.created_by_account_id AND c.organization_id=r.org_id
WHERE r.org_id=$1 AND r.id=ANY(sqlc.arg(ticket_ids)::bigint[]);

-- name: AssociateRepairTicketAccount :execrows
INSERT INTO repair_ticket_audience(org_id,ticket_id,account_id)
SELECT $1,$2,a.id FROM hosted_account a WHERE a.organization_id=$1 AND a.account_id=$3 AND a.depth>0 ON CONFLICT DO NOTHING;

-- name: AddRepairTicketBoard :execrows
INSERT INTO repair_ticket_target(org_id,ticket_id,kind,asset_id,module_id,hashboard_id,installation_id,container_id,site_id,label,model,position,client_id,subclient_id)
SELECT p.org_id,p.ticket_id,4,sqlc.arg(board_id)::bigint,p.module_id,sqlc.arg(board_id)::bigint,i.id,p.container_id,p.site_id,
 'Hashboard '||sqlc.arg(board_id)::bigint||' / Module '||p.module_id||' / slot '||i.position,p.model,i.position,p.client_id,p.subclient_id
FROM repair_ticket_target p JOIN repair_ticket r ON r.org_id=p.org_id AND r.id=p.ticket_id
JOIN hashboard_installation i ON i.module_id=p.module_id AND i.hashboard_id=sqlc.arg(board_id)::bigint AND i.installed_at<=r.created_at AND (i.removed_at IS NULL OR i.removed_at>r.created_at)
JOIN hashboard b ON b.id=i.hashboard_id AND b.org_id=p.org_id
WHERE p.org_id=sqlc.arg(org_id)::bigint AND p.ticket_id=sqlc.arg(ticket_id)::bigint AND p.module_id=sqlc.arg(parent_module_id)::bigint
ORDER BY p.kind,p.asset_id LIMIT 1
ON CONFLICT(org_id,ticket_id,kind,asset_id) DO UPDATE SET asset_id=EXCLUDED.asset_id;

-- name: RepairTicketContainsModule :one
SELECT EXISTS(SELECT 1 FROM repair_ticket_target WHERE org_id=$1 AND ticket_id=$2 AND module_id=$3)::boolean;

-- name: GetAccountTicketAccess :one
SELECT r.created_by_account_id,r.status
FROM repair_ticket r
JOIN repair_ticket_audience a ON a.org_id=r.org_id AND a.ticket_id=r.id AND a.account_id=sqlc.arg(account_internal_id)
WHERE r.org_id=sqlc.arg(org_id) AND r.id=sqlc.arg(ticket_id) AND r.deleted_at IS NULL AND r.shared_with_clients;

-- name: GetAccountRepairTicket :one
SELECT r.* FROM repair_ticket r
JOIN repair_ticket_audience a ON a.org_id=r.org_id AND a.ticket_id=r.id AND a.account_id=sqlc.arg(account_internal_id)
WHERE r.org_id=sqlc.arg(org_id) AND r.id=sqlc.arg(ticket_id) AND r.deleted_at IS NULL AND r.shared_with_clients;

-- name: ListAccountRepairTickets :many
SELECT r.* FROM repair_ticket r
JOIN repair_ticket_audience a ON a.org_id=r.org_id AND a.ticket_id=r.id AND a.account_id=sqlc.arg(account_internal_id)
WHERE r.org_id=sqlc.arg(org_id) AND r.deleted_at IS NULL AND r.shared_with_clients
 AND (sqlc.arg(before_id)::bigint=0 OR r.id<sqlc.arg(before_id))
 AND (sqlc.narg(statuses)::smallint[] IS NULL OR r.status=ANY(sqlc.narg(statuses)::smallint[]))
ORDER BY r.id DESC LIMIT 51;

-- name: ListAccountRepairTicketTargets :many
SELECT t.* FROM repair_ticket_target t
JOIN repair_ticket r ON r.org_id=t.org_id AND r.id=t.ticket_id
JOIN repair_ticket_audience a ON a.org_id=r.org_id AND a.ticket_id=r.id AND a.account_id=sqlc.arg(account_internal_id)
WHERE t.org_id=sqlc.arg(org_id) AND t.ticket_id=ANY(sqlc.arg(ticket_ids)::bigint[])
 AND r.deleted_at IS NULL AND r.shared_with_clients
 AND (t.client_id=sqlc.arg(account_internal_id) OR t.subclient_id=sqlc.arg(account_internal_id))
ORDER BY t.ticket_id,t.kind,t.asset_id;

-- name: ListAccountRepairTicketComments :many
WITH visible_comments AS NOT MATERIALIZED (
 SELECT c.id,c.ticket_id,c.user_name,c.text,c.created_at,c.user_id
 FROM repair_ticket_comment c
 WHERE c.org_id=sqlc.arg(org_id) AND c.ticket_id=sqlc.arg(ticket_id)
  AND NOT c.internal AND c.deleted_at IS NULL
  AND (c.account_id IS NULL OR c.account_id=sqlc.arg(account_internal_id))
), comment_page AS (
 SELECT c.* FROM visible_comments c
 WHERE sqlc.arg(before_comment_id)::bigint=0 OR (c.created_at,c.id) < (
  SELECT anchor.created_at,anchor.id FROM visible_comments anchor WHERE anchor.id=sqlc.arg(before_comment_id)
 )
 ORDER BY c.created_at DESC,c.id DESC LIMIT 51
)
SELECT COALESCE(c.id,0)::bigint AS id,COALESCE(c.ticket_id,0)::bigint AS ticket_id,
 COALESCE(c.user_name,'')::text AS user_name,COALESCE(c.text,'')::text AS text,
 COALESCE(c.created_at,'epoch'::timestamptz)::timestamptz AS created_at,
 COALESCE(c.user_id=sqlc.arg(actor_id),false)::boolean AS authored_by_caller,
 (sqlc.arg(before_comment_id)::bigint=0 OR EXISTS(SELECT 1 FROM visible_comments anchor WHERE anchor.id=sqlc.arg(before_comment_id)))::boolean AS cursor_valid
FROM (SELECT 1) singleton LEFT JOIN comment_page c ON TRUE
ORDER BY c.created_at DESC,c.id DESC;

-- name: CanTargetAccountRepairAsset :one
SELECT EXISTS (
 SELECT 1 FROM repair_ticket_asset_option o
 JOIN hosted_account_asset_projection a ON a.organization_id=o.org_id AND a.kind=o.allocation_kind AND a.asset_id=o.parent_asset_id
 WHERE o.org_id=sqlc.arg(org_id) AND o.kind=sqlc.arg(kind) AND o.id=sqlc.arg(asset_id)
 AND (a.client_id=sqlc.arg(account_internal_id) OR a.subclient_id=sqlc.arg(account_internal_id))
 AND (o.kind<>4 OR o.module_id=sqlc.narg(parent_module_id)::bigint)
)::boolean;

-- name: ListRepairTicketBoardOptions :many
SELECT DISTINCT jsonb_build_object('kind',4,'id',b.id,'moduleId',p.module_id,'installationId',i.id,'containerId',p.container_id,'position',i.position,'siteId',p.site_id,'model',p.model,
 'label','Hashboard '||COALESCE((SELECT value FROM hashboard_identity WHERE hashboard_id=b.id AND kind='serial' ORDER BY value LIMIT 1),b.id::text)||' / Module '||p.module_id||' / slot '||i.position)::jsonb AS payload
FROM repair_ticket_target p JOIN repair_ticket r ON r.org_id=p.org_id AND r.id=p.ticket_id
JOIN hashboard_installation i ON i.module_id=p.module_id AND i.installed_at<=r.created_at AND (i.removed_at IS NULL OR i.removed_at>r.created_at)
JOIN hashboard b ON b.id=i.hashboard_id AND b.org_id=p.org_id
WHERE p.org_id=sqlc.arg(org_id) AND p.ticket_id=sqlc.arg(ticket_id)
 AND (sqlc.arg(search)::text='' OR b.id::text=sqlc.arg(search) OR EXISTS(SELECT 1 FROM hashboard_identity x WHERE x.hashboard_id=b.id AND strpos(lower(x.value),lower(sqlc.arg(search)))>0))
LIMIT 200;

-- name: ShareRepairTicket :exec
UPDATE repair_ticket SET shared_with_clients=TRUE,updated_at=clock_timestamp()
WHERE org_id=$1 AND id=$2 AND deleted_at IS NULL AND NOT shared_with_clients;

-- name: SetRepairTicketCommentVisibility :execrows
WITH changed AS (
 UPDATE repair_ticket_comment c SET internal=sqlc.arg(internal),account_id=NULL
 FROM repair_ticket r
 WHERE c.org_id=sqlc.arg(org_id) AND c.id=sqlc.arg(comment_id) AND c.ticket_id=sqlc.arg(ticket_id)
 AND r.org_id=c.org_id AND r.id=c.ticket_id AND r.deleted_at IS NULL
 AND (sqlc.arg(internal)::boolean OR r.shared_with_clients)
 AND c.deleted_at IS NULL AND c.account_id IS NULL
 RETURNING c.ticket_id
)
UPDATE repair_ticket r SET updated_at=clock_timestamp()
WHERE r.org_id=sqlc.arg(org_id) AND r.id=sqlc.arg(ticket_id)
AND EXISTS(SELECT 1 FROM changed WHERE changed.ticket_id=r.id);

-- name: GetRepairTicketCommentForVisibility :one
SELECT c.internal,COALESCE(a.account_id,'')::text AS account_uuid
FROM repair_ticket_comment c LEFT JOIN hosted_account a ON a.organization_id=c.org_id AND a.id=c.account_id
WHERE c.org_id=$1 AND c.ticket_id=$2 AND c.id=$3 AND c.deleted_at IS NULL;
