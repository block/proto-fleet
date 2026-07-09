-- Facility infrastructure devices (fans / fan groups behind a PLC or
-- drive) used by curtailment sequencing. The core stores only
-- protocol-blind facts; everything protocol-specific lives in the
-- opaque driver_config blob owned by the matching driver adapter
-- (see server/internal/domain/infrastructure/driver).
CREATE TABLE infrastructure_device (
    id            BIGSERIAL PRIMARY KEY,
    org_id        BIGINT NOT NULL,
    site_id       BIGINT NOT NULL,
    building_name VARCHAR(255) NOT NULL DEFAULT '',
    name          VARCHAR(255) NOT NULL,
    -- 'single_fan' | 'fan_group'
    device_kind   VARCHAR(32) NOT NULL,
    fan_count     INT NOT NULL DEFAULT 1,
    enabled       BOOLEAN NOT NULL DEFAULT TRUE,
    -- Driver adapter key, e.g. 'modbus_tcp'. Interpreted only by the
    -- driver registry, never by core queries.
    driver_type   VARCHAR(64) NOT NULL,
    -- Opaque adapter-owned connection config (for modbus_tcp:
    -- endpoint, port, unit_id, register_address, write_mode).
    driver_config JSONB NOT NULL DEFAULT '{}'::jsonb,

    created_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at    TIMESTAMPTZ NULL,

    CONSTRAINT fk_infrastructure_device_organization FOREIGN KEY (org_id)
        REFERENCES organization(id) ON DELETE RESTRICT,
    CONSTRAINT fk_infrastructure_device_site FOREIGN KEY (site_id, org_id)
        REFERENCES site(id, org_id) ON DELETE RESTRICT,
    CONSTRAINT uq_infrastructure_device_id_org_id UNIQUE (id, org_id),

    CONSTRAINT ck_infrastructure_device_kind
        CHECK (device_kind IN ('single_fan', 'fan_group')),
    CONSTRAINT ck_infrastructure_device_fan_count
        CHECK (
            (device_kind = 'single_fan' AND fan_count = 1)
            OR (device_kind = 'fan_group' AND fan_count >= 2)
        )
);

-- Name is unique within a site among live rows so operators can
-- address devices unambiguously in pickers and activity logs.
CREATE UNIQUE INDEX uk_infrastructure_device_site_name
    ON infrastructure_device(site_id, name)
    WHERE deleted_at IS NULL;

CREATE INDEX idx_infrastructure_device_org_deleted
    ON infrastructure_device(org_id, deleted_at);
CREATE INDEX idx_infrastructure_device_site_deleted
    ON infrastructure_device(site_id, deleted_at);

CREATE TRIGGER update_infrastructure_device_updated_at
    BEFORE UPDATE ON infrastructure_device
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
