-- name: DeleteActiveRepairTicketParts :exec
DELETE FROM repair_ticket_part
WHERE org_id = sqlc.arg('org_id') AND ticket_id = sqlc.arg('ticket_id') AND consumed_at IS NULL;

-- name: InsertRepairTicketPart :exec
INSERT INTO repair_ticket_part (org_id, ticket_id, inventory_part_id, part_name, quantity)
VALUES (
    sqlc.arg('org_id'), sqlc.arg('ticket_id'), sqlc.arg('inventory_part_id'),
    sqlc.arg('part_name'), sqlc.arg('quantity')
);

-- name: MarkRepairTicketPartsConsumed :exec
UPDATE repair_ticket_part SET consumed_at = COALESCE(consumed_at, NOW())
WHERE org_id = sqlc.arg('org_id') AND ticket_id = sqlc.arg('ticket_id') AND consumed_at IS NULL;

-- name: ListRepairTicketParts :many
SELECT p.inventory_part_id, COALESCE(i.name, p.part_name) AS part_name,
       p.quantity, p.consumed_at
FROM repair_ticket_part p
LEFT JOIN inventory_part i
  ON i.id = p.inventory_part_id AND i.org_id = p.org_id
WHERE p.org_id = sqlc.arg('org_id') AND p.ticket_id = sqlc.arg('ticket_id')
ORDER BY p.id ASC;
