CREATE TABLE repair_ticket_part (
    id          BIGSERIAL PRIMARY KEY,
    org_id      BIGINT NOT NULL,
    ticket_id   BIGINT NOT NULL,
    part_name   VARCHAR(255) NOT NULL,
    quantity    INT NOT NULL DEFAULT 1,

    CONSTRAINT fk_ticket_part_org FOREIGN KEY (org_id)
        REFERENCES organization(id) ON DELETE RESTRICT,
    CONSTRAINT fk_ticket_part_ticket FOREIGN KEY (ticket_id, org_id)
        REFERENCES repair_ticket(id, org_id) ON DELETE CASCADE,
    CONSTRAINT ck_ticket_part_qty CHECK (quantity > 0)
);

CREATE INDEX idx_ticket_part_ticket
    ON repair_ticket_part(ticket_id);
