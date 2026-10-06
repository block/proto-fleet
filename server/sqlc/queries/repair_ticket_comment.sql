-- name: LockRepairTicketCommentCreateKey :exec
SELECT pg_advisory_xact_lock(
    hashtextextended('repair_ticket_comment_create:' || sqlc.arg('org_id')::bigint::text || ':' || sqlc.arg('idempotency_key')::text, 0)
);

-- name: GetRepairTicketCommentByIdempotencyKey :one
SELECT id, org_id, ticket_id, user_id, user_name, text, created_at, deleted_at,
       create_request_hash
FROM repair_ticket_comment
WHERE org_id = sqlc.arg('org_id')
  AND idempotency_key = sqlc.arg('idempotency_key');

-- name: CreateRepairTicketComment :one
WITH inserted AS (
    INSERT INTO repair_ticket_comment (
        org_id, ticket_id, user_id, user_name, text,
        idempotency_key, create_request_hash
    )
    SELECT sqlc.arg('org_id'), sqlc.arg('ticket_id'), sqlc.arg('user_id'), u.username, sqlc.arg('text'),
           sqlc.arg('idempotency_key'), sqlc.arg('create_request_hash')
    FROM "user" u
    WHERE u.id = sqlc.arg('user_id') AND u.deleted_at IS NULL
    RETURNING id, org_id, ticket_id, user_id, user_name, text, created_at, deleted_at
)
SELECT i.id, i.org_id, i.ticket_id, i.user_id, i.user_name,
       i.text, i.created_at, i.deleted_at
FROM inserted i;

-- name: ListRepairTicketComments :many
SELECT c.id, c.org_id, c.ticket_id, c.user_id, c.user_name,
       c.text, c.created_at, c.deleted_at
FROM repair_ticket_comment c
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
UPDATE repair_ticket_comment
SET deleted_at = NOW()
WHERE id = sqlc.arg('id') AND org_id = sqlc.arg('org_id')
  AND user_id = sqlc.arg('caller_user_id') AND deleted_at IS NULL;
