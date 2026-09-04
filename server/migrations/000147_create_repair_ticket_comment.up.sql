CREATE TABLE repair_ticket_comment (
    id          BIGSERIAL PRIMARY KEY,
    org_id      BIGINT NOT NULL,
    ticket_id   BIGINT NOT NULL,
    user_id     BIGINT NOT NULL,
    user_name   VARCHAR(255) NOT NULL,
    text        TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at  TIMESTAMPTZ,

    CONSTRAINT fk_ticket_comment_org FOREIGN KEY (org_id)
        REFERENCES organization(id) ON DELETE RESTRICT,
    CONSTRAINT fk_ticket_comment_ticket FOREIGN KEY (ticket_id, org_id)
        REFERENCES repair_ticket(id, org_id) ON DELETE CASCADE
);

CREATE INDEX idx_ticket_comment_ticket
    ON repair_ticket_comment(ticket_id)
    WHERE deleted_at IS NULL;
