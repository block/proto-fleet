CREATE TABLE site (
    id                BIGSERIAL PRIMARY KEY,
    org_id            BIGINT NOT NULL,
    name              VARCHAR(255) NOT NULL,
    description       TEXT,
    location_city     VARCHAR(255),
    location_state    VARCHAR(255),
    timezone          VARCHAR(64),
    power_capacity_mw NUMERIC(10,3),
    network_config    TEXT,

    created_at        TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at        TIMESTAMPTZ NULL,

    CONSTRAINT fk_site_organization FOREIGN KEY (org_id)
        REFERENCES organization(id) ON DELETE RESTRICT,
    -- Composite-key target for child tables (building, device, history)
    -- to FK on (site_id, org_id) and reject cross-tenant pointers.
    CONSTRAINT uq_site_id_org_id UNIQUE (id, org_id)
);

CREATE UNIQUE INDEX uk_site_org_name
    ON site(org_id, name)
    WHERE deleted_at IS NULL;

CREATE INDEX idx_site_org_deleted
    ON site(org_id, deleted_at);

CREATE TRIGGER update_site_updated_at
    BEFORE UPDATE ON site
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
