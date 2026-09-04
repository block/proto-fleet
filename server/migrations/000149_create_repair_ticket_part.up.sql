CREATE TABLE repair_ticket_part (
    id                 BIGSERIAL PRIMARY KEY,
    org_id             BIGINT NOT NULL,
    ticket_id          BIGINT NOT NULL,
    inventory_part_id  BIGINT NOT NULL,
    part_name          VARCHAR(255) NOT NULL,
    quantity           INT NOT NULL CHECK (quantity > 0),
    consumed_at        TIMESTAMPTZ,

    CONSTRAINT uq_ticket_inventory_part UNIQUE (ticket_id, inventory_part_id),
    CONSTRAINT fk_ticket_part_ticket FOREIGN KEY (ticket_id, org_id)
        REFERENCES repair_ticket(id, org_id) ON DELETE CASCADE,
    CONSTRAINT fk_ticket_part_inventory FOREIGN KEY (inventory_part_id, org_id)
        REFERENCES inventory_part(id, org_id) ON DELETE RESTRICT
);

CREATE INDEX idx_ticket_part_ticket
    ON repair_ticket_part(ticket_id);

CREATE INDEX idx_ticket_part_inventory
    ON repair_ticket_part(inventory_part_id);
