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
    CONSTRAINT uq_inventory_part_id_org UNIQUE (id, org_id),

    CONSTRAINT ck_inventory_on_hand CHECK (on_hand >= 0),
    CONSTRAINT ck_inventory_allocated CHECK (allocated >= 0),
    CONSTRAINT ck_inventory_reorder CHECK (reorder_point >= 0)
);

CREATE UNIQUE INDEX uk_inventory_part_site_name
    ON inventory_part(org_id, COALESCE(site_id, 0), name)
    WHERE deleted_at IS NULL;

CREATE INDEX idx_inventory_part_org_site
    ON inventory_part(org_id, site_id)
    WHERE deleted_at IS NULL;

CREATE TRIGGER update_inventory_part_updated_at
    BEFORE UPDATE ON inventory_part
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
