-- name: ListReportingJobAccounts :many
SELECT a.* FROM hosted_account a
WHERE a.depth>0 AND NOT a.suspended
 AND NOT EXISTS(SELECT 1 FROM hosted_account parent WHERE parent.organization_id=a.organization_id AND parent.suspended AND (parent.id=a.parent_id OR parent.depth=0))
ORDER BY a.organization_id,a.id;

-- name: LockHostedReportQueueAccount :exec
-- Serialize queue materialization for one account without an organization-wide write lock.
SELECT pg_advisory_xact_lock(hashtextextended('hosted-report-queue:' || sqlc.arg('account_id')::bigint::text, 0));

-- name: UpsertHostedReportQueue :one
INSERT INTO hosted_report_queue(queue_id,organization_id,account_id,work_key,owner_id,site_ids,projection,fingerprint,notification_status,retry_at)
VALUES(sqlc.arg(queue_id),sqlc.arg(organization_id),sqlc.arg(account_id),sqlc.arg(work_key),sqlc.arg(owner_id),sqlc.arg(site_ids)::bigint[],sqlc.arg(projection),sqlc.arg(fingerprint),sqlc.arg(notification_status),sqlc.arg(now_at))
ON CONFLICT(organization_id,account_id,work_key) DO UPDATE
SET owner_id=EXCLUDED.owner_id,site_ids=EXCLUDED.site_ids,projection=EXCLUDED.projection,fingerprint=EXCLUDED.fingerprint,
 generation=hosted_report_queue.generation+1,notification_status=EXCLUDED.notification_status,retry_at=EXCLUDED.retry_at,
 attempts=0,delivered_channel_ids='{}',claim_token=NULL,lease_until=NULL,updated_at=EXCLUDED.retry_at
WHERE hosted_report_queue.fingerprint<>EXCLUDED.fingerprint
RETURNING *;

-- name: ListHostedReportQueue :many
SELECT q.* FROM hosted_report_queue q JOIN hosted_account a ON a.id=q.account_id AND a.organization_id=q.organization_id
WHERE q.organization_id=sqlc.arg(organization_id) AND a.account_id=ANY(sqlc.arg(account_ids)::text[])
 AND q.notification_status<>'quiet' AND NOT(COALESCE(q.projection->'reasons','[]'::jsonb) ? 'superseded')
 AND (sqlc.arg(before_id)::bigint=0 OR q.id<sqlc.arg(before_id))
ORDER BY q.id DESC LIMIT sqlc.arg(page_limit);

-- name: GetHostedReportQueue :one
SELECT * FROM hosted_report_queue WHERE organization_id=$1 AND account_id=$2 AND queue_id=$3 FOR UPDATE;

-- name: AddHostedReportQueueEvent :exec
INSERT INTO hosted_report_queue_event(queue_internal_id,generation,kind,actor_id,reason,created_at)
VALUES($1,$2,$3,$4,$5,$6);

-- name: ListHostedReportQueueEvents :many
SELECT e.*,COALESCE(u.user_id,'')::text AS actor_user_id FROM hosted_report_queue_event e LEFT JOIN "user" u ON u.id=e.actor_id
WHERE e.queue_internal_id=$1 ORDER BY e.id;

-- name: ClaimHostedReportQueueNotification :one
WITH candidate AS (
 SELECT q.id FROM hosted_report_queue q JOIN hosted_account a ON a.id=q.account_id AND a.organization_id=q.organization_id
 WHERE q.notification_status IN ('pending','failed') AND q.attempts<5 AND q.retry_at<=sqlc.arg(now_at)
 AND (q.lease_until IS NULL OR q.lease_until<=sqlc.arg(now_at)) AND NOT a.suspended
 AND EXISTS(SELECT 1 FROM "user" u JOIN user_organization membership ON membership.user_id=u.id
  WHERE u.id=q.owner_id AND membership.organization_id=q.organization_id AND u.deleted_at IS NULL AND membership.deleted_at IS NULL
  AND (EXISTS(SELECT 1 FROM hosted_reporting_authority grant_row
    WHERE grant_row.user_id=u.id AND grant_row.organization_id=q.organization_id AND grant_row.account_id=q.account_id
    AND grant_row.can_create AND (grant_row.all_sites OR q.site_ids<@grant_row.site_ids)
    AND (q.projection->'request' IS NULL OR grant_row.all_sites))
   -- Without a stored grant, account managers hold implicit all-site authority.
   OR (NOT EXISTS(SELECT 1 FROM hosted_reporting_authority stored
    WHERE stored.user_id=u.id AND stored.organization_id=q.organization_id AND stored.account_id=q.account_id)
   AND EXISTS(SELECT 1 FROM user_organization_role uor JOIN role r ON r.id=uor.role_id AND r.organization_id=uor.organization_id
    JOIN role_permission rp ON rp.role_id=r.id JOIN permission p ON p.id=rp.permission_id
    WHERE uor.user_id=u.id AND uor.organization_id=q.organization_id AND uor.scope_type='org'
    AND uor.deleted_at IS NULL AND r.deleted_at IS NULL AND p.key='account:manage'))))
 AND NOT EXISTS(SELECT 1 FROM hosted_account parent WHERE parent.organization_id=a.organization_id AND parent.suspended AND (parent.id=a.parent_id OR parent.depth=0))
 ORDER BY q.retry_at,q.id FOR UPDATE OF q SKIP LOCKED LIMIT 1
)
UPDATE hosted_report_queue q SET claim_token=sqlc.arg(claim_token),lease_until=sqlc.arg(lease_until),attempts=q.attempts+1
FROM candidate WHERE q.id=candidate.id RETURNING q.*;

-- name: ExhaustExpiredHostedReportQueueNotifications :many
WITH candidates AS (
 SELECT id FROM hosted_report_queue
 WHERE notification_status IN ('pending','failed') AND attempts>=5
 AND (lease_until IS NULL OR lease_until<=sqlc.arg(now_at))
 ORDER BY retry_at,id FOR UPDATE SKIP LOCKED LIMIT 32
)
UPDATE hosted_report_queue q
SET notification_status='exhausted',claim_token=NULL,lease_until=NULL,updated_at=sqlc.arg(now_at)
FROM candidates WHERE q.id=candidates.id RETURNING q.id,q.generation;

-- name: CompleteHostedReportQueueNotification :execrows
UPDATE hosted_report_queue SET notification_status=sqlc.arg(notification_status),retry_at=sqlc.arg(retry_at),delivered_channel_ids=sqlc.arg(delivered_channel_ids)::bigint[],claim_token=NULL,lease_until=NULL,updated_at=sqlc.arg(now_at)
WHERE id=sqlc.arg(id) AND generation=sqlc.arg(generation) AND claim_token=sqlc.arg(claim_token) AND lease_until>sqlc.arg(now_at);

-- name: AcknowledgeHostedReportQueue :execrows
UPDATE hosted_report_queue SET notification_status='acknowledged',claim_token=NULL,lease_until=NULL,updated_at=$3
WHERE id=$1 AND generation=$2 AND notification_status<>'acknowledged';

-- name: RetryHostedReportQueueNotification :execrows
UPDATE hosted_report_queue SET notification_status='pending',attempts=0,retry_at=$3,claim_token=NULL,lease_until=NULL,updated_at=$3
WHERE id=$1 AND generation=$2 AND notification_status IN ('failed','acknowledged','sent','unrouted','exhausted','blocked');

-- name: GetReportingJobAccount :one
SELECT * FROM hosted_account WHERE organization_id=$1 AND id=$2;

-- name: RetireHostedReportQueue :many
UPDATE hosted_report_queue
SET projection=jsonb_set(projection,'{reasons}','["superseded"]'::jsonb),
 fingerprint='superseded:' || fingerprint,generation=generation+1,
 notification_status='quiet',claim_token=NULL,lease_until=NULL,updated_at=sqlc.arg(now_at)
WHERE organization_id=sqlc.arg(organization_id) AND account_id=sqlc.arg(account_id)
 AND NOT(work_key=ANY(sqlc.arg(active_work_keys)::text[]))
 AND projection->'reasons' IS DISTINCT FROM '["superseded"]'::jsonb
RETURNING *;

-- name: GetHostedReportQueueWork :one
SELECT projection FROM hosted_report_queue WHERE organization_id=$1 AND account_id=$2 AND work_key=$3;

-- name: ListHostedReportWorkSummaries :many
SELECT r.id,r.report_id,r.kind,r.period_start,r.period_end,r.series_id,r.number,r.created_at,
 r.correction_of,r.owner_id,r.revision,r.status,r.site_ids,r.period,
 jsonb_build_object('complete',r.result->'complete','deadlines',r.result->'deadlines',
 'metrics',COALESCE((SELECT jsonb_object_agg(key,jsonb_build_object('causes',value->'causes')) FROM jsonb_each(r.result->'metrics')),'{}'::jsonb))::jsonb AS result,
 COALESCE((SELECT CASE WHEN bool_or(d.status='confirmed') THEN 'confirmed' WHEN bool_or(d.status='failed') THEN 'failed' ELSE 'unconfirmed' END
 FROM hosted_report_delivery d WHERE d.report_internal_id=r.id),'unconfirmed')::text AS delivery_status
FROM hosted_report r
WHERE r.organization_id=$1 AND r.account_id=$2 AND r.id>sqlc.arg(after_id)::bigint
ORDER BY r.id LIMIT 100;
