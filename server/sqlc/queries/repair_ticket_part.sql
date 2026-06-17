-- name: SetTicketParts :exec
-- Replaces all parts for a ticket. Caller deletes existing then inserts.
-- Used within a transaction managed by the service layer.
DELETE FROM repair_ticket_part
WHERE ticket_id = sqlc.arg('ticket_id')
  AND org_id = sqlc.arg('org_id');

-- name: InsertTicketPart :exec
INSERT INTO repair_ticket_part (
    org_id, ticket_id, part_name, quantity
) VALUES (
    sqlc.arg('org_id'),
    sqlc.arg('ticket_id'),
    sqlc.arg('part_name'),
    sqlc.arg('quantity')
);

-- name: ListTicketParts :many
SELECT *
FROM repair_ticket_part
WHERE ticket_id = sqlc.arg('ticket_id')
  AND org_id = sqlc.arg('org_id');
