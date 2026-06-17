CREATE TABLE repair_ticket (
    id                BIGSERIAL PRIMARY KEY,
    org_id            BIGINT NOT NULL,
    ticket_number     VARCHAR(16) NOT NULL,
    category          SMALLINT NOT NULL DEFAULT 0,
    status            SMALLINT NOT NULL DEFAULT 1,
    urgent            BOOLEAN NOT NULL DEFAULT FALSE,
    component         VARCHAR(255) NOT NULL,
    diagnosis         TEXT,
    miner_identifier  VARCHAR(256),
    alert_id          VARCHAR(64),
    assignee_user_id  BIGINT,
    warranty_status   SMALLINT NOT NULL DEFAULT 0,
    site_id           BIGINT,
    building_id       BIGINT,
    zone              VARCHAR(255),
    rack_id           BIGINT,
    rack_label        VARCHAR(255),
    group_label       VARCHAR(255),
    resolution        SMALLINT NOT NULL DEFAULT 0,
    repair_location   SMALLINT NOT NULL DEFAULT 0,
    notes             TEXT,
    daily_impact_usd  NUMERIC(10,2) DEFAULT 0,
    rma_vendor        VARCHAR(255),
    rma_tracking      VARCHAR(255),
    rma_eta           TIMESTAMPTZ,
    completed_at      TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at        TIMESTAMPTZ,

    CONSTRAINT fk_repair_ticket_org FOREIGN KEY (org_id)
        REFERENCES organization(id) ON DELETE RESTRICT,
    CONSTRAINT uq_repair_ticket_id_org UNIQUE (id, org_id),

    CONSTRAINT ck_repair_ticket_category CHECK (category BETWEEN 0 AND 2),
    CONSTRAINT ck_repair_ticket_status CHECK (status BETWEEN 0 AND 5),
    CONSTRAINT ck_repair_ticket_resolution CHECK (resolution BETWEEN 0 AND 4),
    CONSTRAINT ck_repair_ticket_repair_location CHECK (repair_location BETWEEN 0 AND 2),
    CONSTRAINT ck_repair_ticket_warranty CHECK (warranty_status BETWEEN 0 AND 3)
);

CREATE UNIQUE INDEX uk_repair_ticket_number_org
    ON repair_ticket(org_id, ticket_number)
    WHERE deleted_at IS NULL;

CREATE INDEX idx_repair_ticket_org_status
    ON repair_ticket(org_id, status)
    WHERE deleted_at IS NULL;

CREATE INDEX idx_repair_ticket_org_site
    ON repair_ticket(org_id, site_id)
    WHERE deleted_at IS NULL AND site_id IS NOT NULL;

CREATE INDEX idx_repair_ticket_org_building
    ON repair_ticket(org_id, building_id)
    WHERE deleted_at IS NULL AND building_id IS NOT NULL;

CREATE INDEX idx_repair_ticket_miner
    ON repair_ticket(org_id, miner_identifier)
    WHERE deleted_at IS NULL AND miner_identifier IS NOT NULL;

CREATE INDEX idx_repair_ticket_assignee
    ON repair_ticket(org_id, assignee_user_id)
    WHERE deleted_at IS NULL AND assignee_user_id IS NOT NULL;

CREATE INDEX idx_repair_ticket_rack
    ON repair_ticket(org_id, rack_id)
    WHERE deleted_at IS NULL AND rack_id IS NOT NULL;

CREATE TRIGGER update_repair_ticket_updated_at
    BEFORE UPDATE ON repair_ticket
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
