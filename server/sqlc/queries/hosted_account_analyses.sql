-- name: CreateAccountAnalysis :exec
INSERT INTO hosted_account_analysis(analysis_id,organization_id,account_id,kind,snapshot,site_ids,ticket_ids,created_by,created_at,csv_data,sha256,request_key,request_digest)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13);

-- name: GetAccountAnalysis :one
SELECT id,analysis_id,kind,snapshot,site_ids,ticket_ids,sha256 FROM hosted_account_analysis
WHERE organization_id=$1 AND account_id=$2 AND analysis_id=$3;

-- name: ListAccountAnalyses :many
SELECT a.id,a.created_at,a.analysis_id,a.kind,(a.snapshot-'tables')::jsonb AS snapshot,a.site_ids,
 (a.kind<>'issues_repairs' OR NOT EXISTS (
   SELECT 1 FROM unnest(a.ticket_ids) ticket(id)
   WHERE NOT EXISTS (
     SELECT 1 FROM repair_ticket r JOIN repair_ticket_audience audience
       ON audience.org_id=r.org_id AND audience.ticket_id=r.id AND audience.account_id=a.account_id
     WHERE r.org_id=a.organization_id AND r.id=ticket.id AND r.deleted_at IS NULL AND r.shared_with_clients
   )
 ))::boolean AS tickets_visible
FROM hosted_account_analysis a
WHERE a.organization_id=sqlc.arg(organization_id) AND a.account_id=sqlc.arg(account_id)
 AND (sqlc.arg(before_id)::bigint=0 OR (a.created_at,a.id)<(sqlc.arg(before_at)::timestamptz,sqlc.arg(before_id)::bigint))
 AND (sqlc.arg(kind)::text='' OR a.kind=sqlc.arg(kind))
 AND strpos(lower(a.snapshot->>'name'),lower(sqlc.arg(search)::text))>0
ORDER BY a.created_at DESC,a.id DESC LIMIT sqlc.arg(page_limit);

-- name: GetAccountAnalysisCSVChunk :one
SELECT substring(csv_data FROM sqlc.arg(byte_offset)::integer+1 FOR 65536)::bytea AS data,
 octet_length(csv_data)::bigint AS byte_count,sha256 FROM hosted_account_analysis
WHERE organization_id=$1 AND account_id=$2 AND analysis_id=$3;

-- name: GetAccountAnalysisAccess :one
SELECT kind,site_ids,ticket_ids FROM hosted_account_analysis
WHERE organization_id=$1 AND account_id=$2 AND analysis_id=$3;

-- name: AccountAnalysisTicketsVisible :one
SELECT count(*)::bigint FROM repair_ticket r
JOIN repair_ticket_audience a ON a.org_id=r.org_id AND a.ticket_id=r.id AND a.account_id=sqlc.arg(account_id)
WHERE r.org_id=sqlc.arg(organization_id) AND r.id=ANY(sqlc.arg(ticket_ids)::bigint[])
 AND r.deleted_at IS NULL AND r.shared_with_clients;

-- name: LockAccountAnalysisLibrary :one
-- Serialize library writers while admitting the KEY SHARE locks that foreign
-- keys to this account take, so report preparation cannot deadlock with it.
SELECT id FROM hosted_account WHERE organization_id=$1 AND id=$2 FOR NO KEY UPDATE;

-- name: GetAccountAnalysisRequest :one
SELECT snapshot,request_digest,created_by,kind,site_ids,ticket_ids FROM hosted_account_analysis
WHERE organization_id=$1 AND account_id=$2 AND request_key=$3;

-- name: AccountAnalysisRequestDeleted :one
SELECT EXISTS(SELECT 1 FROM hosted_account_analysis_deletion WHERE organization_id=$1 AND account_id=$2 AND request_key=$3)::boolean;

-- name: AccountAnalysisDeleted :one
SELECT EXISTS(SELECT 1 FROM hosted_account_analysis_deletion WHERE organization_id=$1 AND account_id=$2 AND analysis_id=$3)::boolean;

-- name: CreateAccountAnalysisDeletion :one
INSERT INTO hosted_account_analysis_deletion(analysis_id,organization_id,account_id,created_by,deleted_by,request_key,request_digest)
SELECT a.analysis_id,a.organization_id,a.account_id,a.created_by,sqlc.arg(deleted_by)::bigint,a.request_key,a.request_digest
FROM hosted_account_analysis a WHERE a.organization_id=sqlc.arg(organization_id) AND a.account_id=sqlc.arg(account_id) AND a.analysis_id=sqlc.arg(analysis_id)
RETURNING request_key,request_digest,deleted_at;

-- name: DeleteAccountAnalysis :execrows
DELETE FROM hosted_account_analysis WHERE organization_id=$1 AND account_id=$2 AND analysis_id=$3;
