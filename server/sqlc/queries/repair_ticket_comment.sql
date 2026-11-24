-- name: LockRepairTicketCommentCreateKey :exec
SELECT pg_advisory_xact_lock(
    hashtextextended('repair_ticket_comment_create:' || sqlc.arg('org_id')::bigint::text || ':' || sqlc.arg('idempotency_key')::text, 0)
);

-- name: GetRepairTicketCommentByIdempotencyKey :one
SELECT id, org_id, ticket_id, user_id, user_name, text, created_at, deleted_at,
       create_request_hash,internal,COALESCE((SELECT account_id FROM hosted_account WHERE id=repair_ticket_comment.account_id),'')::text AS account_uuid,COALESCE((SELECT name FROM hosted_account WHERE id=repair_ticket_comment.account_id),'')::text AS account_name
FROM repair_ticket_comment
WHERE org_id = sqlc.arg('org_id')
  AND idempotency_key = sqlc.arg('idempotency_key');

-- name: CreateRepairTicketComment :one
WITH inserted AS (
    INSERT INTO repair_ticket_comment (
        org_id, ticket_id, user_id, user_name, text,
        idempotency_key, create_request_hash,internal,account_id
    )
    SELECT sqlc.arg('org_id'), sqlc.arg('ticket_id'), sqlc.arg('user_id'), u.username, sqlc.arg('text'),
           sqlc.arg('idempotency_key'), sqlc.arg('create_request_hash'), sqlc.arg('internal'), a.id
    FROM "user" u
    LEFT JOIN hosted_account a ON a.organization_id=sqlc.arg('org_id') AND a.account_id=sqlc.arg('account_uuid')
    WHERE (sqlc.arg('account_uuid')::text='' OR a.id IS NOT NULL) AND u.id = sqlc.arg('user_id') AND u.deleted_at IS NULL
    RETURNING id, org_id, ticket_id, user_id, user_name, text, created_at, deleted_at,internal,account_id
)
SELECT i.id, i.org_id, i.ticket_id, i.user_id, i.user_name,
       i.text, i.created_at, i.deleted_at,i.internal,COALESCE(a.account_id,'')::text AS account_uuid,COALESCE(a.name,'')::text AS account_name
FROM inserted i LEFT JOIN hosted_account a ON a.id=i.account_id AND a.organization_id=i.org_id;

-- name: ListRepairTicketComments :many
SELECT c.id, c.org_id, c.ticket_id, c.user_id, c.user_name,
       c.text, c.created_at, c.deleted_at,c.internal,COALESCE(a.account_id,'')::text AS account_uuid,COALESCE(a.name,'')::text AS account_name
FROM repair_ticket_comment c LEFT JOIN hosted_account a ON a.id=c.account_id AND a.organization_id=c.org_id
WHERE c.org_id = sqlc.arg('org_id') AND c.ticket_id = sqlc.arg('ticket_id')
  AND c.deleted_at IS NULL
ORDER BY c.created_at ASC, c.id ASC;

-- name: GetRepairTicketCommentSiteForUpdate :one
SELECT rt.site_id
FROM repair_ticket_comment c
JOIN repair_ticket rt
  ON rt.id = c.ticket_id AND rt.org_id = c.org_id AND rt.deleted_at IS NULL
WHERE c.id = sqlc.arg('id')
  AND c.org_id = sqlc.arg('org_id')
  AND c.user_id = sqlc.arg('caller_user_id')
  AND c.deleted_at IS NULL
FOR UPDATE OF c, rt;

-- name: SoftDeleteRepairTicketCommentByAuthor :execrows
WITH deleted AS (
    UPDATE repair_ticket_comment c
    SET deleted_at = NOW()
    WHERE c.id = sqlc.arg('id') AND c.org_id = sqlc.arg('org_id')
      AND c.user_id = sqlc.arg('caller_user_id') AND c.deleted_at IS NULL
    RETURNING c.org_id, c.ticket_id
)
UPDATE repair_ticket r SET updated_at = clock_timestamp()
FROM deleted d
WHERE r.org_id = d.org_id AND r.id = d.ticket_id;
