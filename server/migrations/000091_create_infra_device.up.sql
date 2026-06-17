CREATE TABLE infra_device (
    id             BIGSERIAL PRIMARY KEY,
    org_id         BIGINT NOT NULL,
    name           VARCHAR(255) NOT NULL,
    device_type    SMALLINT NOT NULL DEFAULT 0,
    subtype        VARCHAR(128),
    site_id        BIGINT,
    building_id    BIGINT,
    ip_address     VARCHAR(45),
    status         SMALLINT NOT NULL DEFAULT 0,
    control_mode   SMALLINT NOT NULL DEFAULT 0,
    rpm            DOUBLE PRECISION,
    protocol       VARCHAR(64),
    last_seen      TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at     TIMESTAMPTZ,

    CONSTRAINT fk_infra_device_org FOREIGN KEY (org_id)
        REFERENCES organization(id) ON DELETE RESTRICT,
    CONSTRAINT uq_infra_device_id_org UNIQUE (id, org_id),

    CONSTRAINT ck_infra_device_type CHECK (device_type BETWEEN 0 AND 3),
    CONSTRAINT ck_infra_device_status CHECK (status BETWEEN 0 AND 3),
    CONSTRAINT ck_infra_device_control CHECK (control_mode BETWEEN 0 AND 3)
);

CREATE INDEX idx_infra_device_org_site
    ON infra_device(org_id, site_id)
    WHERE deleted_at IS NULL AND site_id IS NOT NULL;

CREATE INDEX idx_infra_device_org_building
    ON infra_device(org_id, building_id)
    WHERE deleted_at IS NULL AND building_id IS NOT NULL;

CREATE INDEX idx_infra_device_org_status
    ON infra_device(org_id, status)
    WHERE deleted_at IS NULL;

CREATE TRIGGER update_infra_device_updated_at
    BEFORE UPDATE ON infra_device
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
