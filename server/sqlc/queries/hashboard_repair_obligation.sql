-- name: OpenHashboardRepairObligationsForTicket :exec
-- Opens an obligation for each client-held hashboard the ticket names, unless
-- that client already has one open for the hashboard. A concurrent opener
-- waits on the partial unique index and then inserts nothing.
INSERT INTO hashboard_repair_obligation(org_id,hashboard_id,client_id)
SELECT t.org_id,t.hashboard_id,t.client_id FROM repair_ticket_target t
WHERE t.org_id=sqlc.arg(org_id) AND t.ticket_id=sqlc.arg(ticket_id) AND t.kind=4 AND t.hashboard_id IS NOT NULL AND t.client_id IS NOT NULL
ON CONFLICT (org_id,hashboard_id,client_id) WHERE restored_at IS NULL DO NOTHING;

-- name: LinkRepairTicketToHashboardObligations :exec
-- Runs as its own statement so that it sees an obligation a concurrent opener
-- committed while OpenHashboardRepairObligationsForTicket waited. The start,
-- subclient and placement of an obligation are those of the hashboard target
-- of its linked ticket that opened first.
INSERT INTO hashboard_repair_obligation_ticket(org_id,obligation_id,ticket_id)
SELECT e.org_id,e.id,t.ticket_id
FROM repair_ticket_target t
JOIN hashboard_repair_obligation e ON e.org_id=t.org_id AND e.hashboard_id=t.hashboard_id AND e.client_id=t.client_id AND e.restored_at IS NULL
WHERE t.org_id=sqlc.arg(org_id) AND t.ticket_id=sqlc.arg(ticket_id) AND t.kind=4
ON CONFLICT DO NOTHING;
