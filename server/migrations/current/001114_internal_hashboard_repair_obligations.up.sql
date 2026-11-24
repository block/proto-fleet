-- requires-shared: 1000
-- The client of each hashboard in a module when a module ticket opened. The
-- module target has no client when its hashboards have different clients, so a
-- hashboard that an operator selects later takes its client from this record.
-- The rows are written under an ownership lock in the ticket opening.
CREATE TABLE repair_ticket_board_owner (
 org_id BIGINT NOT NULL,
 ticket_id BIGINT NOT NULL,
 hashboard_id BIGINT NOT NULL REFERENCES hashboard(id),
 client_id BIGINT NOT NULL,
 subclient_id BIGINT,
 PRIMARY KEY (org_id, ticket_id, hashboard_id),
 FOREIGN KEY (ticket_id, org_id) REFERENCES repair_ticket(id, org_id),
 FOREIGN KEY (org_id, client_id) REFERENCES hosted_account(organization_id, id),
 FOREIGN KEY (org_id, subclient_id) REFERENCES hosted_account(organization_id, id)
);

-- A repair obligation is the period in which a host must restore a client's
-- hashboard. A ticket joins the open obligation of each hashboard it names. A
-- module ticket names no hashboard until an operator selects the affected one.
-- Closing a ticket does not end the obligation; only evidenced restoration
-- sets restored_at. The obligation stores no start, subclient or placement:
-- read them from the hashboard target of the linked ticket that opened first.
CREATE TABLE hashboard_repair_obligation (
 id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 org_id BIGINT NOT NULL,
 hashboard_id BIGINT NOT NULL REFERENCES hashboard(id),
 client_id BIGINT NOT NULL,
 restored_at TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 CONSTRAINT uq_hashboard_repair_obligation_id_org UNIQUE (id, org_id),
 FOREIGN KEY (org_id, client_id) REFERENCES hosted_account(organization_id, id)
);
-- Concurrent tickets for one hashboard converge on one open obligation per client.
CREATE UNIQUE INDEX idx_hashboard_repair_obligation_open ON hashboard_repair_obligation(org_id, hashboard_id, client_id) WHERE restored_at IS NULL;

-- linked_at is later than the ticket opening when an operator selects the
-- hashboard on a module ticket.
CREATE TABLE hashboard_repair_obligation_ticket (
 org_id BIGINT NOT NULL,
 obligation_id BIGINT NOT NULL,
 ticket_id BIGINT NOT NULL,
 linked_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 PRIMARY KEY (org_id, obligation_id, ticket_id),
 FOREIGN KEY (obligation_id, org_id) REFERENCES hashboard_repair_obligation(id, org_id),
 FOREIGN KEY (ticket_id, org_id) REFERENCES repair_ticket(id, org_id)
);
CREATE INDEX idx_hashboard_repair_obligation_ticket_ticket ON hashboard_repair_obligation_ticket(org_id, ticket_id);
