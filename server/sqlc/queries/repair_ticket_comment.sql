-- name: CreateRepairTicketComment :one
WITH inserted AS (
    INSERT INTO repair_ticket_comment (org_id, ticket_id, user_id, user_name, text)
    SELECT sqlc.arg('org_id'), sqlc.arg('ticket_id'), sqlc.arg('user_id'), u.username, sqlc.arg('text')
    FROM "user" u
    WHERE u.id = sqlc.arg('user_id') AND u.deleted_at IS NULL
    RETURNING id, org_id, ticket_id, user_id, text, created_at, deleted_at
)
SELECT i.id, i.org_id, i.ticket_id, i.user_id, u.username AS user_name,
       i.text, i.created_at, i.deleted_at
FROM inserted i
JOIN "user" u ON u.id = i.user_id;

-- name: ListRepairTicketComments :many
SELECT c.id, c.org_id, c.ticket_id, c.user_id, u.username AS user_name,
       c.text, c.created_at, c.deleted_at
FROM repair_ticket_comment c
JOIN "user" u ON u.id = c.user_id
WHERE c.org_id = sqlc.arg('org_id') AND c.ticket_id = sqlc.arg('ticket_id')
  AND c.deleted_at IS NULL
ORDER BY c.created_at ASC, c.id ASC;

-- name: SoftDeleteRepairTicketCommentByAuthor :execrows
UPDATE repair_ticket_comment
SET deleted_at = NOW()
WHERE id = sqlc.arg('id') AND org_id = sqlc.arg('org_id')
  AND user_id = sqlc.arg('caller_user_id') AND deleted_at IS NULL;
