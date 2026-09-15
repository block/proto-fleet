CREATE FUNCTION update_repair_ticket_version()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = GREATEST(clock_timestamp(), OLD.updated_at + INTERVAL '1 microsecond');
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TABLE repair_ticket_counter (
    org_id       BIGINT PRIMARY KEY,
    next_number  BIGINT NOT NULL CHECK (next_number > 0),

    CONSTRAINT fk_repair_ticket_counter_org FOREIGN KEY (org_id)
        REFERENCES organization(id) ON DELETE RESTRICT
);

CREATE TABLE repair_ticket (
    id                BIGSERIAL PRIMARY KEY,
    org_id            BIGINT NOT NULL,
    ticket_number     VARCHAR(16) NOT NULL,
    category          SMALLINT NOT NULL,
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
    idempotency_key    VARCHAR(64),
    create_request_hash CHAR(64),

    CONSTRAINT fk_repair_ticket_org FOREIGN KEY (org_id)
        REFERENCES organization(id) ON DELETE RESTRICT,
    CONSTRAINT fk_repair_ticket_assignee FOREIGN KEY (assignee_user_id)
        REFERENCES "user"(id) ON DELETE RESTRICT,
    CONSTRAINT fk_repair_ticket_site FOREIGN KEY (site_id, org_id)
        REFERENCES site(id, org_id) ON DELETE RESTRICT,
    CONSTRAINT fk_repair_ticket_building FOREIGN KEY (building_id, org_id)
        REFERENCES building(id, org_id) ON DELETE RESTRICT,
    CONSTRAINT uq_repair_ticket_id_org UNIQUE (id, org_id),

    CONSTRAINT ck_repair_ticket_category CHECK (category BETWEEN 1 AND 2),
    CONSTRAINT ck_repair_ticket_status CHECK (status BETWEEN 1 AND 5),
    CONSTRAINT ck_repair_ticket_resolution CHECK (resolution BETWEEN 0 AND 5),
    CONSTRAINT ck_repair_ticket_repair_location CHECK (repair_location BETWEEN 0 AND 2),
    CONSTRAINT ck_repair_ticket_warranty CHECK (warranty_status BETWEEN 0 AND 3),
    CONSTRAINT ck_repair_ticket_idempotency_key_nonempty
        CHECK (idempotency_key IS NULL OR length(idempotency_key) > 0),
    CONSTRAINT ck_repair_ticket_create_request_hash_length
        CHECK (create_request_hash IS NULL OR length(create_request_hash) = 64)
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

CREATE UNIQUE INDEX uq_repair_ticket_org_idempotency_key
    ON repair_ticket(org_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

CREATE TRIGGER update_repair_ticket_updated_at
    BEFORE UPDATE ON repair_ticket
    FOR EACH ROW EXECUTE FUNCTION update_repair_ticket_version();

CREATE TABLE repair_ticket_comment (
    id          BIGSERIAL PRIMARY KEY,
    org_id      BIGINT NOT NULL,
    ticket_id   BIGINT NOT NULL,
    user_id     BIGINT NOT NULL,
    user_name   VARCHAR(255) NOT NULL,
    text        TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at  TIMESTAMPTZ,
    idempotency_key VARCHAR(64),
    create_request_hash CHAR(64),

    CONSTRAINT fk_ticket_comment_org FOREIGN KEY (org_id)
        REFERENCES organization(id) ON DELETE RESTRICT,
    CONSTRAINT fk_ticket_comment_ticket FOREIGN KEY (ticket_id, org_id)
        REFERENCES repair_ticket(id, org_id) ON DELETE CASCADE,
    CONSTRAINT fk_ticket_comment_user FOREIGN KEY (user_id)
        REFERENCES "user"(id) ON DELETE RESTRICT,
    CONSTRAINT ck_repair_ticket_comment_idempotency_key_nonempty
        CHECK (idempotency_key IS NULL OR length(idempotency_key) > 0),
    CONSTRAINT ck_repair_ticket_comment_request_hash_length
        CHECK (create_request_hash IS NULL OR length(create_request_hash) = 64)
);

CREATE INDEX idx_ticket_comment_ticket
    ON repair_ticket_comment(ticket_id)
    WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX uq_repair_ticket_comment_org_idempotency_key
    ON repair_ticket_comment (org_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

CREATE TABLE inventory_part (
    id             BIGSERIAL PRIMARY KEY,
    org_id         BIGINT NOT NULL,
    name           VARCHAR(255) NOT NULL,
    type           VARCHAR(64) NOT NULL,
    manufacturer   VARCHAR(255),
    part_number    VARCHAR(128),
    site_id        BIGINT,
    on_hand        INT NOT NULL DEFAULT 0,
    allocated      INT NOT NULL DEFAULT 0,
    reorder_point  INT NOT NULL DEFAULT 0,
    bin_location   VARCHAR(64),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at     TIMESTAMPTZ,

    CONSTRAINT fk_inventory_part_org FOREIGN KEY (org_id)
        REFERENCES organization(id) ON DELETE RESTRICT,
    CONSTRAINT fk_inventory_part_site FOREIGN KEY (site_id, org_id)
        REFERENCES site(id, org_id) ON DELETE RESTRICT,
    CONSTRAINT uq_inventory_part_id_org UNIQUE (id, org_id),

    CONSTRAINT ck_inventory_on_hand CHECK (on_hand >= 0),
    CONSTRAINT ck_inventory_allocated CHECK (allocated >= 0),
    CONSTRAINT ck_inventory_allocation_within_stock CHECK (allocated <= on_hand),
    CONSTRAINT ck_inventory_reorder CHECK (reorder_point >= 0)
);

CREATE UNIQUE INDEX uk_inventory_part_site_name
    ON inventory_part(org_id, COALESCE(site_id, 0), LOWER(name))
    WHERE deleted_at IS NULL;

CREATE INDEX idx_inventory_part_org_site
    ON inventory_part(org_id, site_id)
    WHERE deleted_at IS NULL;

CREATE TRIGGER update_inventory_part_updated_at
    BEFORE UPDATE ON inventory_part
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

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
