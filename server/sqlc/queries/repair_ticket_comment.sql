-- name: CreateTicketComment :one
INSERT INTO repair_ticket_comment (
    org_id, ticket_id, user_id, user_name, text
) VALUES (
    sqlc.arg('org_id'),
    sqlc.arg('ticket_id'),
    sqlc.arg('user_id'),
    sqlc.arg('user_name'),
    sqlc.arg('text')
)
RETURNING *;

-- name: ListTicketComments :many
SELECT *
FROM repair_ticket_comment
WHERE ticket_id = sqlc.arg('ticket_id')
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL
ORDER BY created_at ASC;

-- name: SoftDeleteTicketComment :execrows
UPDATE repair_ticket_comment
SET deleted_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL;
